package mcp_test

// guard 的 T3 审计出口（ok / biz_error / denied）经真实 SDK 会话验证。

import (
	"context"
	"sync"
	"testing"

	. "api-doc-go-client/internal/mcp"
	"api-doc-go-client/internal/mcpaudit"
	"api-doc-go-client/internal/mcpauth"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeAuditor 记录 Log 下来的事件（测试用）。
type fakeAuditor struct {
	mu     sync.Mutex
	events []mcpaudit.Event
}

func (f *fakeAuditor) Log(ev mcpaudit.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, ev)
}
func (f *fakeAuditor) Subscribe() (<-chan mcpaudit.Event, func()) {
	return nil, func() {}
}
func (f *fakeAuditor) List(context.Context, mcpaudit.Query) (*mcpaudit.Page, error) {
	return &mcpaudit.Page{}, nil
}
func (f *fakeAuditor) Path() string          { return "" }
func (f *fakeAuditor) ExportTo(string) error { return nil }
func (f *fakeAuditor) Clear() error          { return nil }
func (f *fakeAuditor) Close() error          { return nil }
func (f *fakeAuditor) kinds() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]int{}
	for _, e := range f.events {
		out[e.ResultKind]++
	}
	return out
}
func (f *fakeAuditor) byTool(tool string) *mcpaudit.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.events {
		if f.events[i].Tool == tool {
			return &f.events[i]
		}
	}
	return nil
}

func auditSession(t *testing.T, z mcpauth.Authorizer) (*mcp.ClientSession, *fakeAuditor) {
	t.Helper()
	aud := &fakeAuditor{}
	root := t.TempDir()
	newProjectDir(t, root, "demo", "uid-demo")
	srv, _, err := NewServer(Config{
		Root: root, NewApp: testNewApp, Authorizer: z, Auditor: aud,
		Transport: "http",
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
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "audit-test", Version: "0"}, nil).
		Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs, aud
}

func TestAuditExits(t *testing.T) {
	allow := &recordingAuthorizer{decision: mcpauth.DecisionAllowAlways}
	cs, aud := auditSession(t, allow)

	ctx := context.Background()
	// 1) low 工具：ok，无审批字段
	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_workspaces"}); err != nil {
		t.Fatal(err)
	}
	// 2) 写工具授权放行：ok + approved
	if res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "create_project",
		Arguments: map[string]any{"name": "audit-demo"},
	}); err != nil || res.IsError {
		t.Fatalf("create_project 应成功：%v", err)
	}
	// 3) 业务错误：白名单外 use_workspace
	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "use_workspace",
		Arguments: map[string]any{"workspace": "Z:/no-such-dir"},
	}); err != nil {
		t.Fatal(err)
	}

	kinds := aud.kinds()
	if kinds["ok"] < 2 {
		t.Errorf("应有 >=2 条 ok，实际 %v", kinds)
	}
	if kinds["biz_error"] < 1 {
		t.Errorf("应有 biz_error，实际 %v", kinds)
	}
	low := aud.byTool("list_workspaces")
	if low == nil || low.Approved != nil || low.GrantedBy != "" {
		t.Errorf("low 工具不应带审批字段：%+v", low)
	}
	create := aud.byTool("create_project")
	if create == nil || create.Approved == nil || !*create.Approved || create.GrantedBy != "interactive" {
		t.Errorf("放行的写工具应 approved=true/interactive：%+v", create)
	}
	if create.DurationMs < 0 {
		t.Error("耗时应非负")
	}

	// 4) denied：换拒绝授权器
	cs2, aud2 := auditSession(t, mcpauth.DenyAuthorizer{Reason: "headless-strict"})
	res, err := cs2.CallTool(ctx, &mcp.CallToolParams{
		Name:      "delete_request",
		Arguments: map[string]any{"project": "demo", "uid": "u"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("应被拒绝")
	}
	ev := aud2.byTool("delete_request")
	if ev == nil || ev.ResultKind != "denied" || ev.ResultOK || ev.GrantedBy != "bypass" {
		t.Fatalf("denied 出口审计不对：%+v", ev)
	}
}
