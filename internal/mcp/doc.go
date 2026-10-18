package mcp

// 文档条目（集合树里的独立 Markdown 文件）在 MCP 侧的读写。
//
// 先分清两种「doc」，它们的载体与工具完全不同：
//   - request.docs：接口自带的说明，存在请求文件的 frontmatter 里，
//     走 create_request / update_request 的 docs 字段，get_request_detail 以「说明文档」返回；
//   - doc 条目：集合树里独立的 .md（uid 独立、有自己的路径，可挂在任意分组目录下），
//     客户端能新建/改名/移动/删除。此前 MCP 只在 get_request_detail 的 relatedDocs 里露出文件名，
//     既读不到正文也改不了 —— 本文件补齐这部分（list/get/create/update/delete_doc）。

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"api-doc-go-client/internal/collection"
)

var (
	errNeedDocName = errors.New("缺少 name：创建文档必须给名称")
	errNeedDocUID  = errors.New("缺少 uid：请先用 list_docs 找到目标文档")
)

// DocItem 文档条目元信息（不含正文），list_docs 的一行。
// 名字避开 service.go 里已有的 DocBrief（那是 get_request_detail 的 relatedDocs 用的精简版）。
type DocItem struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
	Path string `json:"path"`
	Icon string `json:"icon,omitempty"`
	Seq  int    `json:"seq,omitempty"`
	Size int    `json:"size"` // 正文字节数：AI 可据此判断要不要 get_doc 取全文
}

// DocDetail 文档详情：正文 + 磁盘 hash。
// hash 可回传给 update_doc.ifMatch —— 与请求的 ifMatch 同语义，防「读完到写回之间被外部改动」。
type DocDetail struct {
	DocItem
	Content string `json:"content"`
	Hash    string `json:"hash,omitempty"`
}

// ListDocs 列出项目里的文档条目（按路径排序）；query 对名称/路径做大小写不敏感的子串过滤。
func (s *Service) ListDocs(project, query string) ([]DocItem, error) {
	a, _, err := s.resolve(project)
	if err != nil {
		return nil, err
	}
	docs, err := a.ListDocs()
	if err != nil {
		return nil, fmt.Errorf("列不出文档: %w", err)
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]DocItem, 0, len(docs))
	for _, d := range docs {
		if q != "" &&
			!strings.Contains(strings.ToLower(d.Name), q) &&
			!strings.Contains(strings.ToLower(d.Path), q) {
			continue
		}
		out = append(out, docItem(d))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// GetDoc 读单篇文档（含正文）。
func (s *Service) GetDoc(project, uid string) (*DocDetail, error) {
	a, _, err := s.resolve(project)
	if err != nil {
		return nil, err
	}
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return nil, errNeedDocUID
	}
	d, err := a.ReadDoc(uid)
	if err != nil {
		return nil, fmt.Errorf("读取文档 %s 失败（用 list_docs 查看可用 uid）: %w", uid, err)
	}
	return &DocDetail{DocItem: docItem(d), Content: d.Content, Hash: a.DocFileHash(uid)}, nil
}

// CreateDocInput create_doc 的入参。
type CreateDocInput struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 本会话 use_workspace 选定的默认工作目录"`
	Name    string `json:"name" jsonschema:"文档名（不含 .md 后缀，不能带路径分隔符）"`
	Folder  string `json:"folder,omitempty" jsonschema:"目标分组目录（相对集合根，如 docs 或 用户/说明）；留空 = 集合根目录"`
	Content string `json:"content,omitempty" jsonschema:"Markdown 正文；留空则创建空文档，之后可用 update_doc 写入"`
}

// CreateDoc 新建文档条目；content 非空时一并写入。
func (s *Service) CreateDoc(in CreateDocInput) (*DocDetail, error) {
	a, _, err := s.resolveWrite(in.Project)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errNeedDocName
	}
	d, err := a.CreateDoc(name, strings.TrimSpace(in.Folder))
	if err != nil {
		return nil, err
	}
	// CreateDoc 落盘的是空正文；有内容时再存一次（不传 ifMatch：刚创建的文件不存在并发问题）
	if in.Content != "" {
		d.Content = in.Content
		if err := a.SaveDoc(d); err != nil {
			return nil, err
		}
	}
	return &DocDetail{DocItem: docItem(d), Content: d.Content, Hash: a.DocFileHash(d.UID)}, nil
}

// UpdateDocInput update_doc 的入参（指针字段 = 只改传了的）。
type UpdateDocInput struct {
	Project string  `json:"project,omitempty" jsonschema:"项目路径、名称或 uid；省略 = 本会话 use_workspace 选定的默认工作目录"`
	UID     string  `json:"uid" jsonschema:"文档 uid（list_docs / get_doc 返回）"`
	Name    *string `json:"name,omitempty" jsonschema:"新的显示名（只改 frontmatter 的 name，文件名不变）"`
	Content *string `json:"content,omitempty" jsonschema:"新的 Markdown 正文；传空串 = 清空正文；不传不动"`
	Folder  *string `json:"folder,omitempty" jsonschema:"移动到该分组目录（相对集合根）；传空串 = 移到集合根；不传不动"`
	IfMatch string  `json:"ifMatch,omitempty" jsonschema:"并发保护：传 get_doc 返回的 hash；磁盘在此期间被外部改动则拒绝写入"`
}

// UpdateDoc 局部更新文档：正文（带 ifMatch 校验）/ 改名 / 移动可任意组合。
//
// 顺序：先写正文（此时做 hash 校验），再移动、改名 —— 后两者只动路径与 frontmatter，
// 不改文件内容，放在校验之后不会让 ifMatch 失效。
func (s *Service) UpdateDoc(in UpdateDocInput) (*DocDetail, error) {
	a, _, err := s.resolveWrite(in.Project)
	if err != nil {
		return nil, err
	}
	uid := strings.TrimSpace(in.UID)
	if uid == "" {
		return nil, errNeedDocUID
	}
	d, err := a.ReadDoc(uid)
	if err != nil {
		return nil, fmt.Errorf("读取文档 %s 失败（用 list_docs 查看可用 uid）: %w", uid, err)
	}
	if in.Content != nil {
		d.Content = *in.Content
		if err := a.SaveDocExpectHash(d, strings.TrimSpace(in.IfMatch)); err != nil {
			return nil, err
		}
	}
	if in.Folder != nil {
		if err := a.MoveDoc(uid, strings.TrimSpace(*in.Folder)); err != nil {
			return nil, err
		}
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, errNeedDocName
		}
		if err := a.RenameDoc(uid, name); err != nil {
			return nil, err
		}
	}
	out, err := a.ReadDoc(uid)
	if err != nil {
		return nil, err
	}
	return &DocDetail{DocItem: docItem(out), Content: out.Content, Hash: a.DocFileHash(uid)}, nil
}

// DeleteDoc 移除文档（进 .trash，可人工找回）。
func (s *Service) DeleteDoc(project, uid string) error {
	a, _, err := s.resolveWrite(project)
	if err != nil {
		return err
	}
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return errNeedDocUID
	}
	return a.DeleteDoc(uid)
}

// docBrief 把集合层的 DocEntry 投影成 MCP 返回结构（正文只算长度，列表里不带全文）。
func docItem(d *collection.DocEntry) DocItem {
	return DocItem{UID: d.UID, Name: d.Name, Path: d.Path, Icon: d.Icon, Seq: d.Seq, Size: len(d.Content)}
}
