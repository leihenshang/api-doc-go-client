package mcp_test

import (
	. "api-doc-go-client/internal/mcp" //nolint:revive // 历史测试文件：沿用未加前缀的调用写法
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRegisterToolsAll(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), testNewApp)
	if err != nil {
		t.Fatal(err)
	}
	newProjectDir(t, reg.Root(), "demo", "uid-demo")
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	RegisterTools(server, NewService(reg), false)
	names, err := ListToolNames(context.Background(), server)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"list_projects", "get_project_modules", "get_request_detail", "search_requests",
		"create_request", "create_module", "create_project", "send_request", "update_request", "delete_request",
		// 环境变量管理：1 个读 + 5 个写
		"list_envs", "create_env", "rename_env", "delete_env", "set_env_var", "delete_env_var",
	}
	if len(names) != len(want) {
		t.Fatalf("工具数 %d，期望 %d：%v", len(names), len(want), names)
	}
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("缺少工具 %s", w)
		}
	}
}

func TestRegisterToolsReadOnly(t *testing.T) {
	root := t.TempDir()
	newProjectDir(t, root, "demo", "uid-demo")
	reg, err := NewRegistry(root, testNewApp)
	if err != nil {
		t.Fatal(err)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	RegisterTools(server, NewService(reg), true)
	names, err := ListToolNames(context.Background(), server)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		switch n {
		case "create_request", "create_module", "create_project", "update_request", "delete_request",
			"create_env", "rename_env", "delete_env", "set_env_var", "delete_env_var":
			t.Errorf("只读模式不应注册写工具 %s", n)
		}
	}
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["send_request"] {
		t.Error("只读模式仍应提供 send_request（强制不落盘）")
	}
	if !found["list_envs"] {
		t.Error("只读模式仍应提供 list_envs（查环境变量属于读操作）")
	}
}

func TestToolSchemaGenerated(t *testing.T) {
	root := t.TempDir()
	newProjectDir(t, root, "demo", "uid-demo")
	reg, _ := NewRegistry(root, testNewApp)
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	RegisterTools(server, NewService(reg), false)
	t1, t2 := mcp.NewInMemoryTransports()
	ss, err := server.Connect(context.Background(), t1, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if tool.Description == "" {
			t.Errorf("工具 %s 缺描述", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Errorf("工具 %s 没有输入 schema", tool.Name)
			continue
		}
		// schema 必须能序列化成带 type:object 的 JSON Schema
		b, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Errorf("工具 %s 的 schema 无法序列化: %v", tool.Name, err)
			continue
		}
		if !strings.Contains(string(b), `"object"`) {
			t.Errorf("工具 %s 的 schema 顶层应为 object: %s", tool.Name, string(b))
		}
	}
}
