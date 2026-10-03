package mcp_test

import (
	"api-doc-go-client/internal/app"
	. "api-doc-go-client/internal/mcp" //nolint:revive // 历史测试文件：沿用未加前缀的调用写法
	"os"
	"path/filepath"
	"testing"
)

// newProjectDir 造一个最小可打开的集合目录（含 manifest）。
func newProjectDir(t *testing.T, root, name, uid string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "opencollection: 1.0.0\ninfo:\n    name: " + name + "\nmeta:\n    uid: " + uid + "\n"
	if err := os.WriteFile(filepath.Join(dir, "opencollection.yml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRegistryScanAndLocate(t *testing.T) {
	root := t.TempDir()
	newProjectDir(t, root, "alpha", "uid-alpha")
	newProjectDir(t, root, "beta", "uid-beta")
	// 非项目目录：没有 manifest
	if err := os.MkdirAll(filepath.Join(root, "notaproject"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Bruno 集合：应被跳过并给出原因
	bruno := filepath.Join(root, "bruno-col")
	if err := os.MkdirAll(bruno, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bruno, "bruno.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRegistry(root, testNewApp)
	if err != nil {
		t.Fatal(err)
	}
	entries := r.Entries()
	if len(entries) != 4 {
		t.Fatalf("扫描到 %d 项，期望 4（含 2 个跳过项）", len(entries))
	}
	byPath := map[string]Entry{}
	for _, e := range entries {
		byPath[e.Path] = e
	}
	if e := byPath["alpha"]; e.UID != "uid-alpha" || e.Skipped() != "" {
		t.Errorf("alpha 识别错误: %+v", e)
	}
	if e := byPath["notaproject"]; e.Skipped() == "" {
		t.Errorf("非项目目录应被标记跳过: %+v", e)
	}
	if e := byPath["bruno-col"]; e.Skipped() == "" {
		t.Errorf("Bruno 集合应被跳过: %+v", e)
	}

	// 三种定位方式都应命中 alpha
	for _, key := range []string{"uid-alpha", "alpha", "alpha"} {
		e, err := r.Find(key)
		if err != nil {
			t.Fatalf("按 %q 定位失败: %v", key, err)
		}
		if e.UID != "uid-alpha" {
			t.Errorf("按 %q 定位到了 %s", key, e.UID)
		}
	}
	if _, err := r.Find("不存在"); err == nil {
		t.Error("定位不存在的项目应报错")
	}
	if _, err := r.Find(""); err == nil {
		t.Error("空 project 应报错")
	}
}

func TestRegistryLazyOpenAndCreateProject(t *testing.T) {
	root := t.TempDir()
	newProjectDir(t, root, "alpha", "uid-alpha")
	r, err := NewRegistry(root, testNewApp)
	if err != nil {
		t.Fatal(err)
	}
	// 未打开时统计为空
	if e := r.Entries()[0]; e.Requests != 0 {
		t.Errorf("未打开的项目不应有统计: %+v", e)
	}
	a1, e1, err := r.App("alpha")
	if err != nil {
		t.Fatal(err)
	}
	a2, _, err := r.App("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if a1 != a2 {
		t.Error("同一项目应复用同一个 App 实例（避免重复打开监听）")
	}
	if e1.UID != "uid-alpha" {
		t.Errorf("打开后应带上 uid: %+v", e1)
	}

	// 创建项目：目录 + manifest 落盘，并立刻出现在列表里
	p, err := r.CreateProject("新项目", "new-proj")
	if err != nil {
		t.Fatal(err)
	}
	if p.Path != "new-proj" || p.Name != "新项目" || p.UID == "" {
		t.Errorf("创建项目结果异常: %+v", p)
	}
	if _, err := os.Stat(filepath.Join(root, "new-proj", "opencollection.yml")); err != nil {
		t.Errorf("manifest 未落盘: %v", err)
	}
	if _, err := r.CreateProject("新项目", "new-proj"); err == nil {
		t.Error("同名项目应拒绝重复创建")
	}
	if _, err := r.CreateProject("非法/名字", "x"); err == nil {
		t.Error("非法项目名应被拒绝")
	}
}

// testNewApp Registry 打开项目用的运行时工厂（真实 App 实例，与独立 mcpserver 同构）。
func testNewApp() ProjectApp { return app.NewApp() }
