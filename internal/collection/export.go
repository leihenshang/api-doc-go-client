package collection

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
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
		full := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		f, err := share.DecodeRequest(data)
		if err != nil {
			continue
		}
		item := ConflictItem{
			File: e.Name(),
			Name: f.Info.Name,
		}
		if f.Extra != nil {
			if v, ok := f.Extra["conflict_of"].(string); ok {
				item.OfUID = v
			}
			switch v := f.Extra["server_rev"].(type) {
			case int:
				item.ServerRev = int64(v)
			case int64:
				item.ServerRev = v
			case float64:
				item.ServerRev = int64(v)
			}
		}
		if st, err := os.Stat(full); err == nil {
			item.CreatedAt = st.ModTime().UnixMilli()
		}
		out = append(out, item)
	}
	return out, nil
}

// ResolveConflict 三选一（R8 / I4）：
//   - "local"  保留本地：删掉副本，本地继续 dirty，下次 push 覆盖服务端
//   - "remote" 以远端为准：删掉副本（服务端版本由下一轮 pull 落回）
//   - "copy"   保留副本：什么都不动
func (c *Collection) ResolveConflict(file, choice string) error {
	full := filepath.Join(c.Dir, ".conflicts", filepath.Base(file))
	if _, err := os.Stat(full); err != nil {
		return fmt.Errorf("冲突副本不存在")
	}
	switch choice {
	case "local", "remote":
		return c.moveToTrash(full)
	case "copy":
		return nil
	default:
		return fmt.Errorf("未知的取舍方式：%s", choice)
	}
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
		fmt.Fprintf(b, "| `%s` | `%s` | %s |\n", r.Name, r.Value, r.Description)
	}
	fmt.Fprintf(b, "\n")
}

// ExportHTML 导出单文件 HTML（内联样式，可直接浏览器打开/分享）。
func (c *Collection) ExportHTML() (string, error) {
	md, err := c.ExportMarkdown()
	if err != nil {
		return "", err
	}
	// 轻量 Markdown → HTML（标题/代码块/粗体/表格/分隔线/段落），避免再引依赖
	body := mdToHTML(md)
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
  body { font: 14px/1.7 -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Noto Sans SC", sans-serif;
         max-width: 920px; margin: 0 auto; padding: 32px 24px; color: #202124; background: #fff; }
  h1,h2,h3,h4,h5,h6 { margin: 1.4em 0 0.6em; font-weight: 600; line-height: 1.3; }
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
</style>
</head>
<body>
`)
	b.WriteString(body)
	b.WriteString(`
</body>
</html>
`)
	return b.String(), nil
}

// mdToHTML 覆盖导出用到的 Markdown 子集（# 标题、``` 代码、**粗体**、表格、---、> 引用、段落）。
func mdToHTML(src string) string {
	lines := strings.Split(src, "\n")
	var b strings.Builder
	inCode := false
	inTable := false
	inQuote := false

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
			fmt.Fprintf(&b, "<h%d>%s</h%d>\n", level, inlineMD(text), level)
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
			// 分隔行 |---|---|
			if i+1 < len(lines) && strings.Contains(lines[i+1], "---") {
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

func splitRow(row string) []string {
	row = strings.Trim(row, "|")
	parts := strings.Split(row, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

// inlineMD 处理 `code` 与 **bold**（导出用的最小集）。
func inlineMD(s string) string {
	s = html.EscapeString(s)
	s = strings.ReplaceAll(s, "**", "\x00") // 临时占位，避免嵌套
	var b strings.Builder
	inCode := false
	for i := 0; i < len(s); i++ {
		if s[i] == '`' {
			if inCode {
				b.WriteString("</code>")
			} else {
				b.WriteString("<code>")
			}
			inCode = !inCode
			continue
		}
		b.WriteByte(s[i])
	}
	out := b.String()
	// 成对 ** → <strong>
	for {
		i := strings.Index(out, "\x00")
		if i < 0 {
			break
		}
		j := strings.Index(out[i+1:], "\x00")
		if j < 0 {
			out = strings.ReplaceAll(out, "\x00", "")
			break
		}
		out = out[:i] + "<strong>" + out[i+1:i+1+j] + "</strong>" + out[i+1+j+1:]
	}
	return out
}
