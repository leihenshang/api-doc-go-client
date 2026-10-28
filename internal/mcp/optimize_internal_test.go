package mcp

// 本轮 MCP 优化（doc/mcp/mcp优化清单20261028.md）的纯函数单测：
// 覆盖「落盘确认」的行级定位、搜索的整词/片段、参数归一化与局部替换的前置校验。
// 工具层的端到端断言见 optimize_test.go（走内存 server，检查 AI 真正读到的文本）。

import (
	"strings"
	"testing"
)

func TestChangePreviewReplaceMiddleLine(t *testing.T) {
	kind, body := changePreview("a\nb\nc", "a\nB\nc")
	if kind != "第 2–2 行 ±1 行" {
		t.Errorf("改动行标注不符：%q", kind)
	}
	if body != "a\nB\nc" {
		t.Errorf("片段应带前后各 1 行：%q", body)
	}
}

func TestChangePreviewInsertLine(t *testing.T) {
	kind, body := changePreview("a\nc", "a\nb\nc")
	if kind != "第 2–2 行 ±1 行" {
		t.Errorf("新增行的标注不符：%q", kind)
	}
	if !strings.Contains(body, "b") {
		t.Errorf("片段应包含新增行：%q", body)
	}
}

func TestChangePreviewDeleteLine(t *testing.T) {
	kind, body := changePreview("a\nb\nc", "a\nc")
	if !strings.Contains(kind, "行附近 ±1 行") {
		t.Errorf("纯删除应标注「行附近」：%q", kind)
	}
	if strings.Contains(body, "b") || !strings.Contains(body, "a") {
		t.Errorf("片段应反映删除后的内容：%q", body)
	}
}

func TestChangePreviewWholeRewriteFallsBackToHead(t *testing.T) {
	kind, body := changePreview("old", "new")
	if kind != "前 5 行" {
		t.Errorf("整篇重写应回落前 5 行：%q", kind)
	}
	if body != "new" {
		t.Errorf("片段不符：%q", body)
	}
}

func TestChangePreviewEmptyNew(t *testing.T) {
	if kind, body := changePreview("a", ""); kind != "" || body != "" {
		t.Errorf("清空正文时没有可确认的内容：kind=%q body=%q", kind, body)
	}
	if kind, body := changePreview("a", "   \n"); kind != "" || body != "" {
		t.Errorf("纯空白同样视为清空：kind=%q body=%q", kind, body)
	}
}

func TestChangePreviewCRLF(t *testing.T) {
	kind, body := changePreview("a\r\nb", "a\r\nc")
	if kind != "第 2–2 行 ±1 行" {
		t.Errorf("CRLF 输入应正常定位：%q", kind)
	}
	if strings.Contains(body, "\r") {
		t.Errorf("片段应统一成 LF：%q", body)
	}
}

func TestChangePreviewClipsLongRegion(t *testing.T) {
	long := strings.Repeat("长", 500)
	kind, body := changePreview("head\ntail", "head\n"+long+"\ntail")
	if kind != "第 2–2 行 ±1 行" {
		t.Errorf("标注不符：%q", kind)
	}
	if !strings.HasSuffix(body, "…") {
		t.Errorf("超长片段应截断并补省略号")
	}
	if n := len([]rune(body)); n > 401 {
		t.Errorf("片段上限 400 字符（+省略号），实际 %d", n)
	}
}

func TestWholeWordHitBoundaries(t *testing.T) {
	cases := []struct {
		name, text, query string
		want              bool
	}{
		{"独立单词", "user list", "user", true},
		{"长标识符内不命中", "superuser", "user", false},
		{"URL 片段按斜杠分词", "/ministryreportingunits/list", "ministryreportingunits", true},
		{"长标识符作为更长单词前缀不命中", "ministryreportingunitsfoo", "ministryreportingunits", false},
		{"大小写不敏感", "MinistryReportingUnits", "ministryreportingunits", true},
		{"中文按非词字符边界", "用户列表", "用户", true},
		{"中文连续串仍可命中", "重新用户列表", "用户", true},
		{"空查询不命中", "user", "", false},
	}
	for _, c := range cases {
		if got := wholeWordHit(c.text, c.query); got != c.want {
			t.Errorf("%s: wholeWordHit(%q, %q)=%v，期望 %v", c.name, c.text, c.query, got, c.want)
		}
	}
}

func TestSnippetAroundTakesNeighborLines(t *testing.T) {
	text := "第一行\n第二行含 needle\n第三行\n第四行"
	snip := snippetAround(text, "needle")
	if !strings.Contains(snip, "第一行") || !strings.Contains(snip, "needle") || !strings.Contains(snip, "第三行") {
		t.Errorf("应取命中行±1：%q", snip)
	}
	if strings.Contains(snip, "第四行") {
		t.Errorf("不应带更远的行：%q", snip)
	}
	// 找不到原样位置（归一化差异）时回落首个非空行
	fallback := snippetAround("\n\n内容在此", "不存在")
	if fallback != "内容在此" {
		t.Errorf("找不到位置应回落首个非空行：%q", fallback)
	}
	// 超长行截断到 200 字符
	long := strings.Repeat("x", 300)
	if n := len([]rune(snippetAround(long, "x"))); n > 201 {
		t.Errorf("片段上限 200 字符（+省略号），实际 %d", n)
	}
}

func TestScoreCandidateExactVsFuzzy(t *testing.T) {
	super := candidate{Name: "superuser"}
	if score, _, _ := scoreCandidate(super, "user", SearchOptions{Exact: true}); score != 0 {
		t.Errorf("整词模式不应命中更长单词的内部：%d", score)
	}
	if score, _, _ := scoreCandidate(super, "user", SearchOptions{}); score == 0 {
		t.Errorf("模糊模式仍应命中（子串）：%d", score)
	}
	right := candidate{Name: "user-list", Method: "GET", URL: "{{host}}/user"}
	if score, _, _ := scoreCandidate(right, "user", SearchOptions{Exact: true}); score == 0 {
		t.Errorf("整词模式应命中独立单词：%d", score)
	}

	// fields 限定：只搜 docs 时，名称命中不参与打分
	withDocs := candidate{Name: "user", Docs: "用户说明"}
	onlyDocs := map[string]bool{"docs": true}
	if score, _, _ := scoreCandidate(withDocs, "user", SearchOptions{Fields: onlyDocs}); score != 0 {
		t.Errorf("只搜 docs 时不应因名称命中得分：%d", score)
	}
	if score, _, snip := scoreCandidate(withDocs, "用户", SearchOptions{Fields: onlyDocs}); score == 0 || snip == "" {
		t.Errorf("只搜 docs 时应命中说明并给片段：score=%d snip=%q", score, snip)
	}
}

func TestNormalizeDetailSections(t *testing.T) {
	if sel, err := NormalizeDetailSections(nil); err != nil || sel != nil {
		t.Errorf("不传应返回 nil（全量）：%v %v", sel, err)
	}
	sel, err := NormalizeDetailSections([]string{" DOCS ", "examples"})
	if err != nil || !sel["docs"] || !sel["examples"] {
		t.Errorf("应归一大小写与空白：%v %v", sel, err)
	}
	if _, err := NormalizeDetailSections([]string{"bogus"}); err == nil || !strings.Contains(err.Error(), "可选值") {
		t.Errorf("未知分段应报错并列可选值：%v", err)
	}
}

func TestNormalizeSearchFields(t *testing.T) {
	if fields, err := NormalizeSearchFields(nil); err != nil || fields != nil {
		t.Errorf("不传应返回 nil（全部字段）：%v %v", fields, err)
	}
	fields, err := NormalizeSearchFields([]string{"Name", "uri"})
	if err != nil || !fields["name"] || !fields["uri"] {
		t.Errorf("应归一大小写：%v %v", fields, err)
	}
	if _, err := NormalizeSearchFields([]string{"bogus"}); err == nil || !strings.Contains(err.Error(), "可选值") {
		t.Errorf("未知字段应报错：%v", err)
	}
}

func TestApplyReplaces(t *testing.T) {
	// 默认只替换第一处
	if got, err := applyReplaces("x x", []ReplaceOp{{Old: "x", New: "y"}}); err != nil || got != "y x" {
		t.Errorf("默认应只替换首处：%q %v", got, err)
	}
	// replaceAll
	if got, err := applyReplaces("x x", []ReplaceOp{{Old: "x", New: "y", ReplaceAll: true}}); err != nil || got != "y y" {
		t.Errorf("replaceAll 应替换全部：%q %v", got, err)
	}
	// 顺序应用（前一步的结果参与后一步匹配）
	if got, err := applyReplaces("a", []ReplaceOp{{Old: "a", New: "b"}, {Old: "b", New: "c"}}); err != nil || got != "c" {
		t.Errorf("应按顺序应用替换：%q %v", got, err)
	}
	// old 找不到：整体报错
	if _, err := applyReplaces("abc", []ReplaceOp{{Old: "zz", New: "y"}}); err == nil || !strings.Contains(err.Error(), "未匹配") {
		t.Errorf("old 找不到应报错：%v", err)
	}
	// 空 old：报错（避免把整篇误替换）
	if _, err := applyReplaces("abc", []ReplaceOp{{Old: "", New: "y"}}); err == nil || !strings.Contains(err.Error(), "不能为空") {
		t.Errorf("空 old 应报错：%v", err)
	}
}

func TestPreviewFirstLines(t *testing.T) {
	if got := previewFirstLines("", 5); got != "" {
		t.Errorf("空文本应返回空：%q", got)
	}
	got := previewFirstLines("1\n2\n3\n4\n5\n6", 5)
	if strings.Contains(got, "6") || !strings.Contains(got, "5") {
		t.Errorf("应只取前 5 行：%q", got)
	}
	long := strings.Repeat("y", 300)
	if n := len([]rune(previewFirstLines(long, 1))); n > 201 {
		t.Errorf("单行应截到 200 字符（+省略号），实际 %d", n)
	}
}
