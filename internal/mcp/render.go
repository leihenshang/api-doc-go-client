package mcp

import (
	"fmt"
	"sort"
	"strings"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/runner"
)

// renderImportSummary import_collection 的文本结果（条数 + 失败明细）。
func renderImportSummary(project, format, parent string, sum *collection.ImportSummary, dryRun bool) string {
	where := strings.TrimSpace(parent)
	if where == "" {
		where = "项目根"
	}
	head := "已导入"
	if dryRun {
		head = "（dryRun：未落盘）将导入"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %d 条请求（format=%s，项目 %s，位置 %s）", head, sum.Imported, format, project, where)
	if !dryRun && sum.Skipped > 0 {
		fmt.Fprintf(&sb, "，跳过 %d 条（已存在同方法同地址）", sum.Skipped)
	}
	sb.WriteByte('\n')
	if len(sum.Failures) > 0 {
		fmt.Fprintf(&sb, "失败 %d 条：\n", len(sum.Failures))
		for _, f := range sum.Failures {
			sb.WriteString("- " + f + "\n")
		}
	}
	if dryRun {
		sb.WriteString("预演只解析了文件正文：实际导入时「已存在」的请求会被跳过，条数可能更少。\n")
	}
	return sb.String()
}

// renderToolCatalog list_tools 的文本输出：一行一个工具（名 + 读/写 + 必填 + 一句话）。
//
// 刻意不在这里贴完整 schema —— 那正是 list_tools 要省掉的东西；需要细节再单独取。
func renderToolCatalog(items []toolCatalogItem, total int, filter, mode string, readOnly bool) string {
	var sb strings.Builder
	if filter != "" {
		fmt.Fprintf(&sb, "共 %d 个可用工具，命中 %d 个（filter=%s）\n", total, len(items), filter)
	} else {
		fmt.Fprintf(&sb, "共 %d 个可用工具（本次返回 %d 个）\n", total, len(items))
	}
	if strings.EqualFold(strings.TrimSpace(mode), "write") {
		sb.WriteString("（mode=write：只看会落盘改集合的工具）\n")
	} else if strings.EqualFold(strings.TrimSpace(mode), "read") {
		sb.WriteString("（mode=read：只看只读与会话类工具）\n")
	}
	if len(items) == 0 {
		// 只读模式下写工具根本没注册：必须说清楚，否则调用方会误判成「没有写权限/没有这类工具」。
		if readOnly && strings.EqualFold(strings.TrimSpace(mode), "write") {
			sb.WriteString("当前 MCP 服务是只读模式：写工具未注册，因此没有可列出的写工具。\n")
			return sb.String()
		}
		sb.WriteString("没有命中的工具：换个关键词或去掉 filter。\n")
		return sb.String()
	}
	for _, it := range items {
		kind := "读"
		if it.Write {
			kind = "写"
		}
		req := "无必填"
		if len(it.Required) > 0 {
			req = "必填：" + strings.Join(it.Required, "、")
		}
		fmt.Fprintf(&sb, "- %s [%s] %s｜%s\n", it.Name, kind, req, it.Description)
	}
	return sb.String()
}

// bodySummary 请求体的简短摘要（仅用于打分，不进输出）。
func bodySummary(r *collection.Request) string {
	switch r.Body.Type {
	case "raw":
		return r.Body.Raw
	case "form":
		var sb strings.Builder
		for _, f := range r.Body.Form {
			sb.WriteString(f.Name)
			sb.WriteString(f.Value)
			sb.WriteByte(' ')
		}
		return sb.String()
	default:
		return ""
	}
}

// Truncate 按 maxBytes 截断字符串（按 rune 边界，避免切坏多字节字符）。
// 返回裁剪后的文本、是否被截断、原始字节数。
func Truncate(s string, maxBytes int) (string, bool, int) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBodyBytes
	}
	total := len(s)
	if total <= maxBytes {
		return s, false, total
	}
	cut := maxBytes
	for cut > 0 && !isRuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true, total
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// truncateNote 截断时给 AI 的明确说明（不静默丢弃）。
func truncateNote(truncated bool, total, shown int) string {
	if !truncated {
		return ""
	}
	return fmt.Sprintf("…（已截断：共 %d 字节，本次仅返回 %d 字节；要更多内容请调大 maxBodyBytes，或用 get_request_detail 的 sections 只取需要的分段 / includeBody 取全文）", total, shown)
}

// SendOutcome send_request 的返回（响应体已裁剪）。
type SendOutcome struct {
	Status        int    `json:"status"`
	Proto         string `json:"proto,omitempty"`
	URL           string `json:"url"`
	TimeMS        int64  `json:"timeMs"`
	Size          int    `json:"size"`
	ContentType   string `json:"contentType,omitempty"`
	Body          string `json:"body"`
	Truncated     bool   `json:"truncated"`
	OriginalBytes int    `json:"originalBytes"`
	HeaderCount   int    `json:"headerCount"`
	// 连接与性能计数（本次实时调用；不属示例/同步数据）
	Redirects   int    `json:"redirects,omitempty"`
	RemoteAddr  string `json:"remoteAddr,omitempty"`
	TLSVersion  string `json:"tlsVersion,omitempty"`
	TLSCipher   string `json:"tlsCipher,omitempty"`
	Reused      bool   `json:"reused,omitempty"`
	Saved       bool   `json:"saved"`                 // 是否已存为响应示例
	ExampleUID  string `json:"exampleUid,omitempty"`  // 落盘的示例 uid
	ExamplePath string `json:"examplePath,omitempty"` // 落盘的示例路径
}

// outcomeOf 把 runner.Result 转成出参（响应体按 maxBodyBytes 裁剪）。
func outcomeOf(res *runner.Result, maxBodyBytes int) SendOutcome {
	body, truncated, total := Truncate(res.Body, maxBodyBytes)
	return SendOutcome{
		Status: res.Status, Proto: res.Proto, URL: res.URL,
		TimeMS: res.TimeMS, Size: res.Size, ContentType: res.ContentType,
		Body: body, Truncated: truncated, OriginalBytes: total,
		HeaderCount: len(res.Headers),
		Redirects:   res.Redirects, RemoteAddr: res.RemoteAddr,
		TLSVersion: res.TLSVersion, TLSCipher: res.TLSCipher, Reused: res.Reused,
	}
}

// ExampleItem 已保存响应示例的摘要。
type ExampleItem struct {
	UID       string `json:"uid"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Hash      string `json:"hash,omitempty"` // 文件 hash：delete_response_example.ifMatch 用
	CreatedAt int64  `json:"createdAt"`
	Status    int    `json:"status"`
	TimeMS    int64  `json:"timeMs"`
	BodyBytes int    `json:"bodyBytes"`
	WithBody  string `json:"body,omitempty"`      // 仅 include_body 时返回（已裁剪）
	Truncated bool   `json:"truncated,omitempty"` //
}

// renderSend 把发送结果渲染为文本（响应体裁剪时明确标注）。
func renderSend(o *SendOutcome) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d %s · %d ms · %d 字节", o.Status, o.Proto, o.TimeMS, o.Size)
	if o.ContentType != "" {
		fmt.Fprintf(&sb, " · %s", o.ContentType)
	}
	fmt.Fprintf(&sb, "\nURL: %s\n", o.URL)
	// 连接/性能行：仅在拿到时附，避免给 AI 灌空字段
	if o.RemoteAddr != "" || o.TLSVersion != "" || o.Redirects > 0 || o.Reused {
		ra := o.RemoteAddr
		if ra == "" {
			ra = "未知"
		}
		fmt.Fprintf(&sb, "连接：对端 %s", ra)
		if o.TLSVersion != "" {
			fmt.Fprintf(&sb, " · %s", o.TLSVersion)
			if o.TLSCipher != "" {
				fmt.Fprintf(&sb, " (%s)", o.TLSCipher)
			}
		}
		if o.Redirects > 0 {
			fmt.Fprintf(&sb, " · 重定向 %d 次", o.Redirects)
		}
		if o.Reused {
			sb.WriteString(" · 连接复用")
		}
		sb.WriteString("\n")
	}
	if o.Truncated {
		fmt.Fprintf(&sb, "%s\n", truncateNote(true, o.OriginalBytes, len(o.Body)))
	}
	if o.Body != "" {
		sb.WriteString(o.Body)
	} else {
		sb.WriteString("（未返回响应体：includeBody=false）")
	}
	if o.Saved {
		fmt.Fprintf(&sb, "\n\n已保存为响应示例：%s（uid=%s）", o.ExamplePath, o.ExampleUID)
	} else {
		sb.WriteString("\n\n（未保存响应示例）")
	}
	return sb.String()
}

// 详情分段的合法取值（get_request_detail 的 sections，单个与批量共用）。
// 不传 = 全量（与旧行为一致）；显式传时只渲染选中的段，便于把单次响应压到 3000 字符内。
var detailSectionNames = []string{"overview", "docs", "bodyFields", "respDoc", "examples"}

// NormalizeDetailSections 校验并归一 sections：空/全空串 = nil（全量）。
func NormalizeDetailSections(sections []string) (map[string]bool, error) {
	if len(sections) == 0 {
		return nil, nil
	}
	out := make(map[string]bool, len(sections))
	for _, raw := range sections {
		s := strings.ToLower(strings.TrimSpace(raw))
		if s == "" {
			continue
		}
		// 输入统一小写后必须与枚举做「大小写无关」比较：
		// bodyFields / respDoc 是驼峰，早先直接比 s == name 会永远匹配不上（评审 P0-1）。
		canonical := ""
		for _, name := range detailSectionNames {
			if s == strings.ToLower(name) {
				canonical = name
				break
			}
		}
		if canonical == "" {
			return nil, withInvalid(fmt.Errorf("sections 含未知取值 %q；可选值：%s", raw, strings.Join(detailSectionNames, "/")))
		}
		out[canonical] = true
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// hasSection sel 为空 = 全量；否则只渲染选中的段。
func hasSection(sel map[string]bool, name string) bool { return len(sel) == 0 || sel[name] }

// renderDetail 把接口详情渲染为紧凑文本；sections 为空 = 全量（旧行为）。
func renderDetail(d *RequestDetail, sections map[string]bool, docsView LineView) string {
	var sb strings.Builder
	// 身份行恒输出：即使只取 docs，也要让 AI 知道这是哪张卡片（占不了多少体积）
	fmt.Fprintf(&sb, "%s [%s] %s\n", d.Name, d.Method, d.URL)
	fmt.Fprintf(&sb, "项目 %s · 路径 %s · uid %s", d.Project, d.Path, d.UID)
	if d.Hash != "" {
		// 完整 hash：可直接回传 update_request.ifMatch（服务端按 git 风格前缀比对，全串当然也认）
		fmt.Fprintf(&sb, " · hash %s", d.Hash)
	}
	sb.WriteString("\n")
	if hasSection(sections, "overview") {
		// 概述（summary）：写得到就要读得回 —— 详情是 AI 唯一的结构化出口，
		// 漏渲染会让 create/update 传的概述「写进文件但读不出来」，也让 get_request_detail 文案落空。
		if d.Summary != "" {
			fmt.Fprintf(&sb, "概述：%s\n", d.Summary)
		}
		// 新建请求默认带一行空参数（占位行），渲染时要跳过，否则输出里出现无意义的「 = 」
		if params := nonEmpty(d.Params); len(params) > 0 {
			sb.WriteString("\n参数：\n")
			for _, p := range params {
				fmt.Fprintf(&sb, "  %s = %s\n", p.Name, p.Value)
			}
		}
		if len(d.Headers) > 0 {
			sb.WriteString("\nHeaders：\n")
			for _, h := range d.Headers {
				fmt.Fprintf(&sb, "  %s: %s\n", h.Name, h.Value)
			}
		}
		if d.Body.Type != "" && d.Body.Type != "none" {
			fmt.Fprintf(&sb, "\nBody（%s）：\n%s\n", d.Body.Type, d.Body.Raw)
		}
		if d.GRPC != nil {
			fmt.Fprintf(&sb, "\ngRPC：%s（服务 %s / 方法 %s）", d.GRPC.Target, d.GRPC.Service, d.GRPC.Method)
			if d.GRPC.Proto != "" {
				fmt.Fprintf(&sb, " 定义 %s", d.GRPC.Proto)
			}
			fmt.Fprintln(&sb)
		}
		if d.Auth != nil {
			fmt.Fprintf(&sb, "\nAuth：%s\n", d.Auth.Type)
		}
	}
	// 请求体字段表格（body.fields，唯一真源）：5 列与 get_doc_template / 客户端「生成接口文档」同口径。
	// 表格有行才输出 —— 空表格只给原文（与"表格为空只出请求体原文"的约定一致）。
	if hasSection(sections, "bodyFields") && len(d.Body.Fields) > 0 {
		fmt.Fprintf(&sb, "\n请求体字段（%d 行，body.fields）：\n", len(d.Body.Fields))
		sb.WriteString("| 字段名 | 必选 | 类型 | 示例值 | 说明 |\n")
		sb.WriteString("| --- | --- | --- | --- | --- |\n")
		for _, f := range d.Body.Fields {
			req := "否"
			if f.Required {
				req = "是"
			}
			typ := f.Type
			if typ == "" {
				typ = "string"
			}
			fmt.Fprintf(&sb, "| %s | %s | %s | %s | %s |\n", cell(f.Name), req, cell(typ), cell(f.Example), cell(f.Description))
		}
		sb.WriteString("（整表回写用 update_request.bodyFields；只更新 body 原文不会清掉本表）\n")
	}
	if hasSection(sections, "docs") {
		if d.Docs != "" {
			// 说明文档可能很长：与 get_doc 共用行区间 / 大纲视图（全文时超长会加一行建议）
			docsView.Suggest = true
			fmt.Fprintf(&sb, "\n说明文档：\n%s", renderDocContent(d.Docs, docsView))
		}
		if len(d.RelatedDocs) > 0 {
			sb.WriteString("\n相关文档条目：\n")
			for _, r := range d.RelatedDocs {
				fmt.Fprintf(&sb, "  %s（%s）\n", r.Name, r.Path)
			}
		}
	}
	if hasSection(sections, "respDoc") && d.RespDoc != nil {
		fmt.Fprintf(&sb, "\n响应字段文档（返回类型 %s，共 %d 个字段）：\n", d.RespDoc.Type, len(d.RespDoc.Params))
		for _, p := range d.RespDoc.Params {
			req := "可选"
			if p.Required {
				req = "必选"
			}
			fmt.Fprintf(&sb, "  %s: %s（%s，%s）", p.FieldName, p.Description, p.Type, req)
			if p.Example != "" {
				fmt.Fprintf(&sb, " 示例 %s", p.Example)
			}
			sb.WriteString("\n")
		}
		if d.RespDoc.Sample != "" {
			fmt.Fprintf(&sb, "响应样例：\n%s\n", d.RespDoc.Sample)
		}
		fmt.Fprintln(&sb, "（整表回写：改字段说明用 update_request.respDoc，传完整的新表）")
	}
	if hasSection(sections, "examples") {
		if len(d.Examples) > 0 {
			// uid 必须打出来：update_response_example / delete_response_example 的 exampleUid 只能从这里拿
			// （工具结果是一段文本，没有结构化字段可回引），漏了就没法定位「已存在」的示例。
			fmt.Fprintf(&sb, "\n已保存的响应示例（%d 个）：\n", len(d.Examples))
			for _, e := range d.Examples {
				fmt.Fprintf(&sb, "  - %s：%d · %d ms · %d 字节（%s，uid=%s，hash=%s）\n",
					e.Name, e.Status, e.TimeMS, e.BodyBytes, e.Path, e.UID, e.Hash)
				if e.WithBody != "" {
					fmt.Fprintf(&sb, "    %s\n", e.WithBody)
				}
			}
		} else {
			sb.WriteString("\n（还没有保存的响应示例：用 send_request 发送一次即可留存）")
		}
	}
	return sb.String()
}

// previewFirstLines 取文本前 n 行（每行截到 200 字符）：update_* 成功后的「落盘确认」。
func previewFirstLines(text string, n int) string {
	if strings.TrimSpace(text) == "" || n <= 0 {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	for i, l := range lines {
		lines[i] = clipRunes(strings.TrimRight(l, " \t"), 200)
	}
	return strings.Join(lines, "\n")
}

// changePreview 生成「落盘确认」的标注与片段（update_* 成功后用）：
//   - 能按行定位改动（共同前缀/后缀夹出的区间）→ 标注「第 N–M 行 ±1 行」，片段取改动区 ±1 行；
//   - 整篇重写 / 首次写入（没有可比对的前缀与后缀）→ 回落「前 5 行」。
//
// 只做行级粗定位（不引入 diff 依赖）：足够回答「我改的那段进文件了吗」。
func changePreview(oldText, newText string) (string, string) {
	if strings.TrimSpace(newText) == "" {
		return "", ""
	}
	oldLines := strings.Split(strings.ReplaceAll(oldText, "\r\n", "\n"), "\n")
	newLines := strings.Split(strings.ReplaceAll(newText, "\r\n", "\n"), "\n")
	p := 0
	for p < len(oldLines) && p < len(newLines) && oldLines[p] == newLines[p] {
		p++
	}
	s := 0
	for s < len(oldLines)-p && s < len(newLines)-p &&
		oldLines[len(oldLines)-1-s] == newLines[len(newLines)-1-s] {
		s++
	}
	if p == 0 && s == 0 {
		return "前 5 行", previewFirstLines(newText, 5)
	}
	from := p               // 0-based 改动起点（新文本）
	to := len(newLines) - s // 0-based 改动终点（不含）
	kind := ""
	if to <= from {
		// 纯删除（新文本没有变多）：标注删除点附近（p 是删除处的 0-based 下标）
		line := min(p+1, len(newLines))
		if line < 1 {
			line = 1
		}
		kind = fmt.Sprintf("第 %d 行附近 ±1 行", line)
		from, to = max(0, p-1), min(len(newLines), p+1)
	} else {
		kind = fmt.Sprintf("第 %d–%d 行 ±1 行", from+1, to)
	}
	lo, hi := max(0, from-1), min(len(newLines), to+1)
	return kind, clipRunes(strings.Join(newLines[lo:hi], "\n"), 400)
}

// cell 转义 Markdown 表格单元格：竖线与换行会破坏表格行（口径同客户端 genDocs 的 esc）。
func cell(text string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(text, "|", "\\|"), "\n", " "))
}

// nonEmpty 过滤掉「名字与值都为空」的占位行（新建请求自带一行空参数）。
func nonEmpty(kvs []collection.KV) []collection.KV {
	out := make([]collection.KV, 0, len(kvs))
	for _, kv := range kvs {
		if kv.Name == "" && kv.Value == "" {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// renderEnvs 把环境列表渲染成可读文本（list_envs 的输出）。
//
// 排序：环境按名字（与服务端一致），变量按名字 —— map 的迭代顺序随机，
// 不排序的话同一份配置每次输出顺序都不同，AI 会以为内容在变。
func renderEnvs(project string, envs []EnvEntry, total, offset int) string {
	if len(envs) == 0 {
		return fmt.Sprintf("项目 %s 还没有任何环境。用 create_env 新建（请求里以 {{变量名}} 引用其中的变量）。", project)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "项目 %s 共 %d 个环境（本次返回 %d 个，offset %d）：\n", project, total, len(envs), offset)
	for _, env := range envs {
		tag := ""
		if env.Default {
			tag = "（默认：send_request 不指定 env 时用它）"
		}
		hash := ""
		if env.Hash != "" {
			hash = fmt.Sprintf(" · hash %s…", env.Hash)
		}
		fmt.Fprintf(&sb, "\n%s%s · %d 个变量%s\n", env.Name, tag, len(env.Vars), hash)
		names := make([]string, 0, len(env.Vars))
		for name := range env.Vars {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			v := env.Vars[name]
			mark := ""
			if !v.Enabled {
				mark = "（已停用，不参与解析）"
			}
			if v.Secret {
				mark += "（敏感值）"
			}
			fmt.Fprintf(&sb, "  %s = %s%s\n", v.Name, v.Value, mark)
		}
		if len(names) == 0 {
			sb.WriteString("  （空环境，用 set_env_var 添加变量）\n")
		}
	}
	sb.WriteString("\n提示：敏感变量的值只回掩码，这是有意为之（不把真实密钥交给 AI）；")
	sb.WriteString("set_env_var 把掩码原样传回即表示「这条密钥不变」。\n")
	sb.WriteString("写变量前建议把上面的 hash 作为 if_match 传回：环境是整份重写，不带校验会覆盖期间的其它改动。")
	return sb.String()
}

// renderWorkspaces list_workspaces 的输出：授权的工作目录 + 其中的项目 + 当前默认目标。
func renderWorkspaces(items []WorkspaceItem, selected string) string {
	var sb strings.Builder
	sb.WriteString("MCP 可访问的工作目录（客户端设置里的白名单）：\n")
	if len(items) == 0 {
		sb.WriteString("- （空）当前未授权任何工作目录：AI 无法读写集合。请在客户端「设置 → MCP 服务 → 可访问的工作目录」里添加目录。\n")
		return sb.String()
	}
	for _, it := range items {
		perm := "只读"
		if it.Writable {
			perm = "可写"
		}
		mark := ""
		if it.Selected {
			mark = "  ← 当前默认"
		}
		fmt.Fprintf(&sb, "- %s（%s，%s）%s\n", it.Path, it.Name, perm, mark)
		if it.Error != "" {
			fmt.Fprintf(&sb, "    不可用：%s\n", it.Error)
			continue
		}
		if len(it.Projects) == 0 {
			sb.WriteString("    目录下没有项目（集合需要含 opencollection.yml）\n")
			continue
		}
		fmt.Fprintf(&sb, "    项目：%s\n", strings.Join(it.Projects, "、"))
	}
	if selected == "" {
		sb.WriteString("尚未选定默认工作目录：调用 use_workspace 选定后，其余工具的 project 参数即可省略。\n")
	} else {
		fmt.Fprintf(&sb, "当前默认目标：%s（其余工具可省略 project）\n", selected)
	}
	return sb.String()
}

// renderWorkspaceSelected use_workspace 的结果。
func renderWorkspaceSelected(it WorkspaceItem, selected string) string {
	perm := "只读"
	if it.Writable {
		perm = "可写"
	}
	return fmt.Sprintf("已选定默认工作目录：%s（%s，%s）\n后续工具可以省略 project 参数（仍可显式指定）。当前默认目标：%s",
		it.Path, it.Name, perm, selected)
}

// renderDocs list_docs 的结果：一篇文档一行（名称 · 路径 · 字节数）+ 下一层的 uid。
func renderDocs(project string, docs []DocItem, total, offset int) string {
	if total == 0 {
		return fmt.Sprintf("项目 %s 里没有文档条目。用 create_doc 新建。"+
			"（文档是集合树里独立的 Markdown 文件；接口自带的说明走 request.docs，不在这里）", project)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "项目 %s 共 %d 篇文档（本次返回 %d 篇，offset %d）：\n", project, total, len(docs), offset)
	for _, d := range docs {
		fmt.Fprintf(&sb, "\n- %s · %s · %d 字节", d.Name, d.Path, d.Size)
		if d.Hash != "" {
			fmt.Fprintf(&sb, " · hash %s", d.Hash)
		}
		fmt.Fprintf(&sb, "\n  uid %s\n", d.UID)
	}
	if offset+len(docs) < total {
		fmt.Fprintf(&sb, "\n还有 %d 篇：用 offset=%d 继续取。", total-offset-len(docs), offset+len(docs))
	}
	if len(docs) > 0 && docs[0].Hash != "" {
		sb.WriteString("\n用 get_doc 读正文（列表里的 hash 可直接回传 update_doc.ifMatch）。")
	} else {
		sb.WriteString("\n用 get_doc 读正文；需要并发保护时再 get_doc 取 hash（或 list_docs 带 includeHash=true）。")
	}
	return sb.String()
}

// renderDocDetail get_doc / create_doc / update_doc 的结果：头部元信息 + 正文原文。
func renderDocDetail(d *DocDetail, v LineView) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n路径 %s · uid %s", d.Name, d.Path, d.UID)
	if d.Hash != "" {
		// 完整 hash（同 renderDetail）：可直接回传给 update_doc.ifMatch
		fmt.Fprintf(&sb, " · hash %s", d.Hash)
	}
	sb.WriteString("\n")
	if strings.TrimSpace(d.Content) == "" {
		sb.WriteString("\n（正文为空，可用 update_doc 写入）\n")
		return sb.String()
	}
	// 全文原样给出（不转义）；区间/大纲模式带行号，超长全文只加一行「建议分段」提示
	sb.WriteString("\n")
	sb.WriteString(renderDocContent(d.Content, v))
	return sb.String()
}
