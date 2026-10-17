package syncengine

import (
	"testing"

	"api-doc-go-client/internal/collection"
)

// TestParentDir 覆盖文档目录归属计算：根级文件与 docs/ 默认位都应视为「未分组」。
func TestParentDir(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"docs/a.md", "docs"},
		{"api/a.md", "api"},
		{"api/sub/a.md", "api/sub"},
		{"a.md", ""},
		{"README.md", ""},
		{"./docs/a.md", "docs"},
	}
	for _, c := range cases {
		if got := parentDir(c.in); got != c.want {
			t.Errorf("parentDir(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestFolderUIDByDir 目录 → 分组 uid：真实分组命中；docs/ 与裸目录返回空串（未分组）。
func TestFolderUIDByDir(t *testing.T) {
	dir := t.TempDir()
	c, err := collection.Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// 建一个分组（folder.yml），保证 Info().Tree 里有该分组节点
	if err := c.CreateFolder("", "api"); err != nil {
		t.Fatalf("create folder: %v", err)
	}

	e := &Engine{Coll: c}
	if uid := e.folderUIDByDir("api"); uid == "" {
		t.Errorf("folderUIDByDir(api) 应为分组 uid，得到空串")
	}
	if uid := e.folderUIDByDir("docs"); uid != "" {
		t.Errorf("folderUIDByDir(docs) 应为空串（docs/ 不是分组），得到 %q", uid)
	}
	if uid := e.folderUIDByDir("nope"); uid != "" {
		t.Errorf("folderUIDByDir(nope) 应为空串，得到 %q", uid)
	}
}

// TestCollectDocOpsTombstone 本地已删除的文档要推 delete（J11-2.2）：
// Bind.DocHashes 里有记录、ListDocs 里已没有 → 产生 delete op。
func TestCollectDocOpsTombstone(t *testing.T) {
	dir := t.TempDir()
	c, err := collection.Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// 先建一篇文档并「推送」过（写进 DocHashes）
	d, err := c.CreateDoc("待删除文档", "", "")
	if err != nil {
		t.Fatalf("create doc: %v", err)
	}
	e := &Engine{Coll: c, Bind: BindInfo{DocHashes: map[string]string{d.UID: "hash-old"}}}

	// 删除该文档（进 .trash），再收集：应产生一条 delete op
	if err := c.DeleteDoc(d.UID); err != nil {
		t.Fatalf("delete doc: %v", err)
	}
	b := newPushBatch(8)
	e.collectDocOps(b)
	var deletes int
	for _, op := range b.ops {
		if op.Op == "delete" && op.Type == "doc" && op.UID == d.UID {
			deletes++
		}
	}
	if deletes != 1 {
		t.Fatalf("应产生 1 条 doc delete op，实际 %d", deletes)
	}
}
