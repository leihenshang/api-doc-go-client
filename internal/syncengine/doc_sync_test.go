package syncengine

import (
	"os"
	"path/filepath"
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
// 另留一篇存活文档：审核 C#1 后「扫空 + 基线有货」会被防呆拒绝，差量墓碑仅在清单非空时生效。
func TestCollectDocOpsTombstone(t *testing.T) {
	dir := t.TempDir()
	c, err := collection.Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// 建两篇文档；A「已推送」后删除，B 存活保证清单非空
	a, err := c.CreateDoc("待删除文档", "", "")
	if err != nil {
		t.Fatalf("create doc a: %v", err)
	}
	if _, err := c.CreateDoc("存活文档", "", ""); err != nil {
		t.Fatalf("create doc b: %v", err)
	}
	e := &Engine{Coll: c, Bind: BindInfo{DocHashes: map[string]string{a.UID: "hash-old"}}}

	// 删除该文档（进 .trash），再收集：应产生一条 delete op
	if err := c.DeleteDoc(a.UID); err != nil {
		t.Fatalf("delete doc: %v", err)
	}
	b := newPushBatch(8)
	e.collectDocOps(b)
	var deletes int
	for _, op := range b.ops {
		if op.Op == "delete" && op.Type == "doc" && op.UID == a.UID {
			deletes++
		}
	}
	if deletes != 1 {
		t.Fatalf("应产生 1 条 doc delete op，实际 %d", deletes)
	}
}

// TestCollectDocOpsEmptyGuard 审核 C#1 防呆：磁盘上仍有 .md 而 ListDocs 扫空、基线有货时，
// 视为扫描异常拒绝批量删除——宁可不推，也决不把远端整仓文档删光。
// 注意 B24 起「frontmatter 解析失败」不再丢弃文档（回落为全文正文 + 路径哈希 uid），
// 故这里用**扫描白名单排除**的子目录 README.md 构造「文件在、清单空」：
// scanExtAllowed 明确跳过子目录 README.md，但 hasAnyMarkdown 仍能看到它。
// （磁盘真无 .md 的合法墓碑场景由 P1-10 的 TestP110ExternalDocRemovalStillEmitsDocDelete 把关）
func TestCollectDocOpsEmptyGuard(t *testing.T) {
	dir := t.TempDir()
	c, err := collection.Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "README.md"), []byte("# 子目录说明\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = c
	e := &Engine{Coll: c, Bind: BindInfo{DocHashes: map[string]string{
		"uid-a": "h1", "uid-b": "h2",
	}}}
	b := newPushBatch(8)
	e.collectDocOps(b)
	for _, op := range b.ops {
		if op.Op == "delete" {
			t.Fatalf("扫空异常场景不应产生任何删除 op，got %+v", op)
		}
	}
}

// TestCollectExampleOpsEmptyGuard 审核 C#1 同形：examples 目录仍有文件而清单扫空时不产生删除。
func TestCollectExampleOpsEmptyGuard(t *testing.T) {
	dir := t.TempDir()
	c, err := collection.Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "examples"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "examples", "junk.yml"), []byte("meta:\n  uid: [oops\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := &Engine{Coll: c, Bind: BindInfo{ExampleHashes: map[string]string{"uid-x": "h1"}}}
	b := newPushBatch(8)
	e.collectExampleOps(b)
	for _, op := range b.ops {
		if op.Op == "delete" {
			t.Fatalf("示例扫空异常不应产生删除 op，got %+v", op)
		}
	}
}
