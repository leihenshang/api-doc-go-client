package collection

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCreateDocInFolderAppearsInTree 回归：分组目录里新建的文档必须能被扫到 ——
// 既出现在集合树（挂在该分组下），也能被 ListDocs/ReadDoc 找到。
// 此前的 bug：scanFile 的文档分支还卡着 dir == docsDir，任意目录的 .md 不被收录，
// 表现就是「新建后树里没有 + 打开提示条目不存在」。
func TestCreateDocInFolderAppearsInTree(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := c.CreateFolder("", "api"); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	d, err := c.CreateDoc("分组文档", "", "api")
	if err != nil {
		t.Fatalf("CreateDoc: %v", err)
	}
	if d.Path != "api/分组文档.md" {
		t.Fatalf("文档应落在分组目录，实际 path=%q", d.Path)
	}

	// ListDocs 能扫到
	list, err := c.ListDocs()
	if err != nil {
		t.Fatalf("ListDocs: %v", err)
	}
	var found bool
	for _, it := range list {
		if it.UID == d.UID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ListDocs 应包含新建的分组文档（uid=%s），实际 %d 条", d.UID, len(list))
	}

	// ReadDoc 能按 uid 读回（否则前端开页签会报「条目不存在」）
	got, err := c.ReadDoc(d.UID)
	if err != nil {
		t.Fatalf("ReadDoc: %v", err)
	}
	if got.Name != "分组文档" || got.Path != d.Path {
		t.Fatalf("ReadDoc 结果不符: %+v", got)
	}

	// 树里挂在 api 分组下
	info, err := c.Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	var inGroup bool
	var walk func(nodes []*Node, parentPath string)
	walk = func(nodes []*Node, parentPath string) {
		for _, n := range nodes {
			if n.Type == "doc" && n.UID == d.UID && parentPath == "api" {
				inGroup = true
			}
			if n.Type == "folder" {
				walk(n.Children, n.Path)
			}
		}
	}
	walk(info.Tree, "")
	if !inGroup {
		t.Fatalf("文档节点应挂在 api 分组下")
	}
}

// TestMoveDocRootToFolder 回归：根级文档（docs/ 默认位）拖入已有分组后，
// 文件应真的落到目标目录，uid 与文件名主干不变。
func TestMoveDocRootToFolder(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := c.CreateFolder("", "api"); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	d, err := c.CreateDoc("根级文档", "", "")
	if err != nil {
		t.Fatalf("CreateDoc: %v", err)
	}
	if d.Path != "docs/根级文档.md" {
		t.Fatalf("根级文档默认路径不符: %q", d.Path)
	}
	if err := c.MoveDoc(d.UID, "api"); err != nil {
		t.Fatalf("MoveDoc: %v", err)
	}
	got, err := c.ReadDoc(d.UID)
	if err != nil {
		t.Fatalf("ReadDoc: %v", err)
	}
	if got.Path != "api/根级文档.md" {
		t.Fatalf("移动后路径不符: %q", got.Path)
	}
	if _, err := os.Stat(filepath.Join(dir, "api", "根级文档.md")); err != nil {
		t.Fatalf("目标文件不存在: %v", err)
	}
}

// TestRootDocStillWorks 回归：docs/ 默认位与根级文档行为不变。
func TestRootDocStillWorks(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	d, err := c.CreateDoc("默认位文档", "", "")
	if err != nil {
		t.Fatalf("CreateDoc: %v", err)
	}
	if d.Path != "docs/默认位文档.md" {
		t.Fatalf("空 folder 应落 docs/，实际 %q", d.Path)
	}
	if _, err := c.ReadDoc(d.UID); err != nil {
		t.Fatalf("ReadDoc: %v", err)
	}
}
