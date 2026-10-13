package app

import (
	"context"
	"testing"
	"time"

	"api-doc-go-client/internal/mcpauth"
)

// eventRequestID 从事件里取 requestId：进程内 payload 是 mcpauth.Request 结构体，
// 经 SSE/JSON 往返后才是 map[string]any，两种形态都兼容。
func eventRequestID(t *testing.T, ev map[string]any) string {
	t.Helper()
	switch p := ev["payload"].(type) {
	case mcpauth.Request:
		return p.RequestID
	case map[string]any:
		id, _ := p["requestId"].(string)
		return id
	default:
		t.Fatalf("无法识别的事件 payload 类型 %T", ev["payload"])
		return ""
	}
}

// waitRequestEvent 等一条 mcp:authz-request 并返回其 payload。
func waitRequestEvent(t *testing.T, ch <-chan map[string]any) mcpauth.Request {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev["name"] != "mcp:authz-request" {
				continue
			}
			if p, ok := ev["payload"].(mcpauth.Request); ok {
				return p
			}
			t.Fatalf("进程内 payload 应为 mcpauth.Request，得到 %T", ev["payload"])
		case <-deadline:
			t.Fatal("2s 内未收到 mcp:authz-request")
		}
	}
}

// waitEvent 从订阅通道等一条命名事件。
func waitEvent(t *testing.T, ch <-chan map[string]any, name string) map[string]any {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev["name"] == name {
				return ev
			}
		case <-deadline:
			t.Fatalf("2s 内未收到事件 %s", name)
		}
	}
}

func sampleAuthzRequest() mcpauth.Request {
	return mcpauth.Request{
		SessionID: "sess-1", Transport: "http", Tool: "delete_request",
		Risk: mcpauth.RiskHigh, Write: true, Project: "demo",
	}
}

func TestAuthzAllowFlow(t *testing.T) {
	isolateConfigDir(t)
	a := NewApp()
	ch, done := a.SubscribeEvents()
	defer done()

	resCh := make(chan mcpauth.Result, 1)
	go func() {
		r, _ := a.Authorize(context.Background(), sampleAuthzRequest())
		resCh <- r
	}()

	req := waitRequestEvent(t, ch)
	if req.RequestID == "" || req.Tool != "delete_request" {
		t.Fatalf("授权事件字段不对：%+v", req)
	}
	if err := a.AnswerMCPAuthz(req.RequestID, string(mcpauth.DecisionAllow15m)); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-resCh:
		if r.Decision != mcpauth.DecisionAllow15m || r.GrantedBy != "interactive" {
			t.Fatalf("应答结果不对：%+v", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Authorize 未在应答后返回")
	}
	// settled 事件应回发（前端据此关弹窗）
	waitEvent(t, ch, "mcp:authz-settled")
}

func TestAuthzDedupeConcurrent(t *testing.T) {
	isolateConfigDir(t)
	a := NewApp()
	ch, done := a.SubscribeEvents()
	defer done()

	const n = 3
	resCh := make(chan mcpauth.Result, n)
	for i := 0; i < n; i++ {
		go func() {
			r, _ := a.Authorize(context.Background(), sampleAuthzRequest())
			resCh <- r
		}()
	}

	req := waitRequestEvent(t, ch)
	// 等三个 goroutine 都挂上去
	time.Sleep(100 * time.Millisecond)
	if err := a.AnswerMCPAuthz(req.RequestID, string(mcpauth.DecisionDeny)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		select {
		case r := <-resCh:
			if r.Decision != mcpauth.DecisionDeny {
				t.Fatalf("同 key 并发应共享同一决策 deny，得到 %s", r.Decision)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("第 %d 个调用未收到共享决策", i+1)
		}
	}
}

func TestAuthzAnswerUnknown(t *testing.T) {
	isolateConfigDir(t)
	a := NewApp()
	if err := a.AnswerMCPAuthz("nope", "deny"); err == nil {
		t.Fatal("已结束/不存在的请求应答应报错")
	}
	if err := a.AnswerMCPAuthz("x", "bogus"); err == nil {
		t.Fatal("非法决策应报错")
	}
}

func TestAuthzAlwaysPersistsGrant(t *testing.T) {
	isolateConfigDir(t)
	a := NewApp()
	ch, done := a.SubscribeEvents()
	defer done()

	go func() { _, _ = a.Authorize(context.Background(), sampleAuthzRequest()) }()
	req := waitRequestEvent(t, ch)
	if err := a.AnswerMCPAuthz(req.RequestID, string(mcpauth.DecisionAllowAlways)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)

	grants := a.ListMCPGrants()
	if len(grants) != 1 || grants[0].Tool != "delete_request" || grants[0].Project != "demo" {
		t.Fatalf("始终允许应落 1 条授权：%+v", grants)
	}
	if err := a.RemoveMCPGrant("delete_request", "demo"); err != nil {
		t.Fatal(err)
	}
	if len(a.ListMCPGrants()) != 0 {
		t.Fatal("删除后授权表应为空")
	}
}
