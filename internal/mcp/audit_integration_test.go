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

// 审计要能区分「写盘 / 外发 / 改环境」，不能只靠 risk 猜；未走授权的调用 approved 保持 nil。
func TestAuditCapabilityFlags(t *testing.T) {
	cs, aud := auditSession(t, nil) // authz=nil：不做交互授权
	mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "审计读写", "method": "GET", "url": "/a",
	})
	mustCall(t, cs, "list_tools", map[string]any{})
	mustCall(t, cs, "set_env_var", map[string]any{
		"project": "demo", "env": "dev", "name": "k", "value": "v",
	})
	// 网络外发：目标不可达也会记一条 biz_error 审计
	if _, isErr := callTool(t, cs, "send_request", map[string]any{
		"project": "demo", "url": "http://127.0.0.1:1/x", "method": "GET",
	}); !isErr {
		t.Fatal("send_request 应失败（端口 1 不可达）")
	}

	cases := []struct {
		tool                       string
		write, netEgress, envWrite bool
	}{
		{"create_request", true, false, false},
		{"list_tools", false, false, false},
		{"set_env_var", true, false, true},
		{"send_request", false, true, false},
	}
	for _, c := range cases {
		ev := aud.byTool(c.tool)
		if ev == nil {
			t.Errorf("%s 缺审计记录", c.tool)
			continue
		}
		if ev.Write != c.write || ev.NetEgress != c.netEgress || ev.EnvWrite != c.envWrite {
			t.Errorf("%s 能力标记 = write:%v netEgress:%v envWrite:%v，期望 %v/%v/%v",
				c.tool, ev.Write, ev.NetEgress, ev.EnvWrite, c.write, c.netEgress, c.envWrite)
		}
		if ev.Approved != nil {
			t.Errorf("%s 未走授权，approved 应为 nil（未询问），实际 %v", c.tool, *ev.Approved)
		}
	}
}

// 授权结果要落审计：批准来源（interactive/cache/persisted）与弹窗 id 可对账。
type fixedAuthz struct {
	res mcpauth.Result
}

func (f fixedAuthz) Authorize(context.Context, mcpauth.Request) (mcpauth.Result, error) {
	return f.res, nil
}

func TestAuditApprovalFields(t *testing.T) {
	// 允许：approved=true，来源与弹窗 id 原样落盘
	cs, aud := auditSession(t, fixedAuthz{res: mcpauth.Result{
		Decision: mcpauth.DecisionAllow15m, GrantedBy: "interactive", RequestID: "req-42",
	}})
	mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "授权通过", "method": "GET", "url": "/ok",
	})
	ev := aud.byTool("create_request")
	if ev == nil || ev.Approved == nil || !*ev.Approved {
		t.Fatalf("允许后审计应记 approved=true：%+v", ev)
	}
	if ev.GrantedBy != "interactive" || ev.RequestID != "req-42" {
		t.Errorf("批准来源应落审计：grantedBy=%q requestId=%q", ev.GrantedBy, ev.RequestID)
	}

	// 拒绝：approved=false，且工具未执行（结果仍是 denied）
	cs2, aud2 := auditSession(t, fixedAuthz{res: mcpauth.Result{
		Decision: mcpauth.DecisionDeny, Reason: "user",
	}})
	if _, isErr := callTool(t, cs2, "create_request", map[string]any{
		"project": "demo", "name": "被拒", "method": "GET", "url": "/no",
	}); !isErr {
		t.Fatal("拒绝后工具应失败")
	}
	ev2 := aud2.byTool("create_request")
	if ev2 == nil || ev2.Approved == nil || *ev2.Approved {
		t.Fatalf("拒绝后审计应记 approved=false：%+v", ev2)
	}
	if ev2.ResultKind != string(mcpaudit.KindDenied) {
		t.Errorf("拒绝应记 resultKind=denied，实际 %q", ev2.ResultKind)
	}
}
