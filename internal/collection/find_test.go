package collection

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFindByUIDFollowsRename 回归：目录改名后，按 uid 能在同父目录里找回。
func TestFindByUIDFollowsRename(t *testing.T) {
	parent := t.TempDir()
	old := filepath.Join(parent, "old-name")
	c, err := Init(old)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	uid := c.UID
	c.StopWatch()

	renamed := filepath.Join(parent, "new-name")
	if err := os.Rename(old, renamed); err != nil {
		t.Fatalf("rename: %v", err)
	}

	got, err := FindByUID(parent, uid)
	if err != nil {
		t.Fatalf("FindByUID 不该报错：%v", err)
	}
	if got != renamed {
		t.Fatalf("期望找回 %s，实际 %q", renamed, got)
	}
}

// TestFindByUIDIgnoresOthers 同父目录里的其它集合 / 普通目录都不该被误认。
func TestFindByUIDIgnoresOthers(t *testing.T) {
	parent := t.TempDir()
	first, err := Init(filepath.Join(parent, "a"))
	if err != nil {
		t.Fatal(err)
	}
	defer first.StopWatch()
	second, err := Init(filepath.Join(parent, "b"))
	if err != nil {
		t.Fatal(err)
	}
	defer second.StopWatch()
	if err := os.MkdirAll(filepath.Join(parent, "plain-dir"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := FindByUID(parent, second.UID)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(parent, "b") {
		t.Fatalf("应命中 b，实际 %q", got)
	}
}

// TestFindByUIDMisses 找不到时返回空串而不是错误：调用方按「目录不存在」处理。
func TestFindByUIDMisses(t *testing.T) {
	parent := t.TempDir()
	if _, err := Init(filepath.Join(parent, "real")); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, parent, uid string }{
		{"uid 不匹配", parent, "no-such-uid"},
		{"uid 为空", parent, ""},
		{"父目录不存在", filepath.Join(parent, "nope"), "x"},
		{"父目录为空", "", "x"},
	}
	for _, tc := range cases {
		got, err := FindByUID(tc.parent, tc.uid)
		if err != nil {
			t.Fatalf("%s：不该报错，实际 %v", tc.name, err)
		}
		if got != "" {
			t.Fatalf("%s：应为空串，实际 %q", tc.name, got)
		}
	}
}

// TestFindByUIDSkipsBrokenManifest 清单损坏的目录跳过，不影响其它候选。
func TestFindByUIDSkipsBrokenManifest(t *testing.T) {
	parent := t.TempDir()
	broken := filepath.Join(parent, "broken")
	if err := os.MkdirAll(broken, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "opencollection.yml"), []byte(":\t: not yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	good, err := Init(filepath.Join(parent, "good"))
	if err != nil {
		t.Fatal(err)
	}
	defer good.StopWatch()

	got, err := FindByUID(parent, good.UID)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(parent, "good") {
		t.Fatalf("应跳过损坏清单命中 good，实际 %q", got)
	}
}
