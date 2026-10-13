package mcp_test

// 授权链经真实 SDK 会话的端到端行为（T2）。

import (
	"context"
	"strings"
	"testing"

	. "api-doc-go-client/internal/mcp"
	"api-doc-go-client/internal/mcpauth"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// callClient 启一个带授权器的内存服务并返回已连接的客户端会话。
func authzSession(t *testing.T, z mcpauth.Authorizer) *mcp.ClientSession {
	t.Helper()
	root := t.TempDir()
	newProjectDir(t, root, "demo", "uid-demo")
	srv, _, err := NewServer(Config{
		Root:       root,
		NewApp:     testNewApp,
		Authorizer: z,
		Transport:  "http",
	})
	if err != nil {
		t.Fatal(err)
	}
	t1, t2 := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(context.Background(), t1, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "authz-test", Version: "0"}, nil).
		Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func toolText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

func TestAuthzDenyRejectsRiskTool(t *testing.T) {
	cs := authzSession(t, mcpauth.DenyAuthorizer{Reason: "headless-strict"})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "delete_request",
		Arguments: map[string]any{"project": "demo", "uid": "nonexistent"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(toolText(t, res), "拒绝") {
		t.Fatalf("无头严格模式应拒绝风险工具：%+v %s", res.IsError, toolText(t, res))
	}
}

func TestAuthzLowRiskBypasses(t *testing.T) {
	rec := &recordingAuthorizer{calls: 0}
	cs := authzSession(t, rec)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_workspaces"})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("只读工具不应被拦截：%s", toolText(t, res))
	}
	if rec.calls != 0 {
		t.Fatalf("low 风险工具不应触发授权器，实际 %d 次", rec.calls)
	}
}

func TestAuthzAllowExecutes(t *testing.T) {
	rec := &recordingAuthorizer{decision: mcpauth.DecisionAllowAlways}
	cs := authzSession(t, rec)
	// create_project 真的会在临时 Root 下建目录，成功文本含「已创建项目」
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "create_project",
		Arguments: map[string]any{"name": "authz-demo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || !strings.Contains(toolText(t, res), "已创建") {
		t.Fatalf("授权放行后应执行成功：isError=%v text=%s", res.IsError, toolText(t, res))
	}
	if rec.calls != 1 {
		t.Fatalf("应恰好询问 1 次，实际 %d", rec.calls)
	}
}

type recordingAuthorizer struct {
	calls    int
	decision mcpauth.Decision
}

func (r *recordingAuthorizer) Authorize(_ context.Context, _ mcpauth.Request) (mcpauth.Result, error) {
	r.calls++
	d := r.decision
	if d == "" {
		d = mcpauth.DecisionDeny
	}
	return mcpauth.Result{Decision: d, GrantedBy: "interactive"}, nil
}
