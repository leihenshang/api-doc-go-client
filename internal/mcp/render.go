package mcp

import (
	"fmt"
	"sort"
	"strings"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/runner"
)

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
	return fmt.Sprintf("…（已截断：共 %d 字节，本次仅返回 %d 字节；需要完整内容请用 get_request_detail 的 include_body）", total, shown)
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
	Saved         bool   `json:"saved"`                 // 是否已存为响应示例
	ExampleUID    string `json:"exampleUid,omitempty"`  // 落盘的示例 uid
	ExamplePath   string `json:"examplePath,omitempty"` // 落盘的示例路径
}

// outcomeOf 把 runner.Result 转成出参（响应体按 maxBodyBytes 裁剪）。
func outcomeOf(res *runner.Result, maxBodyBytes int) SendOutcome {
	body, truncated, total := Truncate(res.Body, maxBodyBytes)
	return SendOutcome{
		Status: res.Status, Proto: res.Proto, URL: res.URL,
		TimeMS: res.TimeMS, Size: res.Size, ContentType: res.ContentType,
		Body: body, Truncated: truncated, OriginalBytes: total,
		HeaderCount: len(res.Headers),
	}
}

// ExampleItem 已保存响应示例的摘要。
type ExampleItem struct {
	UID       string `json:"uid"`
	Name      string `json:"name"`
	Path      string `json:"path"`
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
	if o.Truncated {
		fmt.Fprintf(&sb, "%s\n", truncateNote(true, o.OriginalBytes, len(o.Body)))
	}
	if o.Body != "" {
		sb.WriteString(o.Body)
	} else {
		sb.WriteString("（未返回响应体：include_body=false）")
	}
	if o.Saved {
		fmt.Fprintf(&sb, "\n\n已保存为响应示例：%s（uid=%s）", o.ExamplePath, o.ExampleUID)
	} else {
		sb.WriteString("\n\n（未保存响应示例）")
	}
	return sb.String()
}

// renderDetail 把接口详情渲染为紧凑文本。
func renderDetail(d *RequestDetail) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s [%s] %s\n", d.Name, d.Method, d.URL)
	fmt.Fprintf(&sb, "项目 %s · 路径 %s · uid %s", d.Project, d.Path, d.UID)
	if d.Hash != "" {
		// 只展示 12 位：服务端按 git 风格前缀比对（≥8 位），这个前缀可直接回传给 update_request.ifMatch
		fmt.Fprintf(&sb, " · hash %s…", d.Hash[:min(12, len(d.Hash))])
	}
	sb.WriteString("\n")
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
	if d.Docs != "" {
		fmt.Fprintf(&sb, "\n说明文档：\n%s\n", d.Docs)
	}
	if len(d.RelatedDocs) > 0 {
		sb.WriteString("\n相关文档条目：\n")
		for _, r := range d.RelatedDocs {
			fmt.Fprintf(&sb, "  %s（%s）\n", r.Name, r.Path)
		}
	}
	if len(d.Examples) > 0 {
		fmt.Fprintf(&sb, "\n已保存的响应示例（%d 个）：\n", len(d.Examples))
		for _, e := range d.Examples {
			fmt.Fprintf(&sb, "  - %s：%d · %d ms · %d 字节（%s）\n", e.Name, e.Status, e.TimeMS, e.BodyBytes, e.Path)
			if e.WithBody != "" {
				fmt.Fprintf(&sb, "    %s\n", e.WithBody)
			}
		}
	} else {
		sb.WriteString("\n（还没有保存的响应示例：用 send_request 发送一次即可留存）")
	}
	return sb.String()
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
func renderEnvs(project string, envs []EnvEntry) string {
	if len(envs) == 0 {
		return fmt.Sprintf("项目 %s 还没有任何环境。用 create_env 新建（请求里以 {{变量名}} 引用其中的变量）。", project)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "项目 %s 共 %d 个环境：\n", project, len(envs))
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
