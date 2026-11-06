package mcp

// 文档「结构概览」：让 AI 先看懂一篇长文档的骨架，再决定读哪一节，而不是一上来吞全文。
//
// 为什么放在 MCP 层而不是落盘到 md：
//   - 文档会被 git pull / 同步拉取 / 其它编辑器改，存下来的大纲必然陈旧；
//   - 读取本来就要把正文读进内存，这里只是多走一遍行扫描，成本与读正文同级；
//   - 落盘会引出「外部改了要不要回写」的两难（回写 → 文件凭空变脏、触发同步与 watcher）。
// 因此：派生 + 进程内缓存（按 uid+hash 命中），零迁移、零同步。

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
)

const (
	// docOutlineSuggestChars 全文超过该字符数时，在正文后追加「先看结构」的建议（按字符而非字节，
	// 中英文混排时才公平）。刻意**不自动截断**：首次调用仍给全文，避免破坏既有调用习惯。
	docOutlineSuggestChars = 8000
	docOutlineDefaultDepth = 2
	docOutlineMaxDepth     = 6
	docOutlineLeadRunes    = 60
	docOutlineCacheSize    = 128
)

// DocHeading 大纲里的一个标题：位置 + 规模 + 首句，够 AI 判断「这节要不要读」。
type DocHeading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	Line  int    `json:"line"`
	// EndLine 本节最后一行（下一个标题的前一行；没有下一个就是文末）
	EndLine int `json:"endLine"`
	Lines   int `json:"lines"`
	// Lead 本节首句（跳过列表符号/表格/引用/代码），默认截到 60 字
	Lead string `json:"lead,omitempty"`
	// CodeBlocks / Tables 小节里的代码块与表格数量：区分「示例型」与「说明型」段落
	CodeBlocks int `json:"codeBlocks,omitempty"`
	Tables     int `json:"tables,omitempty"`
}

// DocOutline 一篇文档的完整结构（depth 过滤在渲染时做，缓存里始终是全集）。
type DocOutline struct {
	Lines         int          `json:"lines"`
	Chars         int          `json:"chars"`
	TotalHeadings int          `json:"totalHeadings"`
	Depth         int          `json:"depth"`
	Omitted       int          `json:"omitted,omitempty"`
	Headings      []DocHeading `json:"headings"`
}

// outlineKey 大纲缓存键：uid + 正文摘要。正文一改键就变，天然失效，不需要 TTL。
//
// 用正文哈希而不是文件 hash：list_docs 不取文件 hash 也要能命中同一份缓存。
func outlineKey(uid, content string) string {
	sum := sha256.Sum256([]byte(content))
	return uid + "|" + hex.EncodeToString(sum[:8])
}

// outlineCache 进程内缓存：key = uid|正文哈希。文件一改 hash 就变，天然失效，不需要 TTL。
var outlineCache = &struct {
	sync.RWMutex
	m     map[string]*DocOutline
	order []string
}{m: make(map[string]*DocOutline)}

// cachedOutline 取缓存；未命中就现算并写回（超过上限按先进先出淘汰）。
func cachedOutline(key, content string) *DocOutline {
	if key != "" {
		outlineCache.RLock()
		o, ok := outlineCache.m[key]
		outlineCache.RUnlock()
		if ok {
			return o
		}
	}
	o := buildDocOutline(content)
	if key == "" {
		return o
	}
	outlineCache.Lock()
	if _, exists := outlineCache.m[key]; !exists {
		outlineCache.m[key] = o
		outlineCache.order = append(outlineCache.order, key)
		if len(outlineCache.order) > docOutlineCacheSize {
			old := outlineCache.order[0]
			outlineCache.order = outlineCache.order[1:]
			delete(outlineCache.m, old)
		}
	}
	outlineCache.Unlock()
	return o
}

// buildDocOutline 扫一遍正文得出标题树、每节行范围/规模/首句（跳过围栏代码块里的 # 行）。
func buildDocOutline(content string) *DocOutline {
	lines := splitDocLines(content)
	heads := markdownHeadings(lines)
	out := &DocOutline{
		Lines: len(lines), Chars: len([]rune(content)),
		TotalHeadings: len(heads), Depth: docOutlineMaxDepth,
	}
	for i, h := range heads {
		end := len(lines)
		if i+1 < len(heads) {
			end = heads[i+1].Line - 1
		}
		if end < h.Line {
			end = h.Line
		}
		code, tables := sectionStats(lines, h.Line+1, end)
		out.Headings = append(out.Headings, DocHeading{
			Level: h.Level, Text: h.Text, Line: h.Line, EndLine: end,
			Lines:      end - h.Line + 1,
			Lead:       sectionLead(lines, h.Line+1, end),
			CodeBlocks: code, Tables: tables,
		})
	}
	return out
}

// sectionStats 统计小节里的代码块数量与表格块数量（1-based 闭区间）。
func sectionStats(lines []string, from, to int) (code, tables int) {
	inFence := false
	inTable := false
	for i := from; i <= to && i >= 1 && i <= len(lines); i++ {
		t := strings.TrimSpace(lines[i-1])
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			if !inFence {
				code++
			}
			inFence = !inFence
			inTable = false
			continue
		}
		if inFence {
			continue
		}
		isRow := strings.HasPrefix(t, "|")
		if isRow && !inTable {
			tables++
		}
		inTable = isRow
	}
	return code, tables
}

// sectionLead 取小节首句：跳过空行、标题、围栏代码块、表格、引用与分隔线，去掉列表符号后截断。
func sectionLead(lines []string, from, to int) string {
	inFence := false
	for i := from; i <= to && i >= 1 && i <= len(lines); i++ {
		t := strings.TrimSpace(lines[i-1])
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "|") ||
			strings.HasPrefix(t, ">") || t == "---" || t == "***" || strings.HasPrefix(t, "<!--") {
			continue
		}
		t = strings.TrimPrefix(t, "- ")
		t = strings.TrimPrefix(t, "* ")
		t = strings.TrimPrefix(t, "+ ")
		if len(t) > 2 && t[0] >= '0' && t[0] <= '9' && (t[1] == '.' || t[1] == ')') {
			t = strings.TrimSpace(t[2:])
		}
		return clipRunes(t, docOutlineLeadRunes)
	}
	return ""
}

// withDepth 过滤到指定层级：更深的标题不展开，只统计数量（给 AI 一个「还能更细」的信号）。
func (o *DocOutline) withDepth(depth int) ([]DocHeading, int) {
	if depth <= 0 {
		depth = docOutlineMaxDepth // 内部默认展开全部；get_doc 会显式传 2
	}
	if depth > docOutlineMaxDepth {
		depth = docOutlineMaxDepth
	}
	kept := make([]DocHeading, 0, len(o.Headings))
	omitted := 0
	for _, h := range o.Headings {
		if h.Level > depth {
			omitted++
			continue
		}
		kept = append(kept, h)
	}
	return kept, omitted
}

// renderDocOutlineText 大纲的人类可读形态（结构化内容见 docOutlineData）。
func renderDocOutlineText(o *DocOutline, depth int, leads bool) string {
	heads, omitted := o.withDepth(depth)
	var sb strings.Builder
	if len(heads) == 0 {
		fmt.Fprintf(&sb, "共 %d 行 / %d 字符 · 未发现 Markdown 标题（可直接全文读取，或 startLine+maxLines 分段）\n",
			o.Lines, o.Chars)
		return sb.String()
	}
	fmt.Fprintf(&sb, "共 %d 行 · %d 个标题（%d 字符；depth=%d 展开 %d 个）\n",
		o.Lines, o.TotalHeadings, o.Chars, clampDepth(depth), len(heads))
	for _, h := range heads {
		fmt.Fprintf(&sb, "%sL%d  %s %s · %d–%d 行（%d 行",
			strings.Repeat("  ", maxInt(h.Level-1, 0)), h.Line,
			strings.Repeat("#", h.Level), h.Text, h.Line, h.EndLine, h.Lines)
		if h.CodeBlocks > 0 {
			fmt.Fprintf(&sb, "，%d 个代码块", h.CodeBlocks)
		}
		if h.Tables > 0 {
			fmt.Fprintf(&sb, "，%d 个表格", h.Tables)
		}
		sb.WriteString("）")
		if leads && h.Lead != "" {
			fmt.Fprintf(&sb, "\n%s→ %s", strings.Repeat("  ", maxInt(h.Level-1, 0)), h.Lead)
		}
		sb.WriteString("\n")
	}
	if omitted > 0 {
		fmt.Fprintf(&sb, "（更深层级还有 %d 个标题：传 depth=%d 展开）\n", omitted, minInt(depth+1, docOutlineMaxDepth))
	}
	sb.WriteString("读某一节：get_doc(uid=…, heading=\"标题\")；读全文：不带 outline 参数。\n")
	return sb.String()
}

// briefHeadings 列表页用的极简标题清单（只到指定层级、最多 limit 条），便于「先挑文档」。
func (o *DocOutline) briefHeadings(depth, limit int) []string {
	heads, _ := o.withDepth(depth)
	out := make([]string, 0, minInt(len(heads), limit))
	for _, h := range heads {
		if len(out) >= limit {
			break
		}
		out = append(out, strings.Repeat("#", h.Level)+" "+h.Text)
	}
	return out
}

// docOutlineData 大纲的结构化输出（与 renderDocOutlineText 同源，字段一一对应）。
func docOutlineData(o *DocOutline, depth int, leads bool) map[string]any {
	heads, omitted := o.withDepth(depth)
	list := make([]map[string]any, 0, len(heads))
	for _, h := range heads {
		m := map[string]any{
			"level": h.Level, "text": h.Text, "line": h.Line,
			"endLine": h.EndLine, "lines": h.Lines,
		}
		if leads && h.Lead != "" {
			m["lead"] = h.Lead
		}
		if h.CodeBlocks > 0 {
			m["codeBlocks"] = h.CodeBlocks
		}
		if h.Tables > 0 {
			m["tables"] = h.Tables
		}
		list = append(list, m)
	}
	out := map[string]any{
		"kind": "outline", "lines": o.Lines, "chars": o.Chars,
		"totalHeadings": o.TotalHeadings, "depth": clampDepth(depth),
		"headings": list,
	}
	if omitted > 0 {
		out["omitted"] = omitted
	}
	return out
}

// findDocSection 按标题文本定位小节：从该标题行到「下一个同级或更高级标题」之前。
// occurrence 指定同名标题取第几个（1 基）；name 允许带 # 前缀（`## 安装`）。
func findDocSection(lines []string, name string, occurrence int) (start, end int, err error) {
	want := normalizeHeadingName(name)
	if want == "" {
		return 0, 0, withInvalid(fmt.Errorf("heading 不能为空：传标题文本，如 \"安装\" 或 \"## 安装\""))
	}
	if occurrence <= 0 {
		occurrence = 1
	}
	heads := markdownHeadings(lines)
	hit := -1
	seen := 0
	for i, h := range heads {
		if !strings.EqualFold(normalizeHeadingName(h.Text), want) {
			continue
		}
		seen++
		if seen == occurrence {
			hit = i
			break
		}
	}
	if hit < 0 {
		names := make([]string, 0, minInt(len(heads), 12))
		for i, h := range heads {
			if i >= 12 {
				break
			}
			names = append(names, "「"+h.Text+"」")
		}
		if len(names) == 0 {
			return 0, 0, withNotFound(fmt.Errorf("没有找到标题 %q：这篇文档没有 Markdown 标题，请用 get_doc(outline=true) 看结构", name))
		}
		return 0, 0, withNotFound(fmt.Errorf("没有找到标题 %q（同名标题共 %d 个）；可用标题：%s",
			name, seen, strings.Join(names, "、")))
	}
	level := heads[hit].Level
	end = len(lines)
	for i := hit + 1; i < len(heads); i++ {
		if heads[i].Level <= level {
			end = heads[i].Line - 1
			break
		}
	}
	return heads[hit].Line, end, nil
}

// normalizeHeadingName 归一标题文本：去掉 ## 前缀与行尾 #，压缩空白，便于「用户说的标题」对上。
func normalizeHeadingName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, "#")
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, "#")
	return strings.Join(strings.Fields(s), " ")
}

// headingPathAt 某一行所属的标题路径（「多同步源 > 背景 > 现状」），供搜索命中定位。
func headingPathAt(lines []string, line int) string {
	heads := markdownHeadings(lines)
	var stack []mdHeading
	for _, h := range heads {
		if h.Line > line {
			break
		}
		for len(stack) > 0 && stack[len(stack)-1].Level >= h.Level {
			stack = stack[:len(stack)-1]
		}
		stack = append(stack, h)
	}
	if len(stack) == 0 {
		return ""
	}
	parts := make([]string, 0, len(stack))
	for _, h := range stack {
		parts = append(parts, h.Text)
	}
	return strings.Join(parts, " > ")
}

func clampDepth(d int) int {
	if d <= 0 {
		return docOutlineMaxDepth
	}
	if d > docOutlineMaxDepth {
		return docOutlineMaxDepth
	}
	return d
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
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
	return n, strings.TrimSpace(strings.TrimRight(strings.TrimSpace(s[n:]), "#"))
}
