package mcp_test

// 工具组开关（C 档）与只读模式 list_tools 文案的回归。

import (
	"context"
	"strings"
	"testing"

	appmcp "api-doc-go-client/internal/mcp"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectServer 连接一个已装配好的内存 server（组过滤场景不能复用 startSelfServer）。
func connectServer(t *testing.T, server *sdk.Server) *sdk.ClientSession {
	t.Helper()
	t1, t2 := sdk.NewInMemoryTransports()
	ss, err := server.Connect(context.Background(), t1, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "group-check", Version: "0"}, nil).
		Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// TestToolGroupRegistration 只注册指定组 + 恒开的 session 组；未知组被忽略。
func TestToolGroupRegistration(t *testing.T) {
	root := t.TempDir()
	newProjectDir(t, root, "demo", "uid-demo")
	reg, err := appmcp.NewRegistry(root, testNewApp)
	if err != nil {
		t.Fatal(err)
	}
	server := sdk.NewServer(&sdk.Implementation{Name: "group", Version: "0"}, nil)
	// unknown 组应被 NormalizeGroups 丢弃，不报错
	appmcp.RegisterToolsWithGroups(server, appmcp.NewService(reg), false, []string{"docs", "unknown"})

	names, err := appmcp.ListToolNames(context.Background(), server)
	if err != nil {
		t.Fatal(err)
	}
	// 4 个 session + 7 个 docs
	if len(names) != 11 {
		t.Fatalf("docs 组注册了 %d 个工具，期望 11：%v", len(names), names)
	}
	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
	}
	for _, n := range []string{
		"list_tools", "list_workspaces", "use_workspace", "list_projects",
		"search_docs", "list_docs", "get_doc", "create_doc", "update_doc", "delete_doc", "save_doc_image",
	} {
		if !got[n] {
			t.Errorf("docs 组应注册 %s", n)
		}
	}
	for _, n := range []string{"create_request", "send_request", "create_env", "save_response_example"} {
		if got[n] {
			t.Errorf("docs 组不应注册 %s", n)
		}
	}
}

// TestReadOnlyListToolsWriteHint 只读模式下 mode=write 必须说明「写工具未注册」，
// 而不是笼统地回「没有命中的工具」。
func TestReadOnlyListToolsWriteHint(t *testing.T) {
	root := t.TempDir()
	newProjectDir(t, root, "demo", "uid-demo")
	reg, err := appmcp.NewRegistry(root, testNewApp)
	if err != nil {
		t.Fatal(err)
	}
	server := sdk.NewServer(&sdk.Implementation{Name: "readonly", Version: "0"}, nil)
	appmcp.RegisterTools(server, appmcp.NewService(reg), true)
	cs := connectServer(t, server)

	text, isErr := callTool(t, cs, "list_tools", map[string]any{"mode": "write"})
	if isErr {
		t.Fatalf("list_tools 不应失败：%s", text)
	}
	if !strings.Contains(text, "只读模式") {
		t.Errorf("只读模式应说明写工具未注册：%s", text)
	}
	if strings.Contains(text, "换个关键词") {
		t.Errorf("不应退化成通用空结果提示：%s", text)
	}
}
