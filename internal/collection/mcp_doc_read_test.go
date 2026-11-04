package collection

// 排查用：MCP 写入文档后，树里能看到、点击却打不开——树点击最终走 ReadDoc(uid)，
// 这里把「创建 → 保存正文 → 树 → 按 uid 读回」整条链路按常见变体跑一遍。

import (
	"strings"
	"testing"
)

func TestMCPCreatedDocIsReadable(t *testing.T) {
	cases := []struct {
		name    string
		folder  string
		uid     string
		content string
	}{
		{"普通文档", "", "mcp-uid-1", "第一行\n第二行"},
		{"带 空格 的名字", "", "mcp-uid-2", "# 标题\n\n正文"},
		{"子目录文档", "用户", "mcp-uid-3", "子目录正文"},
		// Windows 上外部工具/脚本写入常带 CRLF：frontmatter 与正文都要能正确切分
		{"CRLF 正文", "", "mcp-uid-4", "第一行\r\n第二行\r\n"},
		// 中文文件名（MCP 不指定 FileName 时的默认行为）
		{"中文文件名", "", "mcp-uid-5", "中文文件名正文"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestCollection(t)
			if tc.folder != "" {
				if err := c.CreateFolder("", tc.folder); err != nil {
					t.Fatalf("建目录: %v", err)
				}
			}
			d, err := c.CreateDocWithOpts(CreateDocOpts{
				Name: tc.name, UID: tc.uid, Folder: tc.folder,
				OnConflict: DocConflictError,
			})
			if err != nil {
				t.Fatalf("CreateDocWithOpts: %v", err)
			}
			d.Content = tc.content
			if err := c.SaveDoc(d); err != nil {
				t.Fatalf("SaveDoc: %v", err)
			}

			// ① 树里必须有这个节点（用户「能看到」的依据）
			tr, err := c.Tree()
			if err != nil {
				t.Fatalf("Tree: %v", err)
			}
			var nodeUID string
			var walk func([]*Node)
			walk = func(ns []*Node) {
				for _, n := range ns {
					if n.Type == "doc" && n.UID == d.UID {
						nodeUID = n.UID
					}
					walk(n.Children)
				}
			}
			walk(tr)
			if nodeUID == "" {
				t.Fatalf("树里找不到刚创建的文档 uid=%s", d.UID)
			}

			// ② 按树节点的 uid 读回（前端点击走的就是这一步）
			got, err := c.ReadDoc(nodeUID)
			if err != nil {
				t.Fatalf("ReadDoc(%s): %v", nodeUID, err)
			}
			if got.Path != d.Path {
				t.Fatalf("读回的路径不一致: %q != %q", got.Path, d.Path)
			}
			if !strings.Contains(got.Content, "行") && !strings.Contains(got.Content, "正文") {
				t.Fatalf("正文未读到: %q", got.Content)
			}
			// CRLF 场景：frontmatter 之后不能把 `---` 残留进正文
			if strings.HasPrefix(strings.TrimSpace(got.Content), "---") {
				t.Fatalf("正文里残留了 frontmatter 分隔符: %q", got.Content)
			}
		})
	}
}
