package collection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"api-doc-go-client/internal/config"
)

// safeIndexName：清单里的 uid 是不可信输入，不能直接当文件名（否则可越出配置目录写 sqlite）。
func TestSafeIndexNameRejectsTraversal(t *testing.T) {
	ok := []string{"123e4567-e89b-12d3-a456-426614174000", "uid-alpha", "A_b-9"}
	for _, uid := range ok {
		if got := safeIndexName(uid); got != uid {
			t.Errorf("合法 uid %q 应原样保留，实际 %q", uid, got)
		}
	}
	bad := []string{"", "../../../evil", `..\..\evil`, "a/b", "a:b", "..", ".", strings.Repeat("x", 100)}
	for _, uid := range bad {
		got := safeIndexName(uid)
		if got == uid {
			t.Errorf("非法 uid %q 不应原样使用", uid)
		}
		if strings.ContainsAny(got, `/\:`) {
			t.Errorf("非法 uid %q 的兜底名仍含路径分隔符: %q", uid, got)
		}
	}
	// 同一个 uid 必须稳定（索引文件名不能每次打开都变）
	if safeIndexName("../../evil") != safeIndexName("../../evil") {
		t.Error("兜底索引名必须稳定")
	}
}

// indexPath 必须落在用户配置目录内：UID 越界时也不能把 sqlite 建到目录之外。
func TestIndexPathStaysInsideConfigDir(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("AppData", cfgDir)

	c := &Collection{Dir: t.TempDir(), UID: "../../../../tmp/pwned"}
	got := c.indexPath()
	if strings.Contains(got, "..") {
		t.Fatalf("索引路径不应含 ..: %s", got)
	}
	base, err := config.Dir()
	if err != nil {
		t.Fatalf("定位配置目录: %v", err)
	}
	if !strings.HasPrefix(filepath.Clean(got), filepath.Clean(base)) {
		t.Fatalf("索引路径越出配置目录: %s（配置目录 %s）", got, base)
	}
}

// SaveDoc：Path 必须位于 docs/ 内，不能借它写集合之外的文件。
func TestSaveDocRejectsPathOutsideDocs(t *testing.T) {
	dir := t.TempDir()
	c := &Collection{Dir: dir}
	outside := filepath.Join(filepath.Dir(dir), "pwned.md")
	bad := []string{
		"../pwned.md",
		"../../pwned.md",
		outside,
		"docs/../../pwned.md",
		"notes/a.md", // 不在 docs/ 下
		"docs/a.txt", // 非 .md
	}
	for _, p := range bad {
		err := c.SaveDoc(&DocEntry{UID: "u1", Name: "n", Path: p, Content: "x"})
		if err == nil {
			t.Errorf("非法路径 %q 应被拒绝", p)
		}
	}
	if _, err := os.Stat(outside); err == nil {
		t.Fatalf("越界写入发生了: %s", outside)
	}
	// 合法路径仍要能写
	if err := c.SaveDoc(&DocEntry{UID: "u2", Name: "n", Path: "docs/ok.md", Content: "hello"}); err != nil {
		t.Fatalf("合法文档写入失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "docs", "ok.md")); err != nil {
		t.Fatalf("合法文档未落盘: %v", err)
	}
}

// writeManifest：必须保留客户端不认识的顶层键（整份重写会静默丢数据）。
func TestWriteManifestPreservesUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	original := "opencollection: 1.0.0\ninfo:\n    name: old\nmeta:\n    uid: uid-1\nvendor:\n    tool: other\n    keep: yes\n"
	path := filepath.Join(dir, "opencollection.yml")
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Collection{Dir: dir, UID: "uid-1", Name: "new"}
	if err := c.writeManifest(); err != nil {
		t.Fatalf("writeManifest: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "vendor:") || !strings.Contains(text, "keep: yes") {
		t.Fatalf("未知顶层键被丢弃:\n%s", text)
	}
	if !strings.Contains(text, "name: new") {
		t.Fatalf("名称未更新:\n%s", text)
	}
	if !strings.Contains(text, "uid: uid-1") {
		t.Fatalf("uid 丢失:\n%s", text)
	}
}

// ReadMeta 只读清单：不得产生写盘/索引副作用（扫描项目列表会批量调用它）。
func TestReadMetaHasNoSideEffects(t *testing.T) {
	dir := t.TempDir()
	manifest := "opencollection: 1.0.0\ninfo:\n    name: demo\nmeta:\n    uid: uid-meta\n"
	path := filepath.Join(dir, "opencollection.yml")
	if err := os.WriteFile(path, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadMeta(dir)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if meta.Name != "demo" || meta.UID != "uid-meta" {
		t.Fatalf("ReadMeta 结果错误: %+v", meta)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != manifest {
		t.Fatalf("ReadMeta 改写了清单:\n%s", after)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
		t.Fatalf("ReadMeta 产生了额外文件: %v %v", entries, err)
	}
}
