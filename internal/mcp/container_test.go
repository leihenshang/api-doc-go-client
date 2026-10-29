package mcp_test

// 批次 2（清单 20261030）：容器级 CRUD（进 .trash）+ 批量读取 get_docs。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appmcp "api-doc-go-client/internal/mcp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// newSessionWithRoot 建一个自定义根（含 demo + victim 两个项目）的会话。
func newSessionWithRoot(t *testing.T) (string, *sdk.ClientSession) {
	t.Helper()
	root := t.TempDir()
	newProjectDir(t, root, "demo", "uid-demo")
	newProjectDir(t, root, "victim", "uid-victim")
	reg, err := appmcp.NewRegistry(root, testNewApp)
	if err != nil {
		t.Fatal(err)
	}
	server := sdk.NewServer(&sdk.Implementation{Name: "container", Version: "0"}, nil)
	appmcp.RegisterTools(server, appmcp.NewService(reg), false)
	t1, t2 := sdk.NewInMemoryTransports()
	ss, err := server.Connect(context.Background(), t1, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "container-client", Version: "0"}, nil).
		Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return root, cs
}

func TestModuleContainerTools(t *testing.T) {
	_, cs := newSessionWithRoot(t)

	mustCall(t, cs, "create_module", map[string]any{"project": "demo", "name": "API"})
	out := mustCall(t, cs, "rename_module", map[string]any{
		"project": "demo", "path": "API", "name": "接口",
	})
	if !strings.Contains(out, "接口") {
		t.Errorf("重命名返回不符：%s", out)
	}
	mods := mustCall(t, cs, "get_project_modules", map[string]any{"project": "demo"})
	if !strings.Contains(mods, "接口") || strings.Contains(mods, "- API：") {
		t.Errorf("模块名应更新：%s", mods)
	}

	// 非空模块：默认删除必须拒绝（path 是目录名 API；改名只改显示名）
	mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "列表", "method": "GET", "url": "/list", "folder": "API",
	})
	bad, isErr := callTool(t, cs, "delete_module", map[string]any{"project": "demo", "path": "API"})
	if !isErr || !strings.Contains(bad, "[INVALID]") || !strings.Contains(bad, "recursive") {
		t.Errorf("非空模块应拒绝并提示 recursive：%s", bad)
	}
	dry := mustCall(t, cs, "delete_module", map[string]any{
		"project": "demo", "path": "API", "recursive": true, "dryRun": true,
	})
	if !strings.Contains(dry, "dryRun") {
		t.Errorf("dryRun 应只预演：%s", dry)
	}
	if mods := mustCall(t, cs, "get_project_modules", map[string]any{"project": "demo"}); !strings.Contains(mods, "接口") {
		t.Errorf("dryRun 不应真的删除：%s", mods)
	}
	// 同名创建 → [CONFLICT]（统一写契约：不再静默复用）
	if out, isErr := callTool(t, cs, "create_module", map[string]any{"project": "demo", "name": "API"}); !isErr || !strings.Contains(out, "[CONFLICT]") {
		t.Errorf("同名模块应 [CONFLICT]：%s", out)
	}
	ok := mustCall(t, cs, "delete_module", map[string]any{
		"project": "demo", "path": "API", "recursive": true,
	})
	if !strings.Contains(ok, ".trash") {
		t.Errorf("删除应提示回收位：%s", ok)
	}
	mods = mustCall(t, cs, "get_project_modules", map[string]any{"project": "demo"})
	if strings.Contains(mods, "接口") {
		t.Errorf("模块应已删除：%s", mods)
	}

	// 不存在的模块 → [NOT_FOUND]
	if out, isErr := callTool(t, cs, "delete_module", map[string]any{"project": "demo", "path": "不存在"}); !isErr || !strings.Contains(out, "[NOT_FOUND]") {
		t.Errorf("不存在模块应 [NOT_FOUND]：%s", out)
	}
}

func TestPruneEmptyModulesTool(t *testing.T) {
	_, cs := newSessionWithRoot(t)
	mustCall(t, cs, "create_module", map[string]any{"project": "demo", "name": "空模块"})
	out := mustCall(t, cs, "prune_empty_modules", map[string]any{"project": "demo"})
	if !strings.Contains(out, "已清理 1 个空模块目录") {
		t.Errorf("清理数量不符：%s", out)
	}
	mods := mustCall(t, cs, "get_project_modules", map[string]any{"project": "demo"})
	if strings.Contains(mods, "空模块") {
		t.Errorf("空模块应已清理：%s", mods)
	}
}

func TestProjectContainerTools(t *testing.T) {
	root, cs := newSessionWithRoot(t)

	// rename_project：清单显示名更新，列表跟着刷新
	mustCall(t, cs, "rename_project", map[string]any{"project": "demo", "name": "演示项目"})
	list := mustCall(t, cs, "list_projects", map[string]any{})
	if !strings.Contains(list, "演示项目") {
		t.Errorf("list_projects 应显示新名字：%s", list)
	}

	// delete_project：confirm 不符 → [CONFLICT]；正确 → 移入授权根 .trash
	bad, isErr := callTool(t, cs, "delete_project", map[string]any{"project": "victim", "confirm": "随意"})
	if !isErr || !strings.Contains(bad, "[CONFLICT]") {
		t.Errorf("确认名不符应 [CONFLICT]：%s", bad)
	}
	if _, err := os.Stat(filepath.Join(root, "victim")); err != nil {
		t.Errorf("拒绝时不应移动目录：%v", err)
	}
	dry := mustCall(t, cs, "delete_project", map[string]any{"project": "victim", "confirm": "victim", "dryRun": true})
	if !strings.Contains(dry, "dryRun") {
		t.Errorf("dryRun 应只预演：%s", dry)
	}
	if _, err := os.Stat(filepath.Join(root, "victim")); err != nil {
		t.Errorf("dryRun 不应移动目录：%v", err)
	}
	ok := mustCall(t, cs, "delete_project", map[string]any{"project": "victim", "confirm": "victim"})
	if !strings.Contains(ok, ".trash") {
		t.Errorf("删除应提示回收位：%s", ok)
	}
	if _, err := os.Stat(filepath.Join(root, "victim")); !os.IsNotExist(err) {
		t.Errorf("项目目录应已移走")
	}
	entries, err := os.ReadDir(filepath.Join(root, ".trash"))
	if err != nil || len(entries) == 0 {
		t.Errorf("授权根 .trash 应存在且有内容：%v", err)
	}
	list = mustCall(t, cs, "list_projects", map[string]any{})
	if strings.Contains(list, "victim") {
		t.Errorf("已删除项目不应再出现在列表：%s", list)
	}
}

func TestGetDocsBatch(t *testing.T) {
	cs := connectSelf(t)
	uid1 := uidAfter(t, mustCall(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "甲", "folder": "docs", "content": "# 甲",
	}))
	uid2 := uidAfter(t, mustCall(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "乙", "folder": "docs", "content": "# 乙",
	}))

	out := mustCall(t, cs, "get_docs", map[string]any{
		"project": "demo", "uids": []string{uid1, uid2, uid1},
	})
	if !strings.Contains(out, uid1) || !strings.Contains(out, uid2) || !strings.Contains(out, "成功 2 / 2") {
		t.Errorf("批量读取结果不符：%s", out)
	}

	// 混合假 uid：成功的仍返回，失败逐条标注
	mixed := mustCall(t, cs, "get_docs", map[string]any{
		"project": "demo", "uids": []string{uid1, "00000000-0000-0000-0000-000000000000"},
	})
	if !strings.Contains(mixed, "成功 1 / 2") || !strings.Contains(mixed, "读取失败") {
		t.Errorf("混合结果不符：%s", mixed)
	}

	// 超 10 个 → [LIMIT]
	uids := make([]string, 11)
	for i := range uids {
		uids[i] = fmt.Sprintf("missing-%02d", i)
	}
	if out, isErr := callTool(t, cs, "get_docs", map[string]any{"project": "demo", "uids": uids}); !isErr || !strings.Contains(out, "[LIMIT]") {
		t.Errorf("超限应 [LIMIT]：%s", out)
	}
}

// create_project：错误码与 root 语义（评审反馈：此前一律 [INTERNAL]、项目目录当 root 会嵌套）。
func TestCreateProjectErrors(t *testing.T) {
	root, cs := newSessionWithRoot(t)

	// ① 正常：root = 授权工作目录（不是项目目录）
	out := mustCall(t, cs, "create_project", map[string]any{
		"name": "新项目", "dirName": "newproj", "root": root,
	})
	if !strings.Contains(out, "newproj") {
		t.Errorf("创建结果不符：%s", out)
	}
	if _, err := os.Stat(filepath.Join(root, "newproj", "opencollection.yml")); err != nil {
		t.Errorf("项目清单应落盘：%v", err)
	}

	// ② 重名 → [CONFLICT]
	if out, isErr := callTool(t, cs, "create_project", map[string]any{
		"name": "新项目", "dirName": "newproj", "root": root,
	}); !isErr || !strings.Contains(out, "[CONFLICT]") {
		t.Errorf("重名应 [CONFLICT]：%s", out)
	}

	// ③ root 是项目目录本身 → [INVALID] 且给出可操作提示
	if out, isErr := callTool(t, cs, "create_project", map[string]any{
		"name": "嵌套", "dirName": "nested", "root": filepath.Join(root, "demo"),
	}); !isErr || !strings.Contains(out, "[INVALID]") || !strings.Contains(out, "父级工作目录") {
		t.Errorf("项目目录当 root 应 [INVALID] 并提示父级：%s", out)
	}
	if _, err := os.Stat(filepath.Join(root, "demo", "nested")); !os.IsNotExist(err) {
		t.Errorf("不应在项目里嵌套创建")
	}

	// ④ root 不在白名单 → [FORBIDDEN]（不是 [INTERNAL]）
	if out, isErr := callTool(t, cs, "create_project", map[string]any{
		"name": "越权", "root": t.TempDir(),
	}); !isErr || !strings.Contains(out, "[FORBIDDEN]") {
		t.Errorf("白名单外应 [FORBIDDEN]：%s", out)
	}
}

// create_project 不传 root：默认选第一个可写目录；若该目录本身是项目 → [INVALID]（不能嵌套）。
func TestCreateProjectDefaultRootIsProject(t *testing.T) {
	proj := newProjectDir(t, t.TempDir(), "onlyproj", "uid-only")
	reg, err := appmcp.NewRegistryAllow([]appmcp.AllowDir{{Path: proj, Writable: true}}, testNewApp)
	if err != nil {
		t.Fatal(err)
	}
	svc := appmcp.NewService(reg)
	if _, err := svc.CreateProject("", "新项目", ""); err == nil {
		t.Fatalf("授权目录本身是项目时应拒绝")
	} else if appmcp.CodeOf(err) != appmcp.CodeInvalid {
		t.Errorf("应为 [INVALID]，实际 %s: %v", appmcp.CodeOf(err), err)
	}
}
