package collection

import (
	"strings"
	"testing"
)

func TestExportMarkdownAndHTML(t *testing.T) {
	c := newTestCollection(t)
	if err := c.CreateFolder("", "用户"); err != nil {
		t.Fatalf("建分组: %v", err)
	}
	r, err := c.CreateRequest("用户", "用户-列表", "GET")
	if err != nil {
		t.Fatalf("建请求: %v", err)
	}
	r.URL = "{{host}}/api/user/list"
	r.Params = []KV{{Name: "page", Value: "1", Enabled: true, Description: "页码"}}
	r.Docs = "## 说明\n- 查询用户"
	if err := c.SaveRequest(r); err != nil {
		t.Fatalf("保存: %v", err)
	}

	md, err := c.ExportMarkdown()
	if err != nil {
		t.Fatalf("ExportMarkdown: %v", err)
	}
	for _, want := range []string{"# ", "用户-列表", "GET", "{{host}}/api/user/list", "page", "查询用户"} {
		if !strings.Contains(md, want) {
			t.Fatalf("Markdown 缺 %q:\n%s", want, md)
		}
	}

	htmlOut, err := c.ExportHTML()
	if err != nil {
		t.Fatalf("ExportHTML: %v", err)
	}
	for _, want := range []string{"<!DOCTYPE html>", "<h1", "<h2", "用户-列表", "<table>", "查询用户", "</html>"} {
		if !strings.Contains(htmlOut, want) {
			t.Fatalf("HTML 缺 %q:\n%s", want, htmlOut)
		}
	}
	// 原始 HTML 应被转义
	if strings.Contains(htmlOut, "<script>") {
		t.Fatalf("HTML 未转义")
	}
}
