package collection

import (
	"strings"
	"testing"
)

// 导出用的 Markdown → HTML：标签必须配平。
// 之前的实现先做全局占位替换再配对，会产生交叉标签与未闭合 <code>。
func TestInlineMDBalancedTags(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"**粗体**", "<strong>粗体</strong>"},
		{"`code`", "<code>code</code>"},
		{"`a`b`", "<code>a</code>b`"},         // 奇数反引号：最后一个是字面量
		{"**a", "**a"},                        // 未闭合的 ** 按字面量
		{"`**a` b**", "<code>**a</code> b**"}, // code span 内的 ** 不参与配对
		{"<script>", "&lt;script&gt;"},        // 转义仍然生效
		{"`<b>`", "<code>&lt;b&gt;</code>"},   // code 内也转义
		{"a|b", "a|b"},                        // 与表格无关的竖线不动
	}
	for _, c := range cases {
		if got := inlineMD(c.in); got != c.want {
			t.Errorf("inlineMD(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// 表格单元格里的 `|` 必须转义，切分时再还原：否则整行列数错位。
func TestMarkdownTablePipeEscaped(t *testing.T) {
	var b strings.Builder
	writeMDSection(&b, "请求头", []KV{{Enabled: true, Name: "X-Multi", Value: "a|b", Description: "含|竖线"}})
	md := b.String()
	if !strings.Contains(md, `a\|b`) || !strings.Contains(md, `含\|竖线`) {
		t.Fatalf("竖线未转义:\n%s", md)
	}
	// 每一行表格都应当能切成 3 列（值里的竖线不再干扰切分）
	tableRows := 0
	for _, line := range strings.Split(md, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		tableRows++
		if cells := splitRow(trimmed); len(cells) != 3 {
			t.Fatalf("表格行应切成 3 列，实际 %d: %q", len(cells), trimmed)
		}
	}
	if tableRows != 3 {
		t.Fatalf("表格应有 3 行（表头/分隔/数据），实际 %d:\n%s", tableRows, md)
	}
	rows := splitRow("| `X-Multi` | `a\\|b` | 含\\|竖线 |")
	if len(rows) != 3 {
		t.Fatalf("转义后的行应切成 3 列，实际 %d: %v", len(rows), rows)
	}
	if rows[1] != "`a|b`" {
		t.Fatalf("第二列 = %q", rows[1])
	}
}

// 表格分隔行判定：只有整行都是 |---|--- 形态才算，普通正文不能误判。
func TestIsTableSeparator(t *testing.T) {
	yes := []string{"|---|---|", "| --- | :---: |", "|-|"}
	for _, s := range yes {
		if !isTableSeparator(s) {
			t.Errorf("%q 应判为分隔行", s)
		}
	}
	no := []string{"", "文本", "| a | b |", "| 值含 - 的正文 |", "普通 --- 分隔线"}
	for _, s := range no {
		if isTableSeparator(s) {
			t.Errorf("%q 不应判为分隔行", s)
		}
	}
}
