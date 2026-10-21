package collection

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
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
	File string `json:"file"` // .conflicts/ 下的文件名
	// Kind 副本类型：request（接口，字段级副本）| doc | example | env（整文件快照）。
	// 旧版本只有接口副本，缺省按 request 处理。
	Kind      string `json:"kind"`
	OfUID     string `json:"ofUid"`     // 冲突所属条目的 uid
	Name      string `json:"name"`      // 副本里的条目名
	ServerRev int64  `json:"serverRev"` // 服务端版本号
	CreatedAt int64  `json:"createdAt"` // Unix 毫秒
}

// 冲突详情的渲染方式：字段级差异 / 整文件快照。
const (
	ConflictModeFields   = "fields"
	ConflictModeSnapshot = "snapshot"
)

// FieldDiff 一个有差异的字段：本地值 → 服务器值。
type FieldDiff struct {
	Field  string `json:"field"` // name | method | url | docs（前端做字段名国际化）
	Local  string `json:"local"`
	Server string `json:"server"`
}

// ConflictDetail 冲突详情：副本元信息 + 差异字段列表。
type ConflictDetail struct {
	ConflictItem
	// Mode 渲染方式：缺省（空）= fields（接口字段级）；snapshot = 整文件快照。
	Mode       string      `json:"mode,omitempty"`
	Diffs      []FieldDiff `json:"diffs"`
	HasPayload bool        `json:"hasPayload"` // 旧版本遗留副本可能没有服务器快照
	// Preview / RemotePreview 仅 snapshot 模式使用：本地版本片段与远端版本摘要。
	Preview       string `json:"preview,omitempty"`
	RemotePreview string `json:"remotePreview,omitempty"`
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
			continue
		}
		// 非接口条目（文档 / 示例 / 环境）的整文件快照：同一目录、同一列表，状态栏才能计数
		if _, item, err := c.readSnapshotFile(e.Name()); err == nil {
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
	// 通用快照（doc/example/env）也能被 yaml 解成「空请求 + 一堆未知字段」：
	// 必须在这里拒掉，否则快照会被当成一条没有名字的接口副本展示。
	if f.Info.Name == "" && f.Info.Type == "" && f.Meta.UID == "" && f.Extra != nil {
		if _, ok := f.Extra["kind"]; ok {
			return nil, item, fmt.Errorf("不是接口冲突副本")
		}
	}
	item.Kind = "request"
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
// 文档 / 示例 / 环境是整文件快照（mode=snapshot），只给预览，不做字段级 diff。
func (c *Collection) ConflictDetail(file string) (*ConflictDetail, error) {
	if d, err := c.snapshotDetail(file); err == nil {
		return d, nil
	}
	f, item, err := c.readConflictFile(file)
	if err != nil {
		return nil, err
	}
	d := &ConflictDetail{ConflictItem: item, Mode: ConflictModeFields, Diffs: []FieldDiff{}}
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
//
// 文档 / 示例 / 环境的整文件快照走另一条路径：local = 原样写回本地文件，
// remote = 丢弃副本（远端版本已在 pull 时落地）。
func (c *Collection) ResolveConflict(file, choice string) error {
	if snap, _, err := c.readSnapshotFile(file); err == nil {
		return c.resolveSnapshot(snap, file, choice)
	}
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
	return c.exportMarkdown(false)
}

// exportMarkdown 生成集合 Markdown。
// shiftDocs=true（HTML 导出用）：把每个请求 Docs 内的标题整体降一级、嵌到请求标题之下 ——
// HTML 单节展示按标题级别圈定一节，Docs 若与请求标题同级（生成的 Docs 都是 ## 起）会被整段排除，
// 用户点开 API 看不到文档内容；降级后 Docs 连同其响应字段/示例小节都归属于该请求。
// 同时标题级别在该模式下封顶 5，给 Docs 留出 h6 的位置。
func (c *Collection) exportMarkdown(shiftDocs bool) (string, error) {
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
	walkMDTree(&b, tree, res, shiftDocs, 0)
	return b.String(), nil
}

// walkMDTree 按集合树先序输出 Markdown：folder 只出标题并递归，request 交给 writeMDRequest。
// 标题级别由 depth 推导（depth+2），shiftDocs 时封顶 5 —— 给 Docs 留出再深一级（h6）的位置，
// 否则 HTML 单节展示按标题级别圈定一节时会把 Docs 整段排除在节外。
func walkMDTree(b *strings.Builder, nodes []*Node, res *scanResult, shiftDocs bool, depth int) {
	if depth > 6 {
		depth = 6
	}
	level := depth + 2
	if shiftDocs && level > 5 {
		level = 5
	}
	pad := strings.Repeat("#", level)
	for _, n := range nodes {
		if n.Type == "folder" {
			fmt.Fprintf(b, "%s %s\n\n", pad, n.Name)
			if n.Children != nil {
				walkMDTree(b, n.Children, res, shiftDocs, depth+1)
			}
			continue
		}
		// 文档条目（集合里的 .md）：此前落到 res.reqs 查不到就 continue，导出直接把它丢了 ——
		// 表现是「导出的 md/html 里只有接口，一篇文档都没有」（B19 让 .md 进了树，导出没跟上）。
		if n.Type == "doc" {
			d, ok := res.docs[n.UID]
			if !ok {
				continue
			}
			title := d.Name
			if d.Icon != "" {
				title = d.Icon + " " + title
			}
			fmt.Fprintf(b, "%s %s\n\n", pad, title)
			if body := strings.TrimSpace(d.Content); body != "" {
				if shiftDocs {
					// 与请求附带的 Docs 同口径：整体降一级，HTML 单节展示时才归属到这篇文档之下。
					body = shiftHeadings(body, level+1)
				}
				fmt.Fprintf(b, "%s\n\n", body)
			}
			continue
		}
		r, ok := res.reqs[n.UID]
		if !ok {
			continue
		}
		writeMDRequest(b, r, pad, level, shiftDocs)
	}
}

// writeMDRequest 输出单个请求：协议相关分节（gRPC / HTTP）→ 文档段 → 分隔线。
// 响应字段文档（respdoc）只属于 HTTP 请求，故放在 HTTP 分支内。
func writeMDRequest(b *strings.Builder, r *Request, pad string, level int, shiftDocs bool) {
	if r.GRPC != nil {
		writeMDGRPCRequest(b, r, pad, shiftDocs)
	} else {
		writeMDHTTPRequest(b, r, pad, shiftDocs)
		appendMDRespDoc(b, r.RespDoc)
	}
	appendMDDocs(b, r.Docs, level, shiftDocs)
	fmt.Fprintf(b, "---\n\n")
}

// writeMDGRPCRequest 输出 gRPC 请求（G11.6）：无 query / 请求头 / 请求体，
// 改展示目标地址、服务方法、proto 定义、连接模式与 Metadata。
func writeMDGRPCRequest(b *strings.Builder, r *Request, pad string, shiftDocs bool) {
	g := r.GRPC
	// HTML 导出用方法徽标前置（与集合树 / TOC 同源同色）；Markdown 导出保持纯文本
	if shiftDocs {
		fmt.Fprintf(b, "%s %s %s\n\n", pad, methodBadge("GRPC"), r.Name)
	} else {
		fmt.Fprintf(b, "%s %s `GRPC`\n\n", pad, r.Name)
	}
	fmt.Fprintf(b, "```\n%s\n```\n\n", r.URL)
	fmt.Fprintf(b, "**服务方法**：`%s/%s`（%s）\n\n", g.Service, g.Method, grpcStreamLabel(g.Stream))
	if strings.TrimSpace(g.Proto) != "" {
		fmt.Fprintf(b, "**定义**：`%s`\n\n", g.Proto)
	}
	if mode := grpcTLSMode(g.TLS); mode != "" && mode != "none" && mode != "plaintext" {
		fmt.Fprintf(b, "**连接**：%s\n\n", mode)
	}
	writeMDSection(b, "Metadata", g.Metadata)
	writeMDMessage(b, g.Message)
}

// writeMDMessage 输出 gRPC 请求消息：合法 JSON 用 json 代码块，否则退回普通代码块。
func writeMDMessage(b *strings.Builder, message string) {
	msg := strings.TrimSpace(message)
	if msg == "" {
		return
	}
	lang := ""
	if json.Valid([]byte(msg)) {
		lang = "json"
	}
	fmt.Fprintf(b, "**请求消息**\n\n```%s\n%s\n```\n\n", lang, msg)
}

// writeMDHTTPRequest 输出 HTTP 请求：地址、参数、请求头、请求体与认证。
// 标题形态两种：HTML 导出用方法徽标前置，Markdown 导出用「名称 + 尾部 METHOD 行内码」
// （纯文本工具链友好）。
func writeMDHTTPRequest(b *strings.Builder, r *Request, pad string, shiftDocs bool) {
	if shiftDocs {
		fmt.Fprintf(b, "%s %s %s\n\n", pad, methodBadge(r.Method), r.Name)
	} else {
		fmt.Fprintf(b, "%s %s `%s`\n\n", pad, r.Name, strings.ToUpper(r.Method))
	}
	fmt.Fprintf(b, "```\n%s\n```\n\n", r.URL)
	writeMDSection(b, "参数", r.Params)
	writeMDSection(b, "请求头", r.Headers)
	writeMDBody(b, r.Body)
	if r.Auth != nil && r.Auth.Type != "" && r.Auth.Type != "none" {
		fmt.Fprintf(b, "**认证**：%s\n\n", r.Auth.Type)
	}
}

// writeMDBody 输出请求体：原始内容（JSON/文本）以代码块展示，表单字段另起一张表
// 「分开展示」，与 URL「参数」区分 —— 同一请求章节内两张独立表格便于对照阅读。
func writeMDBody(b *strings.Builder, body Body) {
	if body.Type == "" || body.Type == "none" {
		return
	}
	// 空判断用 TrimSpace，但写入保持原文（含首尾空白）：与既有输出逐字一致
	raw := body.Raw
	var formRows []KV
	for _, f := range body.Form {
		if !f.Enabled || f.Name == "" {
			continue
		}
		formRows = append(formRows, KV{Name: f.Name, Value: f.Value, Enabled: true})
	}
	if len(formRows) == 0 {
		if strings.TrimSpace(raw) != "" {
			fmt.Fprintf(b, "**请求体**（%s）\n\n```\n%s\n```\n\n", body.Type, raw)
		}
		return
	}
	fmt.Fprintf(b, "**请求体**（%s）\n\n", body.Type)
	if strings.TrimSpace(raw) != "" {
		fmt.Fprintf(b, "```\n%s\n```\n\n", raw)
	}
	writeMDSection(b, "请求体参数", formRows)
}

// appendMDRespDoc 补齐响应字段文档（respdoc）：返回类型 / 字段表 / 样例 / 响应头（item 9）。
func appendMDRespDoc(b *strings.Builder, d *ResponseDoc) {
	if d == nil {
		return
	}
	if strings.TrimSpace(d.Type) != "" {
		fmt.Fprintf(b, "**返回类型**：%s\n\n", strings.TrimSpace(d.Type))
	}
	if len(d.Params) > 0 {
		fmt.Fprintf(b, "**响应字段**\n\n| 参数名 | 必选 | 类型 | 说明 | 示例 |\n|---|---|---|---|---|\n")
		for _, p := range d.Params {
			req := "否"
			if p.Required {
				req = "是"
			}
			fmt.Fprintf(b, "| `%s` | %s | %s | %s | %s |\n",
				escapePipe(p.FieldName), req, escapePipe(p.Type), escapePipe(p.Description), escapePipe(p.Example))
		}
		b.WriteString("\n")
	}
	if strings.TrimSpace(d.Sample) != "" {
		fmt.Fprintf(b, "**响应样例**\n\n```json\n%s\n```\n\n", strings.TrimSpace(d.Sample))
	}
	if len(d.Headers) > 0 {
		writeMDSection(b, "响应头", d.Headers)
	}
}

// appendMDDocs 附加请求文档。item 9：「文档」放在所有生成内容之下，并用分隔线 +
// 标题 label 与生成内容明显分界；HTML 导出（shiftDocs）时整体降级嵌到请求标题之下。
func appendMDDocs(b *strings.Builder, docs string, level int, shiftDocs bool) {
	doc := strings.TrimSpace(docs)
	if doc == "" {
		return
	}
	if shiftDocs {
		doc = shiftHeadings(doc, level+1)
	}
	b.WriteString("\n---\n\n**文档**\n\n")
	fmt.Fprintf(b, "%s\n\n", doc)
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
// 右侧 sticky 的目录树（TOC）按集合树 DFS 先序列出 folder/request，点击跳转到正文标题；
// 正文每个标题带 toc-N 锚点（由 mdToHTML 分配），与 TOC 链接一一对应。
// ExportHTML 导出单文件 HTML（内联样式，可直接浏览器打开/分享）。
// 右侧 sticky 的目录树（TOC）按集合树 DFS 先序列出 folder/request，点击跳转到正文标题；
// 正文每个标题带 toc-N 锚点（由 mdToHTML 分配），与 TOC 链接一一对应。
//
// 文档骨架（内联 CSS 与交互脚本）抽到 export_template.go：单文件交付必须内联资源，
// 但把 ~300 行样式/脚本留在函数体里会让本函数无法维护，故按块拆成包级常量，
// 这里只保留「取数据 → 装配」。
func (c *Collection) ExportHTML() (string, error) {
	// Docs 标题降级到请求标题之下（否则单节展示会把 Docs 整段排除在 API 小节之外）
	md, err := c.exportMarkdown(true)
	if err != nil {
		return "", err
	}
	// 轻量 Markdown → HTML（标题/代码块/粗体/表格/分隔线/段落），避免再引依赖
	body := mdToHTML(md)

	tree, err := c.Tree()
	if err != nil {
		return "", err
	}
	// scan 拿到每个请求的 Docs：TOC 需为请求之后的文档标题预留 toc-N（见 buildExportTOC）
	res, _ := c.scan()

	var b strings.Builder
	b.WriteString(htmlDocHead)
	b.WriteString(html.EscapeString(c.Name))
	b.WriteString(htmlStyleBlock)
	b.WriteString(`<div class="layout">`)
	b.WriteString(buildExportTOC(c.Name, tree, res))
	b.WriteString(`<main class="content">`)
	b.WriteString(body)
	b.WriteString(`</main>`)
	// 当前请求的右侧大纲：.ol-list 由 JS 在单节展示时按小节/生成标签动态填充。
	// 头部带「收起」按钮；收起后 .ol 整块 display:none（正文自动铺满），
	// 窗口右缘露出 .ol-open 把手用于展开 —— 两者的显隐都由 .layout 上的类控制（见模板里的 olApply）。
	b.WriteString(`<nav class="ol" aria-label="当前页目录">`)
	b.WriteString(`<div class="ol-head"><div class="ol-title">目录</div>`)
	b.WriteString(`<button type="button" class="ol-toggle" aria-expanded="true" aria-label="收起目录" title="收起目录">›</button>`)
	b.WriteString(`</div><div class="ol-list"></div></nav>`)
	b.WriteString(`<button type="button" class="ol-open" aria-label="展开目录" title="展开目录">‹</button>`)
	b.WriteString(`</div>`)
	b.WriteString(htmlScriptBlock)
	return b.String(), nil
}

// buildExportTOC 生成左侧可折叠目录树（TOC）的 HTML 片段。
//
// 锚点分配顺序必须与 mdToHTML 输出正文标题的顺序完全一致：
// 集合名 → 树先序的 folder/request，且每个 request 之后紧跟其 Docs 内的标题。
// 所以 res（scan 结果）用来把每个请求其下 Docs 的标题数计入计数器 ——
// 正文里这些标题同样会被分配 toc-N，不计入会让后续节点的链接整体错位。
func buildExportTOC(name string, tree []*Node, res *scanResult) string {
	// TOC：id 分配顺序必须与 mdToHTML 输出正文标题的顺序完全一致
	// （集合名 → 树先序 folder/request，且每个 request 之后紧跟其 Docs 内的标题）。
	// 先消耗 toc-0 给集合名，再 DFS 遍历节点；doc 节点不生成标题，跳过。
	s := &tocBuilder{res: res}
	toc := &s.b
	toc.WriteString(`<nav class="toc" aria-label="目录">`)
	toc.WriteString(`<div class="toc-title">目录</div>`)
	// 目录搜索：输入时过滤并自动展开命中节点的祖先（见底部 <script>）
	toc.WriteString(`<input type="search" class="toc-search" placeholder="搜索目录…" aria-label="搜索目录">`)
	toc.WriteString(`<ul class="toc-list">`)
	// 集合名（对应正文第一个标题 toc-0）
	toc.WriteString(`<li class="toc-root"><a href="#`)
	toc.WriteString(s.nextID())
	toc.WriteString(`" title="` + html.EscapeString(name) + `">`)
	toc.WriteString(html.EscapeString(name))
	toc.WriteString(`</a></li>`)

	// 生成可折叠的目录树；仅第一个顶层目录默认展开，其余默认折叠。doc 节点不进正文标题，TOC 也跳过。
	s.walk(tree, 0, true)
	toc.WriteString(`</ul></nav>`)
	return toc.String()
}

// tocBuilder 按正文标题顺序分配 toc-N 并写出 HTML 导出目录树。
// id 计数器必须与 mdToHTML 的标题输出顺序严格一致，故 request 之后要消耗其 Docs 的标题数。
type tocBuilder struct {
	b   strings.Builder
	id  int
	res *scanResult
}

// nextID 自增分配一个 toc-N 锚点 id。
func (t *tocBuilder) nextID() string {
	id := fmt.Sprintf("toc-%d", t.id)
	t.id++
	return id
}

// walk 深度优先写出一层节点：doc 跳过（不进正文标题），folder 递归子节点，request 带方法徽标。
// isTop 标记顶层，仅第一个顶层目录默认展开（data-default/aria-expanded），嵌套目录一律默认折叠。
func (t *tocBuilder) walk(nodes []*Node, depth int, isTop bool) {
	for i, n := range nodes {
		if n.Type == "doc" {
			t.doc(n)
			continue
		}
		if n.Type == "folder" {
			t.folder(n, isTop && i == 0, depth)
			continue
		}
		t.request(n)
	}
}

// folder 写一个可折叠目录条目（折叠/展开开关不触发跳转），并递归其子节点。
func (t *tocBuilder) folder(n *Node, open bool, depth int) {
	id := t.nextID()
	t.b.WriteString(`<li class="toc-folder" data-default="`)
	t.b.WriteString(strconv.FormatBool(open))
	t.b.WriteString(`" aria-expanded="`)
	t.b.WriteString(strconv.FormatBool(open))
	t.b.WriteString(`">`)
	// 折叠/展开开关（点击切换 aria-expanded，不触发跳转）
	t.b.WriteString(`<span class="toc-toggle" role="button" tabindex="0" aria-label="折叠或展开"></span>`)
	t.b.WriteString(`<a href="#`)
	t.b.WriteString(id)
	t.b.WriteString(`" title="` + html.EscapeString(n.Name) + `">`)
	t.b.WriteString(html.EscapeString(n.Name))
	t.b.WriteString(`</a>`)
	if len(n.Children) > 0 {
		t.b.WriteString(`<ul>`)
		t.walk(n.Children, depth+1, false)
		t.b.WriteString(`</ul>`)
	}
	t.b.WriteString(`</li>`)
}

// request 写一个请求条目（方法徽标前置，配色与文案口径与客户端集合树 MethodTag 完全一致）。
// 该请求正文之后会紧跟其 Docs 内的标题（被 mdToHTML 占用 toc-N），必须把这部分标题数计入计数器，
// 后续节点的 toc-N 才能与正文对齐。
func (t *tocBuilder) request(n *Node) {
	id := t.nextID()
	t.b.WriteString(`<li class="toc-request">`)
	t.b.WriteString(methodBadge(n.Method))
	t.b.WriteString(`<a href="#`)
	t.b.WriteString(id)
	// 名称过长被 ellipsis 截断时，悬停展示全名（CSS 在 export_template.go 的 .toc-request > a）。
	t.b.WriteString(`" title="` + html.EscapeString(n.Name) + `">`)
	t.b.WriteString(html.EscapeString(n.Name))
	t.b.WriteString(`</a></li>`)
	if t.res != nil {
		if r, ok := t.res.reqs[n.UID]; ok {
			t.id += countDocHeadings(r.Docs)
		}
	}
}

// doc 写一个文档条目（集合里的 .md）。
// 与 request 同理：该条目正文里的标题同样会被 mdToHTML 占用 toc-N，
// 必须把「自身标题 1 + 正文标题数」都计入计数器，后续节点的锚点才不会错位。
func (t *tocBuilder) doc(n *Node) {
	id := t.nextID()
	t.b.WriteString(`<li class="toc-doc">`)
	t.b.WriteString(docBadge())
	t.b.WriteString(`<a href="#`)
	t.b.WriteString(id)
	t.b.WriteString(`" title="` + html.EscapeString(n.Name) + `">`)
	t.b.WriteString(html.EscapeString(n.Name))
	t.b.WriteString(`</a></li>`)
	if t.res != nil {
		if d, ok := t.res.docs[n.UID]; ok {
			t.id += countDocHeadings(d.Content)
		}
	}
}

// docBadge 文档条目的前置徽标：复用方法徽标的 .tm 容器（同宽同字号），
// 只是配色不同，TOC 里请求与文档两类条目才能对齐成一条线。
func docBadge() string {
	return `<span class="tm m-doc">DOC</span>`
}

// mdToHTML 覆盖导出用到的 Markdown 子集（# 标题、``` 代码、**粗体**、`行内代码`、表格、
// --- 分隔线、> 引用、无序/有序列表、段落），并兼容 Docs 里手写的白名单 HTML 标签。
// 每个标题按顺序分配 id="toc-N"（N 从 0 自增），供 HTML 导出的目录树（TOC）锚点跳转。
//
// Markdown 与 HTML 标签冲突的处理口径：
//   - 代码围栏内一切原样转义（含 <tag>），不解析任何标签；
//   - 白名单内的标签（无 script/iframe/form 等危险项，属性经 sanitizeHTMLAttrs 清洗）原样输出；
//   - 其余 `<...>` 一律按文本转义显示 —— 既不吞内容，也不让注入标记活下来。
func mdToHTML(src string) string {
	r := &mdRenderer{}
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trim := strings.TrimSpace(line)

		if r.renderCodeFence(trim) {
			continue
		}
		if r.inCode {
			// 围栏内一切原样转义（含 <tag>），不解析任何标签
			r.b.WriteString(html.EscapeString(line))
			r.b.WriteString("\n")
			continue
		}
		if trim == "" {
			r.closeBlocks()
			continue
		}
		if trim == "---" {
			r.closeBlocks()
			r.b.WriteString("<hr>\n")
			continue
		}
		if r.renderHeading(trim) || r.renderQuote(trim) || r.renderBlockTag(trim) || r.renderListItem(trim) {
			continue
		}
		// 表格行可能顺带吞掉紧随其后的分隔行，故由它回传新的行下标
		if ni, ok := r.renderTableRow(lines, i); ok {
			i = ni
			continue
		}
		r.closeBlocks()
		fmt.Fprintf(&r.b, "<p>%s</p>\n", inlineMD(trim))
	}
	r.closeBlocks()
	if r.inCode {
		r.b.WriteString("</code></pre>\n")
	}
	return r.b.String()
}

// mdRenderer 是 mdToHTML 的行级状态机：块与块之间靠「是否处于代码块 / 表格 /
// 引用 / 列表」这几个开关联动，把它们收进结构里，主循环才能只按块类型分派，
// 而不必把 5 个可变状态在 helper 之间来回传。
type mdRenderer struct {
	b       strings.Builder
	inCode  bool
	inTable bool
	inQuote bool
	listTag string // "" | "ul" | "ol"
	hID     int    // 标题锚点 toc-N 计数器，顺序须与 ExportHTML 的 TOC 一致
}

// closeTable / closeQuote / closeList 收起各自打开中的块；closeBlocks 在任何块级
// 元素切换前调用，保证不会遗留未闭合标签。
func (r *mdRenderer) closeTable() {
	if r.inTable {
		r.b.WriteString("</table>\n")
		r.inTable = false
	}
}

func (r *mdRenderer) closeQuote() {
	if r.inQuote {
		r.b.WriteString("</blockquote>\n")
		r.inQuote = false
	}
}

func (r *mdRenderer) closeList() {
	if r.listTag != "" {
		r.b.WriteString("</" + r.listTag + ">\n")
		r.listTag = ""
	}
}

func (r *mdRenderer) closeBlocks() {
	r.closeTable()
	r.closeQuote()
	r.closeList()
}

// renderCodeFence 切换 ``` 代码围栏状态；返回该行是否已处理。
func (r *mdRenderer) renderCodeFence(trim string) bool {
	if !strings.HasPrefix(trim, "```") {
		return false
	}
	r.closeBlocks()
	if !r.inCode {
		r.b.WriteString("<pre><code>")
		r.inCode = true
	} else {
		r.b.WriteString("</code></pre>\n")
		r.inCode = false
	}
	return true
}

// renderHeading 渲染 # 标题并分配 toc-N 锚点（序号与 ExportHTML 的 TOC 严格对应）。
func (r *mdRenderer) renderHeading(trim string) bool {
	if !strings.HasPrefix(trim, "#") {
		return false
	}
	r.closeBlocks()
	level := headingLevel(trim)
	text := strings.TrimSpace(trim[level:])
	fmt.Fprintf(&r.b, "<h%d id=\"toc-%d\">%s</h%d>\n", level, r.hID, inlineMD(text), level)
	r.hID++
	return true
}

// renderQuote 渲染 > 引用：连续引用行合并进同一个 <blockquote>。
func (r *mdRenderer) renderQuote(trim string) bool {
	if !strings.HasPrefix(trim, ">") {
		return false
	}
	r.closeTable()
	r.closeList()
	if !r.inQuote {
		r.b.WriteString("<blockquote>\n")
		r.inQuote = true
	}
	fmt.Fprintf(&r.b, "<p>%s</p>\n", inlineMD(strings.TrimSpace(strings.TrimPrefix(trim, ">"))))
	return true
}

// renderBlockTag 渲染独占一行且在白名单内的 HTML 块标签（<br>、</details>、<img …>）：
// 原样输出不包 <p>；清洗失败则退化为转义文本（既不吞内容，也不放过注入标记）。
func (r *mdRenderer) renderBlockTag(trim string) bool {
	m := blockTagOnlyRE.FindStringSubmatch(trim)
	if m == nil || !isAllowedHTMLTag(m[1]) {
		return false
	}
	r.closeBlocks()
	if clean, ok := sanitizeHTMLTag(trim); ok {
		r.b.WriteString(clean)
		r.b.WriteString("\n")
	} else {
		fmt.Fprintf(&r.b, "<p>%s</p>\n", html.EscapeString(trim))
	}
	return true
}

// renderListItem 渲染列表项：按起头字符定 ul / ol，连续同类项合并进同一个列表。
func (r *mdRenderer) renderListItem(trim string) bool {
	m := ulItemRE.FindStringSubmatch(trim)
	tag := "ul"
	if m == nil {
		m = olItemRE.FindStringSubmatch(trim)
		tag = "ol"
		if m == nil {
			return false
		}
	}
	r.closeTable()
	r.closeQuote()
	if r.listTag != tag {
		r.closeList()
		r.b.WriteString("<" + tag + ">\n")
		r.listTag = tag
	}
	fmt.Fprintf(&r.b, "<li>%s</li>\n", inlineMD(m[1]))
	return true
}

// renderTableRow 渲染表格的表头行或数据行，返回 (新行下标, 是否已处理)。
// 表头行必须紧跟分隔行（|---|---|）：命中时要把它一并吞掉（回传 i+1）；
// 只判断「下一行含 ---」会把普通正文误当表头，导致后续内容被吞进表里。
func (r *mdRenderer) renderTableRow(lines []string, i int) (int, bool) {
	trim := strings.TrimSpace(lines[i])
	r.closeQuote() // 与进入表格分支前先收起引用块的原有顺序保持一致
	if !strings.HasPrefix(trim, "|") {
		return i, false
	}
	r.closeList()
	cells := splitRow(trim)
	if i+1 < len(lines) && isTableSeparator(lines[i+1]) {
		if !r.inTable {
			r.writeTableHead(cells)
		}
		return i + 1, true // 跳过分隔行
	}
	if !r.inTable {
		return i, false
	}
	r.b.WriteString("<tr>")
	for _, c := range cells {
		fmt.Fprintf(&r.b, "<td>%s</td>", inlineMD(c))
	}
	r.b.WriteString("</tr>\n")
	return i, true
}

// writeTableHead 输出表头并置 inTable。
func (r *mdRenderer) writeTableHead(cells []string) {
	r.b.WriteString("<table>\n<thead><tr>")
	for _, c := range cells {
		fmt.Fprintf(&r.b, "<th>%s</th>", inlineMD(c))
	}
	r.b.WriteString("</tr></thead>\n<tbody>\n")
	r.inTable = true
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

// inlineMD 处理 `code`、**bold** 与白名单内联 HTML 标签（导出用的最小集）。
//
// 必须一次扫描、按 code span 优先：先做全局替换会让 `**` 的配对跨越 code span，
// 产出交叉标签（<code><strong>a</code> b</strong>），奇数个反引号还会留下未闭合的 <code>。
// 未闭合的 ` / ** 一律按字面量输出。白名单外的 `<...>` 按文本转义（防注入/防标签吞内容）。
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
		case s[i] == '<':
			// 白名单内联标签（<b>、<br/>、<a href="…"> 等）原样输出（属性清洗过）；
			// 其它（<script>、未闭合的 `<x`、a < b 等）落到 default 转义。
			if loc := inlineTagRE.FindStringIndex(s[i:]); loc != nil && loc[0] == 0 {
				if clean, ok := sanitizeHTMLTag(s[i : i+loc[1]]); ok {
					b.WriteString(clean)
					i += loc[1]
					continue
				}
			}
			b.WriteString("&lt;")
			i++
		default:
			j := i + 1
			for j < len(s) && s[j] != '`' && s[j] != '<' && !strings.HasPrefix(s[j:], "**") {
				j++
			}
			b.WriteString(html.EscapeString(s[i:j]))
			i = j
		}
	}
	return b.String()
}

// headingLevel 返回 ATX 标题级别（1..6）：行首连续 # 的个数（>6 按 6），规则与 mdToHTML 一致；
// 代码围栏内的行由调用方排除。注意不要求 # 后有空格（#foo 也按标题处理），与既有计数口径保持一致。
func headingLevel(trim string) int {
	if trim == "" || trim[0] != '#' {
		return 0
	}
	n := 0
	for n < len(trim) && trim[n] == '#' {
		n++
	}
	if n > 6 {
		n = 6
	}
	return n
}

// shiftHeadings 把一段 Markdown 的标题整体下移，使最浅标题级别 = minLevel（其余标题按同一差值
// 下移，最深封到 h6）。标题数量不变（只改前缀），故 HTML 的 toc-N 编号仍与正文严格对应。
// 代码围栏内的 # 行不视为标题；delta<=0（Docs 本身已经够深）时原样返回。
func shiftHeadings(md string, minLevel int) string {
	if minLevel < 1 {
		minLevel = 1
	}
	lines := strings.Split(md, "\n")

	min := 0
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
		if lv := headingLevel(trim); lv > 0 && (min == 0 || lv < min) {
			min = lv
		}
	}
	if min == 0 || minLevel-min <= 0 {
		return md
	}
	delta := minLevel - min

	inCode = false
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			continue
		}
		lv := headingLevel(trim)
		if lv == 0 {
			continue
		}
		newLv := lv + delta
		if newLv > 6 {
			newLv = 6
		}
		lines[i] = strings.Repeat("#", newLv) + " " + strings.TrimSpace(trim[lv:])
	}
	return strings.Join(lines, "\n")
}

// methodBadge 生成 TOC 请求条目的方法徽标：名称与配色（--app-method-*）对齐客户端集合树 MethodTag。
func methodBadge(method string) string {
	m := strings.ToUpper(strings.TrimSpace(method))
	cls := "m-other"
	switch m {
	case "GET":
		cls = "m-get"
	case "POST":
		cls = "m-post"
	case "PUT":
		cls = "m-put"
	case "DELETE":
		cls = "m-delete"
	case "PATCH":
		cls = "m-patch"
	case "GRPC":
		cls = "m-grpc"
	}
	if m == "" {
		m = "GET"
	}
	return `<span class="tm ` + cls + `">` + html.EscapeString(m) + `</span>`
}

var (
	// 独占一行的 HTML 标签（块级直通）：<details>、</div>、<br/>、<img src="…"> 等。
	blockTagOnlyRE = regexp.MustCompile(`^</?([a-zA-Z][a-zA-Z0-9-]*)(?:\s[^<>]*)?/?>$`)
	// 无序列表项：- / * / + 起头（生成的 Docs 的「简要描述」「请求头」都是这种行）。
	ulItemRE = regexp.MustCompile(`^[-*+]\s+(.+)$`)
	// 有序列表项：数字 + . 或 ) 起头。
	olItemRE = regexp.MustCompile(`^\d+[.)]\s+(.+)$`)
	// 行内 HTML 标签（inlineMD 用；属性部分不允许出现尖括号，避免误吞后续 Markdown）。
	inlineTagRE = regexp.MustCompile(`</?[a-zA-Z][a-zA-Z0-9-]*(?:\s[^<>]*?)?\s*/?>`)
	// 标签内属性：name 或 name=value（value 支持双引号/单引号/裸值）。
	htmlAttrRE = regexp.MustCompile(`[a-zA-Z_:][-a-zA-Z0-9_:.]*(?:\s*=\s*(?:"[^"]*"|'[^']*'|[^\s"'=<>` + "`" + `]+))?`)
	tagNameRE  = regexp.MustCompile(`^</?([a-zA-Z][a-zA-Z0-9-]*)`)
)

// htmlAllowedTags 允许在导出 HTML 中原样保留的标签白名单。
//
// 刻意不含 script/iframe/object/embed/form/input/button/link/style/meta/svg 等可执行或可外联的标签 ——
// 即使内容来自用户自己的集合，也按「最小能力」放行（markdown-it 常见排版标签即可）。
//
// **只读**：本表在包初始化后不再修改，因此不需要加锁（P2-20）。
// 唯一读取入口是 isAllowedHTMLTag；新增白名单项请改这里并跑 TestHTMLAllowedTagsAreSafe。
var htmlAllowedTags = map[string]bool{
	"a": true, "abbr": true, "b": true, "blockquote": true, "br": true, "caption": true,
	"cite": true, "code": true, "col": true, "colgroup": true, "dd": true, "del": true,
	"details": true, "div": true, "dl": true, "dt": true, "em": true, "figcaption": true,
	"figure": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"hr": true, "i": true, "img": true, "kbd": true, "li": true, "mark": true, "ol": true,
	"p": true, "pre": true, "q": true, "s": true, "small": true, "span": true, "strong": true,
	"sub": true, "sup": true, "summary": true, "table": true, "tbody": true, "td": true,
	"tfoot": true, "th": true, "thead": true, "tr": true, "u": true, "ul": true,
}

// isAllowedHTMLTag 判断标签名（大小写不敏感）是否在白名单内。
// 全部调用点都走它，避免将来有人新增判断分支时直接读 map、忘了忽略大小写。
func isAllowedHTMLTag(name string) bool {
	return htmlAllowedTags[strings.ToLower(name)]
}

// sanitizeHTMLTag 清洗单个 HTML 标签：非白名单标签返回 ok=false；白名单标签删除所有 on* 事件
// 属性与 javascript:/vbscript: 链接，其余属性（class/href/src/alt/title/colspan…）保留并规范引号。
func sanitizeHTMLTag(tag string) (string, bool) {
	m := tagNameRE.FindStringSubmatch(tag)
	if m == nil {
		return "", false
	}
	name := strings.ToLower(m[1])
	if !isAllowedHTMLTag(name) {
		return "", false
	}
	closing := strings.HasPrefix(strings.TrimSpace(tag), "</")
	selfClose := strings.HasSuffix(strings.TrimSpace(tag), "/>")

	var b strings.Builder
	b.WriteString("<")
	if closing {
		b.WriteString("/")
	}
	b.WriteString(name)
	if !closing {
		// 取标签名之后、结尾 > / /> 之前的属性区（m[0] 形如 "<a" 或 "</div"）
		rest := tag[len(m[0]):]
		rest = strings.TrimSpace(rest)
		rest = strings.TrimSuffix(rest, "/>")
		rest = strings.TrimSuffix(rest, ">")
		rest = strings.TrimSpace(rest)
		for _, am := range htmlAttrRE.FindAllString(rest, -1) {
			if frag, keep := cleanHTMLAttr(am); keep {
				b.WriteString(frag)
			}
		}
		if selfClose {
			b.WriteString(" /")
		}
	}
	b.WriteString(">")
	return b.String(), true
}

// cleanHTMLAttr 清洗单个 HTML 属性，返回要写入的片段（含前导空格）与是否保留。
// 事件属性（on*）一律删除；href/src 命中危险协议（javascript:/vbscript:）删除；
// 其余属性规范引号后保留（html.EscapeString 已把引号转成 &#34;/&#39;，双引号包裹安全）。
func cleanHTMLAttr(am string) (string, bool) {
	pair := strings.SplitN(am, "=", 2)
	an := strings.ToLower(strings.TrimSpace(pair[0]))
	if an == "" {
		return "", false
	}
	if strings.HasPrefix(an, "on") {
		return "", false // 事件属性一律删除
	}
	if len(pair) == 1 {
		return " " + an, true
	}
	raw := strings.TrimSpace(pair[1])
	var v string
	if len(raw) >= 2 && (raw[0] == '"' || raw[0] == '\'') {
		v = raw[1 : len(raw)-1]
	} else {
		v = raw
	}
	if (an == "href" || an == "src") && dangerousURL(v) {
		return "", false
	}
	return " " + an + "=\"" + html.EscapeString(v) + "\"", true
}

// dangerousURL 判断 URL 是否带可执行 scheme（javascript:/vbscript:/data:text/html）。
func dangerousURL(v string) bool {
	u := strings.ToLower(strings.TrimSpace(v))
	switch {
	case strings.HasPrefix(u, "javascript:"), strings.HasPrefix(u, "vbscript:"):
		return true
	case strings.HasPrefix(u, "data:text/html"):
		return true
	}
	return false
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
