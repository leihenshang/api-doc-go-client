package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// allowTestProjectDir 造一个最小可打开的集合目录（含 manifest）。
// 这里是包内测试（要直接验证未导出的白名单判定），不能用 mcp_test 里的同款 helper，故单列一份。
func allowTestProjectDir(t *testing.T, root, name, uid string) string {
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

// 白名单规范化：绝对化、去重（含可写取更宽松）、丢弃空项。
func TestNormalizeAllowDedupe(t *testing.T) {
	dir := t.TempDir()
	got := NormalizeAllow([]AllowDir{
		{Path: ""},
		{Path: dir, Writable: false},
		{Path: dir + string(filepath.Separator)},
		{Path: dir, Writable: true}, // 与上一条同目录：可写取更宽松
		{Path: "  "},
	})
	if len(got) != 1 {
		t.Fatalf("应去重为 1 条，实际 %d: %+v", len(got), got)
	}
	if !got[0].Writable {
		t.Fatalf("重复项应取更宽松的权限（可写）: %+v", got[0])
	}
	if !filepath.IsAbs(got[0].Path) {
		t.Fatalf("路径应绝对化: %s", got[0].Path)
	}
}

// 目录包含判定：自己 / 子目录在内，父目录、同级、越界（..）在外。
func TestAllowForContainment(t *testing.T) {
	base := t.TempDir()
	inner := filepath.Join(base, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	allow := NormalizeAllow([]AllowDir{{Path: base, Writable: true}})

	if _, err := allowedFor(allow, base, false); err != nil {
		t.Fatalf("目录自身应命中: %v", err)
	}
	if _, err := allowedFor(allow, inner, false); err != nil {
		t.Fatalf("子目录应命中: %v", err)
	}
	if _, err := allowedFor(allow, outside, false); err == nil {
		t.Fatalf("白名单外的目录应被拒")
	}
	if _, err := allowedFor(allow, filepath.Join(base, "..", filepath.Base(outside)), false); err == nil {
		t.Fatalf("经 .. 绕到白名单外应被拒")
	}
	// 只读授权：读可以，写被拒
	ro := NormalizeAllow([]AllowDir{{Path: base, Writable: false}})
	if _, err := allowedFor(ro, inner, false); err != nil {
		t.Fatalf("只读目录应允许读: %v", err)
	}
	if _, err := allowedFor(ro, inner, true); err == nil {
		t.Fatalf("只读目录应拒绝写")
	}
}

// 注册表只认白名单内的目录：白名单外即使有集合也看不到/定位不到。
func TestRegistryScopesToAllowList(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	allowTestProjectDir(t, allowed, "inside", "uid-inside")
	allowTestProjectDir(t, outside, "outside", "uid-outside")

	reg, err := NewRegistryAllow([]AllowDir{{Path: allowed, Writable: true}}, nil)
	if err != nil {
		t.Fatalf("NewRegistryAllow: %v", err)
	}
	entries := reg.Entries()
	if len(entries) != 1 || entries[0].Name != "inside" {
		t.Fatalf("只应看到白名单内的项目: %+v", entries)
	}
	if _, err := reg.Find("outside"); err == nil {
		t.Fatalf("白名单外的项目不应能被定位")
	}
	if _, err := reg.Find(filepath.Join(outside, "outside")); err == nil {
		t.Fatalf("白名单外的绝对路径也不应能被定位")
	}
	if _, _, err := reg.WriteApp("inside"); err != nil {
		t.Fatalf("可写授权目录应允许写: %v", err)
	}
}

// 只读授权目录：读工具可用，写工具一律拒绝；创建项目只能在可写目录里。
func TestReadOnlyAllowRejectsWrites(t *testing.T) {
	dir := t.TempDir()
	allowTestProjectDir(t, dir, "ro-proj", "uid-ro")
	reg, err := NewRegistryAllow([]AllowDir{{Path: dir, Writable: false}}, nil)
	if err != nil {
		t.Fatalf("NewRegistryAllow: %v", err)
	}
	if _, _, err := reg.App("ro-proj"); err != nil {
		t.Fatalf("只读目录应允许读: %v", err)
	}
	if _, _, err := reg.WriteApp("ro-proj"); err == nil {
		t.Fatalf("只读目录应拒绝写")
	} else if !strings.Contains(err.Error(), "只读") {
		t.Fatalf("拒绝原因应说明是只读授权: %v", err)
	}
	if _, err := reg.CreateProject("newproj", ""); err == nil {
		t.Fatalf("没有可写授权目录时不应能建项目")
	}

	// 白名单外建项目：显式指定也要被拒（以前 create_project 能拿任意 root）
	writeDir := t.TempDir()
	reg2, err := NewRegistryAllow([]AllowDir{{Path: writeDir, Writable: true}}, nil)
	if err != nil {
		t.Fatalf("NewRegistryAllow: %v", err)
	}
	outside := t.TempDir()
	if _, err := reg2.CreateProjectIn(outside, "pwned", ""); err == nil {
		t.Fatalf("白名单外不应能建项目")
	}
	if _, err := os.Stat(filepath.Join(outside, "pwned")); err == nil {
		t.Fatalf("白名单外不应留下任何目录")
	}
	if _, err := reg2.CreateProject("ok", ""); err != nil {
		t.Fatalf("可写授权目录内应能建项目: %v", err)
	}
}

// 工作目录解析：没传 project 时 Service 如实报错并提示怎么解决；UseWorkspace 回报「该记住哪个项目」；
// 白名单外的目标直接被拒。
//
// 默认目标本身是**会话级**状态、由工具层保存（见 session_test.go 的 TestWorkspaceSelectionIsPerSession），
// 所以这里只验证 Service 这一层的契约：给 key 就用 key、不给就报错。
func TestWorkspaceResolveAndDefault(t *testing.T) {
	dir := t.TempDir()
	allowTestProjectDir(t, dir, "demo", "uid-demo")
	reg, err := NewRegistryAllow([]AllowDir{{Path: dir, Writable: true}}, nil)
	if err != nil {
		t.Fatalf("NewRegistryAllow: %v", err)
	}
	svc := NewService(reg)

	// 没传 project：报错里要能看出怎么解决
	if _, err := svc.ListEnvs("", ""); err == nil {
		t.Fatalf("未指定 project 时应报错")
	} else if !strings.Contains(err.Error(), "use_workspace") {
		t.Fatalf("报错应提示用 use_workspace: %v", err)
	}
	if len(svc.ListWorkspaces("")) != 1 {
		t.Fatalf("应列出 1 个授权工作目录: %+v", svc.ListWorkspaces(""))
	}

	// 白名单外的目录：拒绝
	if _, _, err := svc.UseWorkspace(t.TempDir()); err == nil {
		t.Fatalf("白名单外的工作目录应被拒")
	}

	// 选定工作目录：返回要记住的项目标识，返回值里标好当前默认
	it, key, err := svc.UseWorkspace(dir)
	if err != nil {
		t.Fatalf("UseWorkspace: %v", err)
	}
	if key == "" {
		t.Fatalf("UseWorkspace 应回报要记住的项目标识")
	}
	if !it.Selected {
		t.Fatalf("返回值应标记为当前默认: %+v", it)
	}
	// 工具层就是拿这个 key 去补 project 的：带上它就能省略 project
	if _, err := svc.ListEnvs(key, ""); err != nil {
		t.Fatalf("带上默认目标后应可省略 project: %v", err)
	}
	marked := false
	for _, w := range svc.ListWorkspaces(key) {
		if w.Selected {
			marked = true
		}
	}
	if !marked {
		t.Fatalf("list_workspaces 应标出当前默认工作目录: %+v", svc.ListWorkspaces(key))
	}
	// 也支持按项目标识选定
	it2, key2, err := svc.UseWorkspace("uid-demo")
	if err != nil {
		t.Fatalf("按 uid 选定应可用: %v", err)
	}
	if key2 == "" || !it2.Selected {
		t.Fatalf("按 uid 选定应回报项目标识: %+v / %q", it2, key2)
	}
}

// 空的授权列表（客户端还没配白名单）：服务照常可用，但如实回报「没有任何目录」。
func TestEmptyAllowListIsSafe(t *testing.T) {
	reg, err := NewRegistryAllow(nil, nil)
	if err != nil {
		t.Fatalf("空白名单不应导致启动失败: %v", err)
	}
	if len(reg.Entries()) != 0 {
		t.Fatalf("空白名单不应有任何项目: %+v", reg.Entries())
	}
	svc := NewService(reg)
	if _, err := svc.ListEnvs("any", ""); err == nil {
		t.Fatalf("空白名单下访问项目应报错")
	}
}
