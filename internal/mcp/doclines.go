package mcp

// 长文档的读取视图（get_doc / get_request_detail 的 docs 段共用）。
//
// 设计取舍：
//   - 不传参 = 全文（与旧输出一致；超长时只**追加**一行「先看结构」建议，不截断）
//   - outline=true = 结构概览：标题 + 行范围 + 每节首句 + 规模（depth 默认 2）
//   - heading=… = 只读某一节（按标题定位，比让 AI 自己算行号稳）
//   - startLine+maxLines = 按行区间读
//   - 只有区间读取才带行号（`12│ 正文`）：降低 AI 把行号抄进 content / replaces.old 的机会
//   - hash 始终是全文 hash：只读一段也能带 ifMatch 安全更新

import (
	"fmt"
	"strings"
)

const (
	docRangeDefaultLines = 100  // 只给 startLine 时的默认段长
	docRangeMaxLines     = 500  // 单次上限：分段读取不能退化成全文
	docLineClipRunes     = 2000 // 单行上限（避免 50 行里塞进一大段 base64）
)

// LineView 文档正文的读取视图（零值 = 全文）。
type LineView struct {
	Start   int  // 1 基起始行；<=0 视为 1
	Max     int  // 本次最多行数；<=0 用默认，超过上限夹住
	Outline bool // true = 只给结构概览（标题 + 行范围 + 首句）
	Suggest bool // 全文读取时，是否对超长文档追加「先看结构」建议（仅 get_doc 用）

	// Depth 大纲展开到第几级（0 = 默认 2，最大 6）：更深的只统计数量
	Depth int
	// Leads 大纲是否带每节首句（nil = 带）；关了只剩标题，体量再降一个量级
	Leads *bool
	// Heading 只读某一节：按标题文本定位（大小写不敏感，允许带 ## 前缀）
	Heading string
	// HeadingIndex 同名标题取第几个（1 基，默认 1）
	HeadingIndex int
}

// Active 是否要求区间/大纲视图（任一参数出现即进入）。
func (v LineView) Active() bool {
	return v.Start > 0 || v.Max > 0 || v.Outline || strings.TrimSpace(v.Heading) != ""
}

// DocViewResult 一次读取的完整结果。
//
// 为什么文本与结构化输出都从这里取：以前 get_doc 的文本按 outline 截断了，但
// structuredContent 里仍然整篇 content —— 客户端读结构化内容等于又吞了一次全文，
// 「省 token」直接失效。现在两边同源，任何模式都不会偷偷带全文。
type DocViewResult struct {
	Mode string // full | range | outline | empty
	Text string

	Outline *DocOutline
	Depth   int
	Leads   bool

	Start int // range 模式：1 基闭区间
	End   int
	Total int
	// RangeText 区间原文（不带行号前缀，便于 AI 直接引用）
	RangeText string

	Chars int
	Hint  string
	Err   error
}

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

// buildDocView 按视图参数生成正文/大纲/区间结果（文本与结构化共用）。
// cacheKey 为大纲缓存键（uid|hash）；空串表示不缓存（请求内嵌 docs 这类一次性内容）。
func buildDocView(content string, v LineView, cacheKey string) DocViewResult {
	lines := splitDocLines(content)
	res := DocViewResult{Mode: "full", Total: len(lines), Chars: len([]rune(content))}
	if len(lines) == 0 {
		res.Mode = "empty"
		if v.Active() {
			res.Text = "（正文为空，没有可读取的行）\n"
		}
		return res
	}
	switch {
	case v.Outline:
		o := cachedOutline(cacheKey, content)
		res.Mode = "outline"
		res.Outline = o
		res.Depth = clampDepth(v.Depth)
		res.Leads = v.Leads == nil || *v.Leads
		res.Text = renderDocOutlineText(o, res.Depth, res.Leads)
		return res
	case v.Active():
		start, max := v.Start, v.Max
		if name := strings.TrimSpace(v.Heading); name != "" {
			s, e, err := findDocSection(lines, name, v.HeadingIndex)
			if err != nil {
				res.Mode, res.Err, res.Text = "range", err, err.Error()
				return res
			}
			start, max = s, e-s+1
		}
		s, e := rangeBounds(len(lines), start, max)
		res.Mode, res.Start, res.End = "range", s, e
		var sb strings.Builder
		if s == 0 {
			// 越界：把原始 start 交给渲染层，由它给出「超出范围 + 总行数」的可自查文案
			renderDocRange(&sb, lines, LineView{Start: start, Max: max})
			res.Text = sb.String()
			return res
		}
		res.RangeText = strings.Join(lines[s-1:e], "\n")
		renderDocRange(&sb, lines, LineView{Start: s, Max: e - s + 1})
		res.Text = sb.String()
		return res
	}
	// 全文：原样返回；超长时**追加**建议（不截断，保持既有调用习惯）
	var sb strings.Builder
	sb.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		sb.WriteString("\n")
	}
	if v.Suggest && res.Chars >= docOutlineSuggestChars {
		res.Hint = fmt.Sprintf(
			"本篇 %d 字符 / %d 行（%s）：下次可先看结构 → get_doc(uid=…, outline=true)，depth 默认 2；"+
				"只读某一节 → get_doc(uid=…, heading=\"小节标题\")；分段读 → startLine+maxLines。",
			res.Chars, res.Total, humanBytes(len(content)))
		sb.WriteString("\n")
		sb.WriteString(res.Hint)
		sb.WriteString("\n")
	}
	res.Text = sb.String()
	return res
}

// renderDocContent 渲染正文：全文 / 行区间 / 结构概览（无缓存、无结构化）。
func renderDocContent(content string, v LineView) string {
	return buildDocView(content, v, "").Text
}

// rangeBounds 计算闭区间起止（1 基）并夹住越界；空文档返回 (0,0)。
func rangeBounds(total, start, max int) (int, int) {
	if total == 0 {
		return 0, 0
	}
	if start <= 0 {
		start = 1
	}
	if max <= 0 {
		max = docRangeDefaultLines
	}
	if max > docRangeMaxLines {
		max = docRangeMaxLines
	}
	if start > total {
		return 0, 0
	}
	end := start + max - 1
	if end > total {
		end = total
	}
	return start, end
}

// renderDocRange 行区间读取：头部给出总量与下一段起点，正文带行号。
func renderDocRange(sb *strings.Builder, lines []string, v LineView) {
	total := len(lines)
	if total == 0 {
		sb.WriteString("（正文为空，没有可读取的行）\n")
		return
	}
	start, end := rangeBounds(total, v.Start, v.Max)
	if start == 0 {
		fmt.Fprintf(sb, "行 %d 起 / 共 %d 行：startLine 超出范围（行号从 1 开始）\n", maxInt(v.Start, 1), total)
		return
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
