package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	. "api-doc-go-client/internal/mcp" //nolint:revive // 与同目录测试文件保持一致的调用写法

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCP 是给外部 AI 用的：它的输出里不能出现真实文件路径。
//
// 路径一旦出去，AI 就可以绕开集合直接读写文件（改源码、删目录），
// 而客户端能管的只有「白名单 + 工具」这一层。所以对外一律用别名（ws_1）与
// 相对标识；入参仍然兼容绝对路径（旧调用方），但回显必须脱敏。
func TestNoRealPathsInMCPOutput(t *testing.T) {
	root := t.TempDir()
	proj := sessionProjectDir(t, root, "demo", "uid-demo")

	server, _, err := NewServer(Config{
		Allow:  []AllowDir{{Path: root, Writable: true}},
		NewApp: testNewApp,
		Logger: silentLogger(),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	defer ts.Close()
	cs := connectHTTP(t, ts)

	assertClean := func(t *testing.T, name, text string) {
		t.Helper()
		if strings.Contains(text, root) {
			t.Fatalf("%s 的输出里出现了真实路径 %s：\n%s", name, root, text)
		}
	}

	// ① list_workspaces：只有别名，没有绝对路径
	text, isErr := callTool(t, cs, "list_workspaces", nil)
	if isErr {
		t.Fatalf("list_workspaces 失败：%s", text)
	}
	assertClean(t, "list_workspaces", text)
	if !strings.Contains(text, "ws_1") {
		t.Fatalf("list_workspaces 应给出别名 ws_1：\n%s", text)
	}

	// ② 用别名就能选定工作目录（外部拿不到路径也照样能用）
	if text, isErr := callTool(t, cs, "use_workspace", map[string]any{"workspace": "ws_1"}); isErr {
		t.Fatalf("按别名 use_workspace 失败：%s", text)
	}

	// ③ 入参给绝对路径（兼容旧调用方）：结果里必须已经换成别名
	text, isErr = callTool(t, cs, "get_project_modules", map[string]any{"project": proj})
	if isErr {
		t.Fatalf("get_project_modules 失败：%s", text)
	}
	assertClean(t, "get_project_modules(绝对路径入参)", text)

	// ④ 只读/白名单外的报错也不能把授权目录写出来
	if text, isErr := callTool(t, cs, "create_project", map[string]any{"name": "另一个", "root": proj}); !isErr {
		t.Fatalf("在项目目录里新建项目应被拒：%s", text)
	} else {
		assertClean(t, "create_project 报错", text)
	}

	// ⑤ 结构化输出同理：ID 是别名，不是路径
	for _, it := range NewService(mustRegistry(t, root)).ListWorkspaces("") {
		if filepath.IsAbs(it.ID) || strings.Contains(it.ID, string(filepath.Separator)) {
			t.Fatalf("WorkspaceItem.ID 应是别名而不是路径：%q", it.ID)
		}
	}
}

// mustRegistry 造一个单根注册表（只为上面的结构化断言取 ListWorkspaces）。
func mustRegistry(t *testing.T, root string) *Registry {
	t.Helper()
	reg, err := NewRegistryAllow([]AllowDir{{Path: root, Writable: true}}, testNewApp)
	if err != nil {
		t.Fatalf("NewRegistryAllow: %v", err)
	}
	return reg
}
