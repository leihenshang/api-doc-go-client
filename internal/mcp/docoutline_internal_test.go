package mcp

import (
	"strings"
	"testing"
)

// 大纲提取：围栏代码不算标题、首句跳过代码/表格/引用、行范围与规模正确。
func TestBuildDocOutlineSections(t *testing.T) {
	content := strings.Join([]string{
		"# 顶层",  // 1
		"开场白。",  // 2
		"## 背景", // 3
		"现有实现只允许一个集合绑定一个项目。", // 4
		"",              // 5
		"```go",         // 6
		"# 这是代码注释，不是标题", // 7
		"```",           // 8
		"### 现状",        // 9
		"- 列表首项就是首句",    // 10
		"| 列 | 说明 |",    // 11
		"| --- | --- |", // 12
		"## 方案",         // 13
		"> 引用不算首句",      // 14
		"表格前的一句话。",      // 15
	}, "\n")

	o := buildDocOutline(content)
	if o.TotalHeadings != 4 {
		t.Fatalf("标题数应为 4（围栏里的 # 不算）：%+v", o.Headings)
	}
	if o.Lines != 15 {
		t.Errorf("行数应为 15，实际 %d", o.Lines)
	}
	if o.Chars != len([]rune(content)) {
		t.Errorf("字符数应为 %d，实际 %d", len([]rune(content)), o.Chars)
	}

	byText := map[string]DocHeading{}
	for _, h := range o.Headings {
		byText[h.Text] = h
	}
	bg, ok := byText["背景"]
	if !ok {
		t.Fatalf("缺少「背景」：%+v", o.Headings)
	}
	// 大纲里的行范围只覆盖「该标题自己的正文」，到第一个子标题为止（子标题单独成项）；
	// 按标题读某一节时才会把子标题一起带出来（见 findDocSection）。
	if bg.Line != 3 || bg.EndLine != 8 || bg.Lines != 6 {
		t.Errorf("背景自身行范围应为 3–8（6 行），实际 %d–%d（%d 行）", bg.Line, bg.EndLine, bg.Lines)
	}
	if !strings.Contains(bg.Lead, "现有实现只允许一个集合") {
		t.Errorf("背景首句应跳过代码块：%q", bg.Lead)
	}
	if bg.CodeBlocks != 1 {
		t.Errorf("背景应统计到 1 个代码块，实际 %d", bg.CodeBlocks)
	}

	xz := byText["现状"]
	if xz.Lead != "列表首项就是首句" {
		t.Errorf("首句应去掉列表符号：%q", xz.Lead)
	}
	if xz.Tables != 1 {
		t.Errorf("现状应统计到 1 个表格，实际 %d", xz.Tables)
	}

	fa := byText["方案"]
	if fa.Lead != "表格前的一句话。" {
		t.Errorf("首句应跳过引用块：%q", fa.Lead)
	}

	// depth=2：h3 不展开，只计数
	kept, omitted := o.withDepth(2)
	if len(kept) != 3 || omitted != 1 {
		t.Errorf("depth=2 应展开 3 个、隐藏 1 个，实际 %d/%d", len(kept), omitted)
	}
	// 默认（0）= 全部层级（get_doc 才显式传 2）
	if all, _ := o.withDepth(0); len(all) != 4 {
		t.Errorf("depth=0 应展开全部 4 个，实际 %d", len(all))
	}
}

// 按标题定位小节：范围到「下一个同级或更高级标题」之前，支持同名第 N 个。
func TestFindDocSection(t *testing.T) {
	content := strings.Join([]string{
		"# 一级",    // 1
		"## 安装",   // 2
		"步骤一",     // 3
		"### 依赖",  // 4
		"go 1.26", // 5
		"## 配置",   // 6
		"配置内容",    // 7
		"## 安装",   // 8
		"第二次出现",   // 9
	}, "\n")
	lines := splitDocLines(content)

	start, end, err := findDocSection(lines, "安装", 1)
	if err != nil || start != 2 || end != 5 {
		t.Fatalf("第一次「安装」应为 2–5（含子标题），实际 %d–%d err=%v", start, end, err)
	}
	start, end, err = findDocSection(lines, "## 安装", 2)
	if err != nil || start != 8 || end != 9 {
		t.Fatalf("第二个「安装」应为 8–9（到文末），实际 %d–%d err=%v", start, end, err)
	}
	if _, _, err := findDocSection(lines, "不存在的标题", 1); err == nil {
		t.Fatal("找不到标题应报错")
	} else if !strings.Contains(err.Error(), "没有找到标题") || !strings.Contains(err.Error(), "可用标题") {
		t.Errorf("报错应给出可用标题：%v", err)
	}
	if _, _, err := findDocSection(lines, "  ##   安装  ", 1); err != nil {
		t.Errorf("标题文本应归一（去 # 与多余空白）：%v", err)
	}
}

// 结构化输出必须与文本同源：大纲/分段模式不能偷偷带全文。
func TestDocDetailViewDataModes(t *testing.T) {
	content := "# 标题\n" + strings.Repeat("正文内容。", 200) + "\n## 小节\n小节正文。"
	d := &DocDetail{DocItem: DocItem{UID: "u1", Name: "n", Path: "docs/n.md"}, Content: content, Hash: "h"}

	outline := buildDocView(content, LineView{Outline: true}, outlineKey("u1", content))
	od := docDetailViewData("demo", d, outline)
	if od["kind"] != "outline" {
		t.Fatalf("kind 应为 outline：%v", od)
	}
	if _, has := od["content"]; has {
		t.Fatalf("大纲模式不应带全文（structuredContent 会再吞一次）：%v", od)
	}
	if _, has := od["headings"]; !has {
		t.Fatalf("大纲模式应带 headings：%v", od)
	}

	// 即使全文很长，大纲模式的输出也应远小于全文（防回退）
	if raw, _ := od["headings"].([]map[string]any); len(raw) != 2 {
		t.Errorf("应有 2 个标题，实际 %d", len(raw))
	}

	rng := buildDocView(content, LineView{Heading: "小节"}, outlineKey("u1", content))
	rd := docDetailViewData("demo", d, rng)
	if rd["kind"] != "range" || rd["content"] == content {
		t.Fatalf("按标题读取应只给该节：kind=%v contentLen=%d", rd["kind"], len(toStr(rd["content"])))
	}
	if got := toStr(rd["content"]); !strings.HasPrefix(got, "## 小节") || !strings.Contains(got, "小节正文") {
		t.Errorf("该节应以标题行起始并包含正文：%q", got)
	}

	full := buildDocView(content, LineView{Suggest: true}, "")
	fd := docDetailViewData("demo", d, full)
	if fd["kind"] != "full" || fd["content"] != content {
		t.Fatalf("全文模式应原样返回 content：kind=%v", fd["kind"])
	}
	if _, has := fd["hint"]; has {
		t.Errorf("未超过字符阈值不应有建议：%v", fd["hint"])
	}
}

// 搜索命中要能看出所属标题路径。
func TestHeadingPathAt(t *testing.T) {
	lines := splitDocLines("# 设计\n## 背景\n### 现状\n这里是现状\n## 方案\n方案内容")
	if got := headingPathAt(lines, 4); got != "设计 > 背景 > 现状" {
		t.Errorf("第 4 行应在「设计 > 背景 > 现状」下，实际 %q", got)
	}
	if got := headingPathAt(lines, 6); got != "设计 > 方案" {
		t.Errorf("第 6 行应在「设计 > 方案」下，实际 %q", got)
	}
	if got := headingPathAt(lines, 1); got != "设计" {
		t.Errorf("标题行自身应只含自己，实际 %q", got)
	}
}

// 缓存按「uid + 正文哈希」命中：正文一变就失效。
func TestOutlineCacheKeyedByContent(t *testing.T) {
	a := cachedOutline(outlineKey("u1", "# A"), "# A")
	b := cachedOutline(outlineKey("u1", "# A"), "# A")
	if a != b {
		t.Error("同一正文重复取应命中缓存（同一指针）")
	}
	c := cachedOutline(outlineKey("u1", "# B"), "# B")
	if c == a || c.Headings[0].Text != "B" {
		t.Error("正文变化后应重新计算")
	}
}

func toStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
