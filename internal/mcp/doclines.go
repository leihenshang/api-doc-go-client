package mcp

// 长文档的分段读取（get_doc 与 get_request_detail 的 docs 段共用）。
//
// 设计取舍：
//   - 不传 startLine / maxLines / outline = 全文（与旧输出一致；超长时只加一行建议，不截断）
//   - 只有区间读取才带行号（`12│ 正文`）：降低 AI 把行号抄进 content / replaces.old 的机会
//   - hash 始终是全文 hash：只读一段也能带 ifMatch 安全更新
//   - search_docs 返回的 L<行号> 可直接用作 startLine

import (
	"fmt"
	"strings"
)

const (
	docRangeDefaultLines = 100      // 只给 startLine 时的默认段长
	docRangeMaxLines     = 500      // 单次上限：分段读取不能退化成全文
	docLineClipRunes     = 2000     // 单行上限（避免 50 行里塞进一大段 base64）
	docFullHintLines     = 400      // 全文超过该行数时给「建议分段」提示
	docFullHintBytes     = 32 << 10 // 全文超过该字节数时同样提示
)

// LineView 文档正文的读取视图（零值 = 全文）。
type LineView struct {
	Start   int  // 1 基起始行；<=0 视为 1
	Max     int  // 本次最多行数；<=0 用默认，超过上限夹住
	Outline bool // true = 只给 Markdown 标题大纲（带行号）
	Suggest bool // 全文读取时，是否对超长文档附加「建议分段」提示（仅 get_doc 用）
}

// Active 是否要求区间/大纲视图（任一参数出现即进入）。
func (v LineView) Active() bool { return v.Start > 0 || v.Max > 0 || v.Outline }

// splitDocLines 按行切分（统一 CRLF；末尾换行不额外算一行）。
func splitDocLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if text == "" {
		return nil
	}
	text = strings.TrimSuffix(text, "\n")
	return strings.Split(text, "\n")
}

// lineCount 行数（与 splitDocLines 同口径）。
func lineCount(text string) int { return len(splitDocLines(text)) }

// humanBytes 字节数的可读表示（提示用，不追求精确换算）。
func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// renderDocContent 渲染正文：全文 / 行区间 / Markdown 大纲。
// 空内容返回空串（由调用方给「正文为空」的提示）。
func renderDocContent(content string, v LineView) string {
	lines := splitDocLines(content)
	var sb strings.Builder
	switch {
	case v.Outline:
		renderDocOutline(&sb, lines)
	case v.Active():
		renderDocRange(&sb, lines, v)
	case len(lines) == 0:
		return ""
	default:
		if v.Suggest && (len(lines) > docFullHintLines || len(content) > docFullHintBytes) {
			fmt.Fprintf(&sb, "（全文 %d 行 / %s：较长，建议用 startLine+maxLines 分段读取，或 outline=true 先看标题结构）\n",
				len(lines), humanBytes(len(content)))
		}
		sb.WriteString(content)
		if !strings.HasSuffix(content, "\n") {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// renderDocRange 行区间读取：头部给出总量与下一段起点，正文带行号。
func renderDocRange(sb *strings.Builder, lines []string, v LineView) {
	total := len(lines)
	if total == 0 {
		sb.WriteString("（正文为空，没有可读取的行）\n")
		return
	}
	start := v.Start
	if start <= 0 {
		start = 1
	}
	max := v.Max
	if max <= 0 {
		max = docRangeDefaultLines
	}
	if max > docRangeMaxLines {
		max = docRangeMaxLines
	}
	if start > total {
		fmt.Fprintf(sb, "行 %d 起 / 共 %d 行：startLine 超出范围（行号从 1 开始）\n", start, total)
		return
	}
	end := start + max - 1
	if end > total {
		end = total
	}
	if end < total {
		fmt.Fprintf(sb, "行 %d–%d / 共 %d 行（下一段 startLine=%d）；行号仅供阅读，写回时不要带\n\n", start, end, total, end+1)
	} else {
		fmt.Fprintf(sb, "行 %d–%d / 共 %d 行；行号仅供阅读，写回时不要带\n\n", start, end, total)
	}
	width := len(fmt.Sprintf("%d", total))
	clipped := false
	for i := start; i <= end; i++ {
		text := lines[i-1]
		shown := clipRunes(text, docLineClipRunes)
		if shown != text {
			clipped = true
		}
		fmt.Fprintf(sb, "%*d│ %s\n", width, i, shown)
	}
	if clipped {
		fmt.Fprintf(sb, "（部分超长行已截断：单行上限 %d 字）\n", docLineClipRunes)
	}
}

// renderDocOutline Markdown 标题大纲：带行号，便于用 startLine+maxLines 跳读。
func renderDocOutline(sb *strings.Builder, lines []string) {
	heads := markdownHeadings(lines)
	if len(heads) == 0 {
		fmt.Fprintf(sb, "共 %d 行 · 未发现 Markdown 标题（可用 startLine+maxLines 分段读取）\n", len(lines))
		return
	}
	fmt.Fprintf(sb, "共 %d 行 · %d 个标题（L 为行号；用 startLine+maxLines 读取对应段落）\n", len(lines), len(heads))
	for _, h := range heads {
		// 保留层级（#/##/###）：大纲要能看出嵌套关系，而不是一串平铺标题
		fmt.Fprintf(sb, "L%d  %s %s\n", h.Line, strings.Repeat("#", h.Level), h.Text)
	}
}

// mdHeading 一行 Markdown 标题（只认 ATX 形式）。
type mdHeading struct {
	Line  int
	Level int
	Text  string
}

// markdownHeadings 收集标题行；跳过围栏代码块内的 # 行（代码里的注释不算标题）。
func markdownHeadings(lines []string) []mdHeading {
	var out []mdHeading
	inFence := false
	fenceMark := ""
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			mark := trimmed[:3]
			switch {
			case !inFence:
				inFence, fenceMark = true, mark
			case mark == fenceMark:
				inFence = false
			}
			continue
		}
		if inFence {
			continue
		}
		if level, text := parseHeading(trimmed); text != "" {
			out = append(out, mdHeading{Line: i + 1, Level: level, Text: text})
		}
	}
	return out
}

// parseHeading 解析 `### 标题`；不是标题返回 (0, "")。
func parseHeading(s string) (int, string) {
	n := 0
	for n < len(s) && s[n] == '#' {
		n++
	}
	if n == 0 || n > 6 {
		return 0, ""
	}
	if n < len(s) && s[n] != ' ' && s[n] != '\t' {
		return 0, "" // `#标签` 不是标题（Markdown 要求 # 后跟空白）
	}
	return n, strings.TrimSpace(s[n:])
}
