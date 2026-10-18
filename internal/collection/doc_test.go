package collection

import (
	"strings"
	"testing"
)

// TestScanKeepsDocWithMarkdownTable 文档正文带 Markdown 表格时必须仍被扫描到（P0 修复）。
//
// 回归背景：scan 先对整文件做 yaml.Unmarshal 取 head，而文档 frontmatter 之后是 Markdown
// 正文——正文里出现表格 / 连续列表时 YAML 解析失败，文档被当成「非法 YAML」丢掉：
// 保存成功、树里却看不到，用户会以为文档丢了。
func TestScanKeepsDocWithMarkdownTable(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, tc := range []struct{ name, body string }{
		{"带表格", "## 环境变量\n\n| 变量 | 说明 |\n| --- | --- |\n| `host` | 服务地址 |\n"},
		{"带列表", "## 步骤\n\n1. 第一步\n2. 第二步\n"},
		{"带代码块", "## 示例\n\n```bash\ncurl -H \"A: b\" http://x\n```\n"},
		{"纯文本", "一句话正文。\n"},
	} {
		d, err := c.CreateDoc(tc.name, "", "docs")
		if err != nil {
			t.Fatalf("CreateDoc(%s): %v", tc.name, err)
		}
		d.Content = tc.body
		if err := c.SaveDoc(d); err != nil {
			t.Fatalf("SaveDoc(%s): %v", tc.name, err)
		}
	}
	docs, err := c.ListDocs()
	if err != nil {
		t.Fatalf("ListDocs: %v", err)
	}
	if len(docs) != 4 {
		names := make([]string, 0, len(docs))
		for _, d := range docs {
			names = append(names, d.Name)
		}
		t.Fatalf("4 篇文档都应被扫描到，实际 %d: %v", len(docs), names)
	}
	// 正文必须原样保留（只去掉 frontmatter），表格不能丢
	for _, d := range docs {
		if d.Name == "带表格" && !strings.Contains(d.Content, "| `host` | 服务地址 |") {
			t.Fatalf("表格正文丢失: %q", d.Content)
		}
	}
}
