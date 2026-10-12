package collection

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	share "github.com/leihenshang/api-doc-go-share/collection"
)

// ListAllRequests 列出全部请求（供本地 Mock 匹配用）。
func (c *Collection) ListAllRequests() ([]*Request, error) {
	res, err := c.scan()
	if err != nil {
		return nil, err
	}
	out := make([]*Request, 0, len(res.reqs))
	for _, r := range res.reqs {
		out = append(out, r)
	}
	return out, nil
}

// LatestExample 取该请求最新一条响应示例（无则 nil）。
func (c *Collection) LatestExample(reqUID string) (*ResponseExample, error) {
	list, err := c.ListResponseExamples(reqUID)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(list[0].Response.Body) == "" {
		return nil, nil
	}
	return list[0], nil
}

// SaveConflictCopy 把本地版本另存到 .conflicts/（R8：不带可同步 uid）。
func (c *Collection) SaveConflictCopy(r *Request, serverRev int64, serverPayload []byte) (string, error) {
	if r == nil {
		return "", fmt.Errorf("请求为空")
	}
	dir := filepath.Join(c.Dir, ".conflicts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s.conflict-%d.yml", sanitizeFileName(r.Name), time.Now().UnixMilli())
	full := filepath.Join(dir, name)
	f := r.toFile()
	// 去掉可同步 uid，避免冲突副本被当成普通条目推回
	f.Meta.UID = ""
	f.Extra = map[string]any{
		"conflict_of": r.UID,
		"server_rev":  serverRev,
	}
	if len(serverPayload) > 0 {
		f.Extra["server_payload"] = string(serverPayload)
	}
	data, err := f.Encode()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return "", err
	}
	return full, nil
}

// ConflictItem 一份冲突副本的元信息。
type ConflictItem struct {
	File      string `json:"file"`      // .conflicts/ 下的文件名
	OfUID     string `json:"ofUid"`     // 冲突所属的请求 uid
	Name      string `json:"name"`      // 副本里的请求名
	ServerRev int64  `json:"serverRev"` // 服务端版本号
	CreatedAt int64  `json:"createdAt"` // Unix 毫秒
}

// FieldDiff 一个有差异的字段：本地值 → 服务器值。
type FieldDiff struct {
	Field  string `json:"field"` // name | method | url | docs（前端做字段名国际化）
	Local  string `json:"local"`
	Server string `json:"server"`
}

// ConflictDetail 冲突详情：副本元信息 + 差异字段列表。
type ConflictDetail struct {
	ConflictItem
	Diffs      []FieldDiff `json:"diffs"`
	HasPayload bool        `json:"hasPayload"` // 旧版本遗留副本可能没有服务器快照
}

// conflictPayload 服务端同步载荷的最小子集（collection 层不能依赖 syncengine，避免循环引用）。
type conflictPayload struct {
	Name   string `json:"name"`
	Method string `json:"method"`
	URL    string `json:"url"`
	Extra  string `json:"extra"`
}

// ListConflicts 列出 .conflicts/ 下的冲突副本。
func (c *Collection) ListConflicts() ([]ConflictItem, error) {
	dir := filepath.Join(c.Dir, ".conflicts")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := []ConflictItem{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		if _, item, err := c.readConflictFile(e.Name()); err == nil {
			out = append(out, item)
		}
	}
	return out, nil
}

// readConflictFile 读取并解码一个冲突副本，同时还原其元信息。
func (c *Collection) readConflictFile(file string) (*requestFile, ConflictItem, error) {
	var item ConflictItem
	full := filepath.Join(c.Dir, ".conflicts", filepath.Base(file))
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, item, fmt.Errorf("冲突副本不存在")
	}
	f, err := share.DecodeRequest(data)
	if err != nil {
		return nil, item, fmt.Errorf("冲突副本已损坏：%w", err)
	}
	item.File = filepath.Base(file)
	item.Name = f.Info.Name
	if f.Extra != nil {
		if v, ok := f.Extra["conflict_of"].(string); ok {
			item.OfUID = v
		}
		item.ServerRev = toRev(f.Extra["server_rev"])
	}
	if st, err := os.Stat(full); err == nil {
		item.CreatedAt = st.ModTime().UnixMilli()
	}
	return f, item, nil
}

// ConflictDetail 提取副本里本地版本与服务器版本的差异字段。
func (c *Collection) ConflictDetail(file string) (*ConflictDetail, error) {
	f, item, err := c.readConflictFile(file)
	if err != nil {
		return nil, err
	}
	d := &ConflictDetail{ConflictItem: item, Diffs: []FieldDiff{}}
	sp, ok := f.Extra["server_payload"].(string)
	if !ok || strings.TrimSpace(sp) == "" {
		// 旧副本无服务器快照：HasPayload 留 false，前端只允许保留本地
		return d, nil
	}
	var p conflictPayload
	if err := json.Unmarshal([]byte(sp), &p); err != nil {
		return nil, fmt.Errorf("服务器版本解析失败：%w", err)
	}
	d.HasPayload = true
	localName, localMethod, localURL, localDocs := conflictLocalValues(f)
	// 比较口径必须与 ResolveConflict("remote") 的实际写入完全一致：
	// name/method/url 服务端留空（omitempty）表示未改动；desc 仅在 extra 里存在时才比较。
	if p.Name != "" {
		d.Diffs = appendFieldDiff(d.Diffs, "name", localName, p.Name)
	}
	if p.Method != "" {
		d.Diffs = appendFieldDiff(d.Diffs, "method", strings.ToUpper(localMethod), strings.ToUpper(p.Method))
	}
	if p.URL != "" {
		d.Diffs = appendFieldDiff(d.Diffs, "url", localURL, p.URL)
	}
	if desc, ok := remoteDesc(p.Extra); ok {
		d.Diffs = appendFieldDiff(d.Diffs, "docs", localDocs, desc)
	}
	return d, nil
}

// ResolveConflict 应用冲突取舍（I4）：
//   - "local"  以副本里的本地版本整份还原原文件，base_rev 前移到 server_rev
//     （下轮 push 是正常的新提交，不再重复冲突）
//   - "remote" 按服务端快照改写标量字段并固化为已同步
//   - "copy"   什么都不做（保留副本待手动处理）
func (c *Collection) ResolveConflict(file, choice string) error {
	f, item, err := c.readConflictFile(file)
	if err != nil {
		return err
	}
	full := filepath.Join(c.Dir, ".conflicts", filepath.Base(file))
	switch choice {
	case "local":
		if err := c.applyLocalVersion(f, item); err != nil {
			return err
		}
	case "remote":
		if err := c.applyRemoteVersion(f, item); err != nil {
			return err
		}
	case "copy":
		return nil
	default:
		return fmt.Errorf("未知的取舍方式：%s", choice)
	}
	return c.moveToTrash(full)
}

// applyLocalVersion 用副本（完整本地版本）还原原文件。
func (c *Collection) applyLocalVersion(f *requestFile, item ConflictItem) error {
	if item.OfUID == "" {
		return fmt.Errorf("副本缺少溯源 uid，无法定位原请求")
	}
	r, err := c.ReadRequest(item.OfUID)
	if err != nil {
		return fmt.Errorf("原请求已不存在，无法应用本地版本：%w", err)
	}
	// 副本保存时摘除了 uid；还原 uid 与新基线、删除溯源键后，整份写回原路径
	f.Meta.UID = item.OfUID
	f.Meta.BaseRev = item.ServerRev
	for _, k := range []string{"conflict_of", "server_rev", "server_payload"} {
		delete(f.Extra, k)
	}
	out := fromFile(r.Path, f)
	return c.SaveRequest(out)
}

// applyRemoteVersion 按服务端快照改写原请求（口径同 syncengine.applyAPI：仅 name/method/url/docs）。
func (c *Collection) applyRemoteVersion(f *requestFile, item ConflictItem) error {
	sp, ok := f.Extra["server_payload"].(string)
	if !ok || strings.TrimSpace(sp) == "" {
		return fmt.Errorf("该冲突副本缺少服务器版本快照，无法采用服务器版本")
	}
	var p conflictPayload
	if err := json.Unmarshal([]byte(sp), &p); err != nil {
		return fmt.Errorf("服务器版本解析失败：%w", err)
	}
	if item.OfUID == "" {
		return fmt.Errorf("副本缺少溯源 uid，无法定位原请求")
	}
	r, err := c.ReadRequest(item.OfUID)
	if err != nil {
		return fmt.Errorf("原请求已不存在，无法应用服务器版本：%w", err)
	}
	if p.Name != "" {
		r.Name = p.Name
	}
	if p.Method != "" {
		r.Method = strings.ToUpper(p.Method)
	}
	if p.URL != "" {
		r.URL = p.URL
	}
	// 服务端快照的 extra：说明 / 请求头 / 请求参数 / JSON 体（口径同 syncengine 拉取）
	ApplySyncExtra(r, p.Extra)
	r.BaseRev = item.ServerRev
	if err := c.SaveRequest(r); err != nil {
		return err
	}
	// 固化为已同步：下一轮不再 dirty、不再冲突
	return c.MarkSynced(r.UID, c.FileHashOf(r.Path), item.ServerRev)
}

// conflictLocalValues 从副本取本地版本的四个可同步标量。
func conflictLocalValues(f *requestFile) (name, method, url, docs string) {
	name, docs = f.Info.Name, f.Docs
	if f.Info.Type == TypeGRPC || f.GRPC != nil {
		method = MethodGRPC
		if f.GRPC != nil {
			url = GrpcURL(f.GRPC.Target, f.GRPC.Service, f.GRPC.Method)
		}
		return
	}
	method, url = f.HTTP.Method, f.HTTP.URL
	return
}

// remoteDesc 从服务端载荷的 extra JSON 中取 desc；字段不存在时 ok=false。
func remoteDesc(extraJSON string) (string, bool) {
	if strings.TrimSpace(extraJSON) == "" {
		return "", false
	}
	var m map[string]any
	if json.Unmarshal([]byte(extraJSON), &m) != nil {
		return "", false
	}
	v, ok := m["desc"]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func appendFieldDiff(out []FieldDiff, field, local, server string) []FieldDiff {
	if local == server {
		return out
	}
	return append(out, FieldDiff{Field: field, Local: local, Server: server})
}

func toRev(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	}
	return 0
}

// ExportMarkdown 把集合导出为一份 Markdown 文档（标题层级 = 目录树，请求含方法/URL/参数/头/体/文档）。
func (c *Collection) ExportMarkdown() (string, error) {
	res, err := c.scan()
	if err != nil {
		return "", err
	}
	tree, err := c.Tree()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", c.Name)
	fmt.Fprintf(&b, "> 由 api-doc-go 客户端导出；集合目录 `%s`\n\n", c.Dir)

	var walk func(nodes []*Node, depth int)
	walk = func(nodes []*Node, depth int) {
		for _, n := range nodes {
			if depth > 6 {
				depth = 6
			}
			pad := strings.Repeat("#", depth+2)
			if n.Type == "folder" {
				fmt.Fprintf(&b, "%s %s\n\n", pad, n.Name)
				if n.Children != nil {
					walk(n.Children, depth+1)
				}
				continue
			}
			r, ok := res.reqs[n.UID]
			if !ok {
				continue
			}
			// gRPC 请求按协议分节（G11.6）：无 query / 请求头 / 请求体，改展示目标与方法信息
			if g := r.GRPC; g != nil {
				fmt.Fprintf(&b, "%s %s `GRPC`\n\n", pad, r.Name)
				fmt.Fprintf(&b, "```\n%s\n```\n\n", r.URL)
				fmt.Fprintf(&b, "**服务方法**：`%s/%s`（%s）\n\n", g.Service, g.Method, grpcStreamLabel(g.Stream))
				if strings.TrimSpace(g.Proto) != "" {
					fmt.Fprintf(&b, "**定义**：`%s`\n\n", g.Proto)
				}
				if mode := grpcTLSMode(g.TLS); mode != "" && mode != "none" && mode != "plaintext" {
					fmt.Fprintf(&b, "**连接**：%s\n\n", mode)
				}
				writeMDSection(&b, "Metadata", g.Metadata)
				if strings.TrimSpace(g.Message) != "" {
					body := strings.TrimSpace(g.Message)
					if json.Valid([]byte(body)) {
						fmt.Fprintf(&b, "**请求消息**\n\n```json\n%s\n```\n\n", body)
					} else {
						fmt.Fprintf(&b, "**请求消息**\n\n```\n%s\n```\n\n", body)
					}
				}
				if strings.TrimSpace(r.Docs) != "" {
					fmt.Fprintf(&b, "%s\n\n", strings.TrimSpace(r.Docs))
				}
				fmt.Fprintf(&b, "---\n\n")
				continue
			}
			fmt.Fprintf(&b, "%s %s `%s`\n\n", pad, r.Name, strings.ToUpper(r.Method))
			fmt.Fprintf(&b, "```\n%s\n```\n\n", r.URL)
			writeMDSection(&b, "参数", r.Params)
			writeMDSection(&b, "请求头", r.Headers)
			if r.Body.Type != "" && r.Body.Type != "none" {
				fmt.Fprintf(&b, "**请求体**（%s）\n\n", r.Body.Type)
				raw := r.Body.Raw
				if raw == "" && len(r.Body.Form) > 0 {
					var lines []string
					for _, f := range r.Body.Form {
						if !f.Enabled {
							continue
						}
						lines = append(lines, fmt.Sprintf("- `%s`: `%s`", f.Name, f.Value))
					}
					raw = strings.Join(lines, "\n")
				}
				if strings.TrimSpace(raw) != "" {
					fmt.Fprintf(&b, "```\n%s\n```\n\n", raw)
				}
			}
			if r.Auth != nil && r.Auth.Type != "" && r.Auth.Type != "none" {
				fmt.Fprintf(&b, "**认证**：%s\n\n", r.Auth.Type)
			}
			if strings.TrimSpace(r.Docs) != "" {
				fmt.Fprintf(&b, "%s\n\n", strings.TrimSpace(r.Docs))
			}
			fmt.Fprintf(&b, "---\n\n")
		}
	}
	walk(tree, 0)
	return b.String(), nil
}

func writeMDSection(b *strings.Builder, title string, rows []KV) {
	var live []KV
	for _, r := range rows {
		if r.Enabled && r.Name != "" {
			live = append(live, r)
		}
	}
	if len(live) == 0 {
		return
	}
	fmt.Fprintf(b, "**%s**\n\n", title)
	fmt.Fprintf(b, "| 名称 | 值 | 说明 |\n|---|---|---|\n")
	for _, r := range live {
		// 单元格里的 `|` 必须转义：否则该行会多出一列，整张表列数对不上
		fmt.Fprintf(b, "| `%s` | `%s` | %s |\n", escapePipe(r.Name), escapePipe(r.Value), escapePipe(r.Description))
	}
	fmt.Fprintf(b, "\n")
}

// escapePipe 表格单元格里的竖线（Markdown 表格用 \| 表示字面量竖线）。
func escapePipe(s string) string {
	if !strings.Contains(s, "|") {
		return s
	}
	return strings.ReplaceAll(s, "|", `\|`)
}

// ExportHTML 导出单文件 HTML（内联样式，可直接浏览器打开/分享）。
// 左侧 sticky 的目录树（TOC）按集合树 DFS 先序列出 folder/request，点击跳转到正文标题；
// 正文每个标题带 toc-N 锚点（由 mdToHTML 分配），与 TOC 链接一一对应。
func (c *Collection) ExportHTML() (string, error) {
	md, err := c.ExportMarkdown()
	if err != nil {
		return "", err
	}
	// 轻量 Markdown → HTML（标题/代码块/粗体/表格/分隔线/段落），避免再引依赖
	body := mdToHTML(md)

	tree, err := c.Tree()
	if err != nil {
		return "", err
	}
	// 请求正文（含 Docs）会被 mdToHTML 分配 toc-N 锚点；TOC 只给 folder/request 编号。
	// 因此这里用 scan 拿到每个请求的 Docs，给请求编号后把其 Docs 内的标题数计入计数器，
	// 使 TOC 的 toc-N 与正文实际标题顺序（含文档子标题）严格对应，避免点选错位。
	res, _ := c.scan()

	// TOC：id 分配顺序必须与 mdToHTML 输出正文标题的顺序完全一致
	// （集合名 → 树先序 folder/request，且每个 request 之后紧跟其 Docs 内的标题）。
	// 先消耗 toc-0 给集合名，再 DFS 遍历节点；doc 节点不生成标题，跳过。
	var tocID int
	nextID := func() string {
		id := fmt.Sprintf("toc-%d", tocID)
		tocID++
		return id
	}
	var toc strings.Builder
	toc.WriteString(`<nav class="toc" aria-label="目录">`)
	toc.WriteString(`<div class="toc-title">目录</div>`)
	// 目录搜索：输入时过滤并自动展开命中节点的祖先（见底部 <script>）
	toc.WriteString(`<input type="search" class="toc-search" placeholder="搜索目录…" aria-label="搜索目录">`)
	// 显示全部：退出「单节展示」模式，右侧恢复列出所有 API
	toc.WriteString(`<button type="button" class="toc-all">显示全部</button>`)
	toc.WriteString(`<ul class="toc-list">`)
	// 集合名（对应正文第一个标题 toc-0）
	toc.WriteString(`<li class="toc-root"><a href="#`)
	toc.WriteString(nextID())
	toc.WriteString(`">`)
	toc.WriteString(html.EscapeString(c.Name))
	toc.WriteString(`</a></li>`)

	// walkTOC 生成可折叠的目录树；isTop 标记顶层，仅第一个顶层目录默认展开（data-default / aria-expanded），
	// 其余目录默认折叠，满足「默认仅第一个目录打开」的需求。doc 节点不进正文标题，TOC 也跳过。
	var walkTOC func(nodes []*Node, depth int, isTop bool)
	walkTOC = func(nodes []*Node, depth int, isTop bool) {
		for i, n := range nodes {
			// doc 节点不进正文标题，TOC 也跳过（与 ExportMarkdown 行为一致）
			if n.Type == "doc" {
				continue
			}
			if n.Type == "folder" {
				id := nextID()
				// 仅第一个顶层目录默认展开；嵌套目录一律默认折叠
				open := isTop && i == 0
				toc.WriteString(`<li class="toc-folder" data-default="`)
				toc.WriteString(strconv.FormatBool(open))
				toc.WriteString(`" aria-expanded="`)
				toc.WriteString(strconv.FormatBool(open))
				toc.WriteString(`">`)
				// 折叠/展开开关（点击切换 aria-expanded，不触发跳转）
				toc.WriteString(`<span class="toc-toggle" role="button" tabindex="0" aria-label="折叠或展开"></span>`)
				toc.WriteString(`<a href="#`)
				toc.WriteString(id)
				toc.WriteString(`">`)
				toc.WriteString(html.EscapeString(n.Name))
				toc.WriteString(`</a>`)
				if len(n.Children) > 0 {
					toc.WriteString(`<ul>`)
					walkTOC(n.Children, depth+1, false)
					toc.WriteString(`</ul>`)
				}
				toc.WriteString(`</li>`)
				continue
			}
		// request
		id := nextID()
		toc.WriteString(`<li class="toc-request"><a href="#`)
		toc.WriteString(id)
		toc.WriteString(`">`)
		toc.WriteString(html.EscapeString(n.Name))
		toc.WriteString(`</a></li>`)
		// 该请求正文之后会紧跟其 Docs 内的标题（被 mdToHTML 占用 toc-N），
		// 必须把这部分标题数计入计数器，后续节点的 toc-N 才能与正文对齐。
		if res != nil {
			if r, ok := res.reqs[n.UID]; ok {
				tocID += countDocHeadings(r.Docs)
			}
		}
		}
	}
	walkTOC(tree, 0, true)
	toc.WriteString(`</ul></nav>`)

	var b strings.Builder
	b.WriteString(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>`)
	b.WriteString(html.EscapeString(c.Name))
	b.WriteString(`</title>
<style>
  :root { color-scheme: light; }
  * { box-sizing: border-box; }
  body { font: 14px/1.7 -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Noto Sans SC", sans-serif;
         margin: 0; padding: 0; color: #202124; background: #fff; }
  .layout { display: flex; gap: 24px; max-width: 1100px; margin: 0 auto; padding: 32px 24px; align-items: flex-start; }
  /* 左侧 sticky 目录树：窄栏、浅底卡片，滚动时固定 */
  .toc { flex: 0 0 240px; position: sticky; top: 16px; max-height: calc(100vh - 32px); overflow: auto;
         border: 1px solid #e3e3e3; border-radius: 10px; background: #f5f6f8; padding: 12px 14px; font-size: 13px; }
  .toc-title { font-weight: 600; color: #202124; margin-bottom: 8px; }
  /* 目录搜索框 */
  .toc-search { width: 100%; box-sizing: border-box; margin: 0 0 10px; padding: 6px 8px;
    border: 1px solid #e3e3e3; border-radius: 6px; font-size: 13px; font-family: inherit;
    color: #202124; background: #fff; outline: none; }
  .toc-search:focus { border-color: #18a058; }
  /* 显示全部：退出单节展示模式 */
  .toc-all { width: 100%; box-sizing: border-box; margin: 0 0 10px; padding: 5px 8px;
    border: 1px solid #e3e3e3; border-radius: 6px; background: #fff; color: #18a058;
    font-size: 12px; font-family: inherit; cursor: pointer; }
  .toc-all:hover { border-color: #18a058; background: #f0faf3; }
  .toc-list, .toc ul { list-style: none; margin: 0; padding: 0; }
  /* 嵌套目录：保留左侧缩进，并加树形连接线（风格对齐 client 侧栏树） */
  .toc ul { padding-left: 14px; }
  .toc li { margin: 2px 0; }
  /* 连接线：每个嵌套 li 画一段竖线（拼接成连续竖线），再用 ::after 画短横线连到文字；
     末项的竖线只到横线（中线），形成 └ 形，不向下多余延伸 */
  .toc-list ul > li { position: relative; }
  .toc-list ul > li::before {
    content: ""; position: absolute; top: -3px; bottom: 0; left: -8px;
    border-left: 1px solid #e3e3e3;
  }
  .toc-list ul > li::after {
    content: ""; position: absolute; top: 11px; left: -8px; width: 8px;
    border-top: 1px solid #e3e3e3;
  }
  .toc-list ul > li:last-child::before { bottom: auto; height: 14px; }
  /* 折叠/展开：文件夹默认展开（aria-expanded=true）才显示子列表；折叠时隐藏 */
  .toc-folder[aria-expanded="false"] > ul { display: none; }
  /* 文件夹开关：右侧三角形，展开时旋转 90° 指向下方；
     用 -1px 微调使其与文字视觉居中（三角形几何中心略低于文本中线） */
  .toc-toggle { display: inline-block; width: 0; height: 0; margin: 0 6px 0 2px;
    border-style: solid; border-width: 4px 0 4px 6px; border-color: transparent transparent transparent #6b7280;
    vertical-align: middle; position: relative; top: -1px; cursor: pointer; transition: transform .15s ease; }
  .toc-folder[aria-expanded="true"] > .toc-toggle { transform: rotate(90deg); }
  .toc a { color: #202124; text-decoration: none; display: block; padding: 3px 6px; border-radius: 4px; line-height: 1.4; }
  .toc a:hover { background: #18a058; color: #fff; }
  /* 必须放在 .toc a 的块级规则之后并用更高优先级覆盖，否则 folder 标题会被
     覆盖回 display:block，导致开关与文字各占一行（错位） */
  .toc li.toc-folder > a { display: inline; }
  .toc-root > a { font-weight: 600; }
  .toc-folder > a { font-weight: 600; color: #18a058; }
  .toc a:target, .toc a.current { background: #18a058; color: #fff; }
  .content { flex: 1 1 auto; min-width: 0; }
  h1,h2,h3,h4,h5,h6 { margin: 1.4em 0 0.6em; font-weight: 600; line-height: 1.3; scroll-margin-top: 16px; }
  h1 { font-size: 1.8em; border-bottom: 1px solid #e3e3e3; padding-bottom: 0.3em; }
  code { font-family: "JetBrains Mono", ui-monospace, Menlo, Consolas, monospace;
         background: #f5f6f8; padding: 0.15em 0.4em; border-radius: 4px; font-size: 0.92em; }
  pre { background: #f5f6f8; padding: 12px 14px; border-radius: 8px; overflow: auto; }
  pre code { background: none; padding: 0; }
  table { border-collapse: collapse; width: 100%; margin: 0.6em 0 1em; font-size: 13px; }
  th, td { border: 1px solid #e3e3e3; padding: 6px 10px; text-align: left; }
  th { background: #fafafa; font-weight: 500; }
  hr { border: none; border-top: 1px solid #e3e3e3; margin: 1.6em 0; }
  blockquote { margin: 0.6em 0; padding: 0.4em 1em; border-left: 3px solid #18a058; background: #f5f6f8; color: #555; }
  strong { font-weight: 600; }
  /* 窄屏：目录树移到正文上方、不再 sticky */
  @media (max-width: 720px) {
    .layout { flex-direction: column; gap: 12px; padding: 16px 14px; }
    .toc { flex: 0 0 auto; position: static; max-height: none; width: 100%; }
  }
</style>
</head>
<body>
`)
	b.WriteString(`<div class="layout">`)
	b.WriteString(toc.String())
	b.WriteString(`<main class="content">`)
	b.WriteString(body)
	b.WriteString(`</main></div>`)
	b.WriteString(`
<script>
(function(){
  var list = document.querySelector('.toc-list');
  var box = document.querySelector('.toc-search');
  if(!list || !box) return;

  // 折叠 / 展开：点击开关切换 aria-expanded，CSS 据此隐藏子列表（点击不会触发跳转）
  list.addEventListener('click', function(e){
    var t = e.target;
    if(t && t.classList && t.classList.contains('toc-toggle')){
      var li = t.parentElement;
      li.setAttribute('aria-expanded', li.getAttribute('aria-expanded') === 'true' ? 'false' : 'true');
    }
  });
  list.addEventListener('keydown', function(e){
    var t = e.target;
    if(t && t.classList && t.classList.contains('toc-toggle') && (e.key === 'Enter' || e.key === ' ')){
      e.preventDefault();
      t.click();
    }
  });

  // 搜索：隐藏不匹配条目，自动展开命中节点的祖先目录，命中目录时一并展开其子节点
  function reset(){
    var lis = list.querySelectorAll('li');
    for(var i=0;i<lis.length;i++){
      lis[i].style.display = '';
      if(lis[i].classList.contains('toc-folder')){
        lis[i].setAttribute('aria-expanded', lis[i].getAttribute('data-default') || 'false');
      }
    }
  }
  function filter(q){
    if(!q){ reset(); return; }
    q = q.toLowerCase();
    var all = list.querySelectorAll('li');
    var i, a, node, cur, ul, parent, k, desc;
    for(i=0;i<all.length;i++){ all[i].dataset.shown = ''; }
    for(i=0;i<all.length;i++){
      a = all[i].querySelector(':scope > a');
      if(a && a.textContent.toLowerCase().indexOf(q) !== -1){
        node = all[i];
        node.dataset.shown = '1';
        if(node.classList.contains('toc-folder')){
          node.setAttribute('aria-expanded', 'true');
          desc = node.querySelectorAll('li');
          for(k=0;k<desc.length;k++){ desc[k].dataset.shown = '1'; }
        }
        cur = node;
        while(cur){
          ul = cur.parentElement;
          parent = ul ? ul.parentElement : null;
          if(parent && parent !== list && parent.tagName === 'LI'){
            parent.dataset.shown = '1';
            if(parent.classList.contains('toc-folder')) parent.setAttribute('aria-expanded', 'true');
            cur = parent;
          } else { cur = null; }
        }
      }
    }
    for(i=0;i<all.length;i++){
      all[i].style.display = all[i].dataset.shown ? '' : 'none';
    }
  }
  box.addEventListener('input', function(){ filter(box.value.trim()); });

  // 单节展示：点击目录项时，右侧仅显示该项对应的 API（含其下子节点），其余隐藏。
  // 这样导出的 HTML 不再一屏铺满全部 API，而是像文档站一样「左侧选、右侧看一节」。
  var content = document.querySelector('.content');
  var tocLinks = list.querySelectorAll('.toc a');

  function clearCurrent(){
    for(var i=0;i<tocLinks.length;i++){ tocLinks[i].classList.remove('current'); }
  }
  function showAll(){
    if(!content) return;
    var kids = content.children;
    for(var i=0;i<kids.length;i++){ kids[i].style.display = ''; }
    clearCurrent();
  }
  // 仅展示 id 对应的那一节：从目标标题起，到下一个同级或更高级标题为止（含其间的所有子节点）。
  function showSection(id){
    if(!content) return;
    var target = document.getElementById(id);
    if(!target) return;
    var lvl = parseInt(target.tagName.charAt(1), 10); // 1..6
    var nodes = [];
    var sib = target;
    while(sib){
      nodes.push(sib);
      sib = sib.nextElementSibling;
      if(sib && sib.tagName.charAt(0) === 'H'){
        var nlvl = parseInt(sib.tagName.charAt(1), 10);
        if(nlvl <= lvl) break; // 遇到同级或更高级标题 => 超出本节范围
      }
    }
    var kids = content.children;
    for(var i=0;i<kids.length;i++){ kids[i].style.display = 'none'; }
    for(var j=0;j<nodes.length;j++){ nodes[j].style.display = ''; }
    clearCurrent();
    var cur = document.querySelector('.toc a[href="#' + id + '"]');
    if(cur) cur.classList.add('current');
    if(target.scrollIntoView) target.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }

  // 点击目录链接：目录(fodler)只在左侧展开/收起子树，右侧不展示其下 API；
  // 只有点击具体的 API(request) 才在右侧单节展示，集合根点击可看全部。
  for(var li=0; li<tocLinks.length; li++){
    tocLinks[li].addEventListener('click', function(e){
      var href = this.getAttribute('href') || '';
      if(href.charAt(0) !== '#') return;
      e.preventDefault();
      var item = this.parentElement; // 该链接所在的 <li>
      if(item && item.classList.contains('toc-folder')){
        // 目录：仅切换 TOC 展开/折叠，便于展开后点选其下的 API；右侧内容不变
        var expanded = item.getAttribute('aria-expanded') === 'true';
        item.setAttribute('aria-expanded', expanded ? 'false' : 'true');
        return;
      }
      showSection(href.slice(1));
    });
  }
  // 显示全部按钮
  var allBtn = document.querySelector('.toc-all');
  if(allBtn){ allBtn.addEventListener('click', showAll); }

  // 默认进入单节展示：优先展示第一个 API（request），避免一屏铺满；无 API 时退回第一个标题
  var firstReq = list.querySelector('.toc-request a');
  if(firstReq && firstReq.getAttribute('href')){
    var fid = firstReq.getAttribute('href').slice(1);
    if(document.getElementById(fid)){ showSection(fid); }
  } else {
    var firstHeading = content ? content.querySelector('h2,h3,h4,h5,h6') : null;
    if(firstHeading && firstHeading.id){ showSection(firstHeading.id); }
  }
})();
</script>
</body>
</html>
`)
	return b.String(), nil
}

// mdToHTML 覆盖导出用到的 Markdown 子集（# 标题、``` 代码、**粗体**、表格、---、> 引用、段落）。
// 每个标题按顺序分配 id="toc-N"（N 从 0 自增），供 HTML 导出的目录树（TOC）锚点跳转。
func mdToHTML(src string) string {
	lines := strings.Split(src, "\n")
	var b strings.Builder
	inCode := false
	inTable := false
	inQuote := false
	// 标题锚点计数器：与 ExportHTML 生成 TOC 的顺序一致（集合名 → 树先序 folder/request）。
	hID := 0

	closeTable := func() {
		if inTable {
			b.WriteString("</table>\n")
			inTable = false
		}
	}
	closeQuote := func() {
		if inQuote {
			b.WriteString("</blockquote>\n")
			inQuote = false
		}
	}

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trim := strings.TrimSpace(line)

		if strings.HasPrefix(trim, "```") {
			closeTable()
			closeQuote()
			if !inCode {
				b.WriteString("<pre><code>")
				inCode = true
			} else {
				b.WriteString("</code></pre>\n")
				inCode = false
			}
			continue
		}
		if inCode {
			b.WriteString(html.EscapeString(line))
			b.WriteString("\n")
			continue
		}

		if trim == "" {
			closeTable()
			closeQuote()
			continue
		}
		if trim == "---" {
			closeTable()
			closeQuote()
			b.WriteString("<hr>\n")
			continue
		}
		if strings.HasPrefix(trim, "#") {
			closeTable()
			closeQuote()
			level := 0
			for level < len(trim) && trim[level] == '#' {
				level++
			}
			if level > 6 {
				level = 6
			}
			text := strings.TrimSpace(trim[level:])
			id := fmt.Sprintf("toc-%d", hID)
			hID++
			fmt.Fprintf(&b, "<h%d id=\"%s\">%s</h%d>\n", level, id, inlineMD(text), level)
			continue
		}
		if strings.HasPrefix(trim, ">") {
			closeTable()
			if !inQuote {
				b.WriteString("<blockquote>\n")
				inQuote = true
			}
			fmt.Fprintf(&b, "<p>%s</p>\n", inlineMD(strings.TrimSpace(strings.TrimPrefix(trim, ">"))))
			continue
		}
		closeQuote()
		// 表格
		if strings.HasPrefix(trim, "|") {
			cells := splitRow(trim)
			// 分隔行 |---|---|：必须整行都是表格分隔语法，
			// 只判断「下一行含 ---」会把普通正文误当表头（后续内容被吞进表里）
			if i+1 < len(lines) && isTableSeparator(lines[i+1]) {
				if !inTable {
					b.WriteString("<table>\n<thead><tr>")
					for _, c := range cells {
						fmt.Fprintf(&b, "<th>%s</th>", inlineMD(c))
					}
					b.WriteString("</tr></thead>\n<tbody>\n")
					inTable = true
				}
				i++ // 跳过分隔行
				continue
			}
			if inTable {
				b.WriteString("<tr>")
				for _, c := range cells {
					fmt.Fprintf(&b, "<td>%s</td>", inlineMD(c))
				}
				b.WriteString("</tr>\n")
			}
			continue
		}
		closeTable()
		fmt.Fprintf(&b, "<p>%s</p>\n", inlineMD(trim))
	}
	closeTable()
	closeQuote()
	if inCode {
		b.WriteString("</code></pre>\n")
	}
	return b.String()
}

// countDocHeadings 统计一段 Markdown 里「标题」的数量，规则与 mdToHTML 完全一致
// （遇到 ``` 围栏时跳过其内部内容；以 # 开头的行算一个标题，最多 6 级）。
// 用途：导出 HTML 时，请求的正文（含 Docs）会被 mdToHTML 分配 toc-N 锚点，
// 而 TOC 只给 folder/request 编号；若 Docs 内含有标题，会占用 toc-N 序号使两者错位。
// 因此给请求编号后，必须把其 Docs 内的标题数也计入计数器，保证 TOC 链接与正文锚点严格对应。
func countDocHeadings(md string) int {
	if strings.TrimSpace(md) == "" {
		return 0
	}
	lines := strings.Split(md, "\n")
	n := 0
	inCode := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			continue
		}
		if strings.HasPrefix(trim, "#") {
			n++
		}
	}
	return n
}

// splitRow 按未转义的 `|` 切分表格行（支持 \| 表示字面量竖线）。
func splitRow(row string) []string {
	row = strings.Trim(row, "|")
	var (
		out []string
		cur strings.Builder
	)
	for i := 0; i < len(row); i++ {
		switch {
		case row[i] == '\\' && i+1 < len(row) && row[i+1] == '|':
			cur.WriteByte('|')
			i++
		case row[i] == '|':
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(row[i])
		}
	}
	out = append(out, strings.TrimSpace(cur.String()))
	return out
}

// isTableSeparator 判断一行是否为 Markdown 表格的分隔行（|---|---|）。
func isTableSeparator(line string) bool {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "|") || !strings.Contains(t, "-") {
		return false
	}
	for _, r := range t {
		switch r {
		case '|', '-', ':', ' ', '\t':
		default:
			return false
		}
	}
	return true
}

// inlineMD 处理 `code` 与 **bold**（导出用的最小集）。
//
// 必须一次扫描、按 code span 优先：先做全局替换会让 `**` 的配对跨越 code span，
// 产出交叉标签（<code><strong>a</code> b</strong>），奇数个反引号还会留下未闭合的 <code>。
// 未闭合的 ` / ** 一律按字面量输出。
func inlineMD(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		switch {
		case s[i] == '`':
			if end := strings.IndexByte(s[i+1:], '`'); end >= 0 {
				b.WriteString("<code>")
				b.WriteString(html.EscapeString(s[i+1 : i+1+end]))
				b.WriteString("</code>")
				i += end + 2
				continue
			}
			b.WriteString(html.EscapeString("`"))
			i++
		case strings.HasPrefix(s[i:], "**"):
			if end := strings.Index(s[i+2:], "**"); end >= 0 {
				b.WriteString("<strong>")
				b.WriteString(html.EscapeString(s[i+2 : i+2+end]))
				b.WriteString("</strong>")
				i += end + 4
				continue
			}
			b.WriteString(html.EscapeString("**"))
			i += 2
		default:
			j := i + 1
			for j < len(s) && s[j] != '`' && !strings.HasPrefix(s[j:], "**") {
				j++
			}
			b.WriteString(html.EscapeString(s[i:j]))
			i = j
		}
	}
	return b.String()
}

// grpcStreamLabel 流式形态的中文标注（导出文档用）。
func grpcStreamLabel(stream string) string {
	switch strings.ToLower(strings.TrimSpace(stream)) {
	case "server":
		return "server（服务端流）"
	case "client":
		return "client（客户端流）"
	case "bidi":
		return "bidi（双向流）"
	default:
		return "unary（一元）"
	}
}

// grpcTLSMode 连接安全模式（nil 视为明文）。
func grpcTLSMode(t *GrpcTLS) string {
	if t == nil {
		return "plaintext"
	}
	if strings.TrimSpace(t.Mode) == "" {
		return "plaintext"
	}
	return t.Mode
}
