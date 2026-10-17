package collection

// 导出 HTML 的标签白名单守卫（P2-20）：白名单是「只读常量表」，这里把它当契约测，
// 而不是依赖人工 review 发现「谁又加了个 iframe」。

import (
	"strings"
	"testing"
)

// TestHTMLAllowedTagsAreSafe 危险标签绝不能进白名单。
func TestHTMLAllowedTagsAreSafe(t *testing.T) {
	dangerous := []string{
		"script", "iframe", "object", "embed", "form", "input", "button",
		"link", "style", "meta", "svg", "math", "base", "template", "frame",
	}
	for _, tag := range dangerous {
		if isAllowedHTMLTag(tag) {
			t.Fatalf("危险标签 %q 不应在白名单里", tag)
		}
		// 大小写混写同样要挡住
		if isAllowedHTMLTag(strings.ToUpper(tag)) {
			t.Fatalf("危险标签 %q（大写）不应通过", tag)
		}
	}
	// 常用排版标签必须在
	for _, tag := range []string{"p", "code", "pre", "table", "td", "ul", "li", "a", "img"} {
		if !isAllowedHTMLTag(tag) {
			t.Fatalf("排版标签 %q 应在白名单里", tag)
		}
	}
}

// TestSanitizeDropsExecutableMarkup 端到端：带 script / onerror / javascript: 的内容导出后
// 不能残留可执行片段（用户自己的集合也可能从别处粘进来一段 HTML）。
func TestSanitizeDropsExecutableMarkup(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	req, err := c.CreateRequest("", "注入用例", "GET")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	req.Docs = `<p>ok</p><script>alert(1)</script><img src=x onerror="alert(2)">` +
		`<a href="javascript:alert(3)">x</a><iframe src="//evil"></iframe>`
	if err := c.SaveRequest(req); err != nil {
		t.Fatalf("SaveRequest: %v", err)
	}
	html, err := c.ExportHTML()
	if err != nil {
		t.Fatalf("ExportHTML: %v", err)
	}
	lower := strings.ToLower(html)
	// 模板自身内嵌恰好一段交互脚本（目录折叠/搜索），除此之外不允许出现任何 script。
	if got := strings.Count(lower, "<script>"); got != 1 {
		t.Fatalf("应只有模板自带的 1 段 <script>，got %d（用户内容未转义？）", got)
	}
	for _, bad := range []string{"onerror=", "javascript:", "<iframe"} {
		if strings.Contains(lower, bad) {
			t.Fatalf("导出结果不应包含可执行片段 %q", bad)
		}
	}
	// 内容不能被「删掉了事」：script 应作为转义文本保留下来（既不执行也不丢失）。
	if !strings.Contains(html, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatal("script 内容应以转义文本保留")
	}
	// 正常内容必须保留，别把清洗做成「全删」
	if !strings.Contains(html, "注入用例") {
		t.Fatal("正文应保留")
	}
}
