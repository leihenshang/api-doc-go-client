package mcp

// 长文档分段读取（doclines.go）的纯函数单测：
// 行数口径、区间头部/行号、超界与夹取、单行截断、大纲与围栏代码块、全文建议提示。

import (
	"strings"
	"testing"
)

func TestSplitDocLines(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"空内容", "", 0},
		{"只有换行", "\n", 1},
		{"无结尾换行", "a\nb", 2},
		{"有结尾换行不多算", "a\nb\n", 2},
		{"CRLF", "a\r\nb\r\nc", 3},
		{"空行单独成行", "a\n\nb", 3},
	}
	for _, c := range cases {
		if got := lineCount(c.in); got != c.want {
			t.Errorf("%s: lineCount=%d，期望 %d", c.name, got, c.want)
		}
	}
	if got := splitDocLines("a\r\nb"); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("CRLF 应统一成 LF：%#v", got)
	}
}

func TestRenderDocRangeHeaderAndNumbers(t *testing.T) {
	content := "l1\nl2\nl3\nl4\nl5\nl6"
	out := renderDocContent(content, LineView{Start: 2, Max: 2})
	if !strings.Contains(out, "行 2–3 / 共 6 行（下一段 startLine=4）") {
		t.Errorf("头部应给出范围、总量与下一段：\n%s", out)
	}
	if !strings.Contains(out, "行号仅供阅读，写回时不要带") {
		t.Errorf("应提示行号仅供阅读：\n%s", out)
	}
	if !strings.Contains(out, "2│ l2") || !strings.Contains(out, "3│ l3") {
		t.Errorf("正文应带行号：\n%s", out)
	}
	if strings.Contains(out, "1│ l1") || strings.Contains(out, "4│ l4") {
		t.Errorf("区间外不应出现：\n%s", out)
	}
}

func TestRenderDocRangeTailHasNoNextHint(t *testing.T) {
	out := renderDocContent("a\nb\nc", LineView{Start: 2, Max: 10})
	if !strings.Contains(out, "行 2–3 / 共 3 行") {
		t.Errorf("末段应夹到总行数：\n%s", out)
	}
	if strings.Contains(out, "下一段") {
		t.Errorf("末段不应提示下一段：\n%s", out)
	}
}

func TestRenderDocRangeOutOfBounds(t *testing.T) {
	out := renderDocContent("a\nb", LineView{Start: 9})
	if !strings.Contains(out, "startLine 超出范围") || !strings.Contains(out, "共 2 行") {
		t.Errorf("越界应明确说明并可自查总量：\n%s", out)
	}
}

func TestRenderDocRangeCapsMaxLines(t *testing.T) {
	lines := make([]string, 600)
	for i := range lines {
		lines[i] = "x"
	}
	out := renderDocContent(strings.Join(lines, "\n"), LineView{Start: 1, Max: 9999})
	if !strings.Contains(out, "行 1–500 / 共 600 行") {
		t.Errorf("单次上限应夹到 500：\n%s", out[:120])
	}
}

func TestRenderDocRangeClipsLongLine(t *testing.T) {
	long := strings.Repeat("字", 2500)
	out := renderDocContent("短行\n"+long, LineView{Start: 2, Max: 1})
	if !strings.Contains(out, "…") || !strings.Contains(out, "单行上限 2000 字") {
		t.Errorf("超长行应截断并说明：\n%s", out[:120])
	}
}

func TestRenderDocOutlineSkipsFenceAndTags(t *testing.T) {
	content := strings.Join([]string{
		"# 顶层",
		"正文",
		"## 二级",
		"```go",
		"# 这是代码注释，不是标题",
		"```",
		"~~~",
		"## 波浪线围栏里也不算",
		"~~~",
		"#标签不是标题",
		"### 结尾",
	}, "\n")
	out := renderDocContent(content, LineView{Outline: true})
	if !strings.Contains(out, "共 11 行 · 3 个标题") {
		t.Errorf("标题数应排除围栏内容与 #标签：\n%s", out)
	}
	for _, want := range []string{"L1  # 顶层", "L3  ## 二级", "L11  ### 结尾"} {
		if !strings.Contains(out, want) {
			t.Errorf("大纲缺少 %q：\n%s", want, out)
		}
	}
	for _, bad := range []string{"代码注释", "波浪线围栏", "#标签"} {
		if strings.Contains(out, bad) {
			t.Errorf("大纲不应包含 %q：\n%s", bad, out)
		}
	}
}

func TestRenderDocOutlineNoHeadings(t *testing.T) {
	out := renderDocContent("a\nb", LineView{Outline: true})
	if !strings.Contains(out, "未发现 Markdown 标题") {
		t.Errorf("无标题应给出替代建议：\n%s", out)
	}
}

func TestRenderDocFullReadHintsOnlyWhenLong(t *testing.T) {
	short := renderDocContent("a\nb", LineView{Suggest: true})
	if strings.Contains(short, "建议用 startLine") {
		t.Errorf("短文档不应加建议：\n%s", short)
	}
	lines := make([]string, docFullHintLines+1)
	for i := range lines {
		lines[i] = "x"
	}
	long := renderDocContent(strings.Join(lines, "\n"), LineView{Suggest: true})
	if !strings.Contains(long, "建议用 startLine+maxLines 分段读取") {
		t.Errorf("超长全文应加建议：\n%s", long[:150])
	}
	if strings.Count(long, "x") <= docFullHintLines {
		t.Errorf("加建议不能截断正文")
	}
	// 不要求提示时（create/update 的正文回显）保持原样
	if plain := renderDocContent(strings.Join(lines, "\n"), LineView{}); strings.Contains(plain, "建议用 startLine") {
		t.Errorf("Suggest=false 不应加建议")
	}
}

func TestRenderDocContentEmpty(t *testing.T) {
	if out := renderDocContent("", LineView{}); out != "" {
		t.Errorf("空内容全文应返回空串：%q", out)
	}
	if out := renderDocContent("", LineView{Start: 1}); !strings.Contains(out, "正文为空") {
		t.Errorf("空内容区间读取应明说：%q", out)
	}
}
