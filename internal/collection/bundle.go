package collection

// 集合整包导出 / 导入（问题记录 item 26）。
//
// 导出一个自包含的 JSON：目录结构 + 全部请求（含参数/头/体/认证/设置/脚本/断言/gRPC/
// 接口说明 docs/响应字段文档 respDoc）+ 全部文档条目（Markdown 正文）。
// 别的 api-doc-go 客户端选「API Doc Go 集合」格式即可直接导入并**完整展示**。
//
// 为什么不复用 Postman / OpenAPI：那两种格式表达不了本客户端已有内容 ——
// respDoc（响应字段文档）、脚本与断言、gRPC 段、目录里独立的 .md 文档、排序 seq ——
// 转出去再转回来必然缺斤少两。这里直接沿用内存契约（Request / DocEntry 的 JSON 形态），
// 因此往返是无损的。
//
// 明确**不含**环境与密钥（environments/*.secrets.yml）：导出文件常被分享，
// 把密钥写进去等于泄露；接收方导入后按需自建环境。

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"
)

const (
	// BundleFormat 包内标识：导入时据此判断「这是不是本客户端的集合文件」。
	BundleFormat = "api-doc-go/collection"
	// BundleVersion 格式版本；导入时拒绝比当前更高的版本（避免误读新字段）。
	BundleVersion = 1
)

// CollectionBundle 集合整包。
type CollectionBundle struct {
	Format     string         `json:"format"`
	Version    int            `json:"version"`
	ExportedAt string         `json:"exportedAt,omitempty"`
	Collection BundleInfo     `json:"collection"`
	Folders    []BundleFolder `json:"folders,omitempty"`
	Requests   []*Request     `json:"requests,omitempty"`
	Docs       []DocEntry     `json:"docs,omitempty"`
}

// BundleInfo 集合本身的元信息（名称用于导入后建同名顶层分组）。
type BundleInfo struct {
	Name string `json:"name"`
	UID  string `json:"uid,omitempty"`
}

// BundleFolder 目录条目（保留空目录与顺序；path 相对集合根）。
type BundleFolder struct {
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
	Seq  int    `json:"seq,omitempty"`
}

// ExportBundle 把当前集合打成一份 JSON（缩进 2 空格，便于人工查看/进版本库）。
func (c *Collection) ExportBundle() ([]byte, error) {
	res, err := c.scan()
	if err != nil {
		return nil, err
	}
	b := CollectionBundle{
		Format:     BundleFormat,
		Version:    BundleVersion,
		ExportedAt: time.Now().Format(time.RFC3339),
		Collection: BundleInfo{Name: c.Name, UID: c.UID},
	}
	// 按树先序遍历：目录、请求、文档的顺序与客户端侧栏一致，导入后顺序也就保住了
	// （导入侧逐个创建，seq 由 nextSeq 递增分配）。
	var walk func(nodes []*Node)
	walk = func(nodes []*Node) {
		for _, n := range nodes {
			switch n.Type {
			case "folder":
				b.Folders = append(b.Folders, BundleFolder{Path: n.Path, Name: n.Name, Seq: n.Seq})
				walk(n.Children)
			case "request":
				r, ok := res.reqs[n.UID]
				if !ok || r == nil {
					continue
				}
				cp := *r
				// 运行期字段不外带：ExpectHash 是本机读盘时的哈希，BaseRev 是同步基线
				cp.ExpectHash = ""
				cp.BaseRev = 0
				b.Requests = append(b.Requests, &cp)
			case "doc":
				d, ok := res.docs[n.UID]
				if !ok || d == nil {
					continue
				}
				b.Docs = append(b.Docs, *d)
			}
		}
	}
	walk(c.treeFromScan(res))
	return json.MarshalIndent(b, "", "  ")
}

// ImportBundle 把一份集合包导入到 parent 分组下（parent 空 = 集合根）。
//
// 落位规则：包的 collection.name 会建成一层顶层分组，包内目录挂在它下面 ——
// 这样"导出时是哪个集合、有几个目录"在导入方一眼可见，也不会把别人的集合摊平到当前集合根下。
// 集合名为空或非法时退化为直接挂在 parent 下。
func (c *Collection) ImportBundle(parent string, data []byte) (*ImportSummary, error) {
	var b CollectionBundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("集合文件解析失败（应为 %s 的 JSON）: %w", BundleFormat, err)
	}
	if strings.TrimSpace(b.Format) != BundleFormat {
		return nil, fmt.Errorf("不是 api-doc-go 集合文件：format = %q（期望 %q）", b.Format, BundleFormat)
	}
	ver := b.Version
	if ver <= 0 {
		ver = 1 // 早期手写文件可能缺 version，按 1 处理
	}
	if ver > BundleVersion {
		return nil, fmt.Errorf("集合文件版本 %d 高于当前客户端支持的 %d，请升级客户端后再导入", ver, BundleVersion)
	}
	if len(b.Requests) == 0 && len(b.Docs) == 0 && len(b.Folders) == 0 {
		return nil, fmt.Errorf("集合文件里没有内容（目录/请求/文档都为空）")
	}

	base := strings.TrimSpace(parent)
	sum := &ImportSummary{}

	// 顶层：集合名作为一层分组
	if name := strings.TrimSpace(b.Collection.Name); name != "" && validEntryName(name) {
		if err := c.CreateFolder(base, name); err != nil {
			sum.addFailure(fmt.Sprintf("创建集合分组 %s 失败: %v", name, err))
			sum.Skipped++
		} else {
			base = joinRel(base, sanitizeFileName(name))
		}
	}

	// 先建目录：树先序保证父目录先于子目录（空目录也得以保留）
	for _, f := range b.Folders {
		dir := strings.Trim(strings.TrimSpace(filepathToSlash(f.Path)), "/")
		if dir == "" {
			continue
		}
		if err := c.ensureImportDir(joinRel(base, dir)); err != nil {
			sum.addFailure(fmt.Sprintf("创建目录 %s 失败: %v", dir, err))
			sum.Skipped++
		}
	}

	// 再落请求：folder = 目录部分（源 path 去掉文件名）
	for _, r := range b.Requests {
		if r == nil {
			continue
		}
		name := strings.TrimSpace(r.Name)
		folder := joinRel(base, dirOf(r.Path))
		if _, err := c.CreateRequestFromDraft(folder, name, r); err != nil {
			sum.addFailure(fmt.Sprintf("导入请求 %s 失败: %v", name, err))
			sum.Skipped++
			continue
		}
		sum.Imported++
	}

	// 最后落文档：CreateDoc 生成 uid/路径，再写回正文与图标/排序
	for i := range b.Docs {
		d := b.Docs[i]
		name := strings.TrimSpace(d.Name)
		folder := joinRel(base, dirOf(d.Path))
		created, err := c.CreateDoc(name, "", folder)
		if err != nil {
			sum.addFailure(fmt.Sprintf("导入文档 %s 失败: %v", name, err))
			sum.Skipped++
			continue
		}
		created.Content = d.Content
		created.Icon = d.Icon
		created.Seq = d.Seq
		if err := c.SaveDoc(created); err != nil {
			sum.addFailure(fmt.Sprintf("写入文档 %s 失败: %v", name, err))
			sum.Skipped++
			continue
		}
		sum.Imported++
	}
	return sum, nil
}

// ensureImportDir 逐级创建导入用的分组目录（路径可能来自别人的文件，必须走既有校验）。
func (c *Collection) ensureImportDir(rel string) error {
	clean, err := normalizeFolderPath(rel)
	if err != nil {
		return err
	}
	if clean == "" {
		return nil
	}
	parts := strings.Split(clean, "/")
	cur := ""
	for _, p := range parts {
		if err := c.CreateFolder(cur, p); err != nil {
			return err
		}
		cur = joinRel(cur, sanitizeFileName(p))
	}
	return nil
}

// filepathToSlash 只做分隔符归一：包里的路径一律以 / 表示。
func filepathToSlash(p string) string { return strings.ReplaceAll(p, "\\", "/") }

// dirOf 取"目录部分"（入参是以 / 分隔的相对路径，含文件名）。越界路径由下游
// normalizeFolderPath 拒绝，这里不做放行。
func dirOf(p string) string {
	p = strings.TrimSpace(filepathToSlash(p))
	if p == "" {
		return ""
	}
	d := path.Dir(p)
	if d == "." || d == "/" {
		return ""
	}
	return strings.Trim(d, "/")
}
