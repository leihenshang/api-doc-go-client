package mcpauth

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeUI 记录被询问次数并按脚本应答。
type fakeUI struct {
	calls    int
	decision Decision
	wait     chan struct{} // 非 nil 时先等待（模拟在途/并发场景）
}

func (f *fakeUI) Authorize(_ context.Context, _ Request) (Result, error) {
	f.calls++
	if f.wait != nil {
		<-f.wait
	}
	return Result{Decision: f.decision, GrantedBy: "interactive"}, nil
}

func TestDenyAuthorizer(t *testing.T) {
	d := DenyAuthorizer{}
	res, err := d.Authorize(context.Background(), Request{Tool: "send_request"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision != DecisionDeny || res.Reason != "headless-strict" {
		t.Fatalf("期望 headless-strict 拒绝，得到 %+v", res)
	}
}

func TestChainPersistedBypassesUI(t *testing.T) {
	ui := &fakeUI{decision: DecisionDeny} // UI 全拒，但持久授权应优先命中
	store := NewGrantStore([]Grant{{Tool: "delete_request", Project: "demo", CreatedAt: 1}})
	z := NewChain(store, time.Minute, ui)
	res, err := z.Authorize(context.Background(), Request{Tool: "delete_request", Project: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision != DecisionAllowAlways || res.GrantedBy != "persisted" || ui.calls != 0 {
		t.Fatalf("持久授权应免询问：%+v calls=%d", res, ui.calls)
	}
	// 未命中持久表 → 落到 UI（被拒）
	res, _ = z.Authorize(context.Background(), Request{Tool: "delete_request", Project: "other"})
	if res.Decision != DecisionDeny || ui.calls != 1 {
		t.Fatalf("未命中持久授权应询问 UI：%+v calls=%d", res, ui.calls)
	}
}

func TestGrantWildcards(t *testing.T) {
	store := NewGrantStore([]Grant{
		{Tool: "*", Project: ""},                 // 全工具任意项目
		{Tool: "set_env_var", Project: "proj-x"}, // 精确
	})
	if !store.Match("create_request", "anything") {
		t.Error("* 通配应命中任意工具/项目")
	}
	if !store.Match("set_env_var", "proj-x") {
		t.Error("精确授权应命中")
	}
	store.Remove("set_env_var", "proj-x")
	if store.Match("set_env_var", "proj-x") != true { // "*" 仍覆盖
		t.Error("删精确项后 * 仍应命中")
	}
	store.Remove("*", "")
	if store.Match("create_request", "x") {
		t.Error("清空通配后不应再命中")
	}
}

func TestCacheTTL(t *testing.T) {
	ui := &fakeUI{decision: DecisionAllow15m}
	z := NewCachedAuthorizer(20*time.Millisecond, ui)
	req := Request{SessionID: "s1", Tool: "delete_request", Project: "p"}
	for i := 0; i < 3; i++ {
		res, _ := z.Authorize(context.Background(), req)
		if res.Decision != DecisionAllow15m {
			t.Fatalf("第 %d 次应放行", i+1)
		}
	}
	if ui.calls != 1 {
		t.Fatalf("TTL 内应只询问 1 次，实际 %d", ui.calls)
	}
	time.Sleep(30 * time.Millisecond)
	_, _ = z.Authorize(context.Background(), req)
	if ui.calls != 2 {
		t.Fatalf("TTL 过期后应再次询问，实际 calls=%d", ui.calls)
	}
}

func TestCacheSessionIsolation(t *testing.T) {
	ui := &fakeUI{decision: DecisionAllow15m}
	z := NewCachedAuthorizer(time.Minute, ui)
	_, _ = z.Authorize(context.Background(), Request{SessionID: "s1", Tool: "t", Project: "p"})
	_, _ = z.Authorize(context.Background(), Request{SessionID: "s2", Tool: "t", Project: "p"})
	if ui.calls != 2 {
		t.Fatalf("不同会话授权不应互认，期望询问 2 次，实际 %d", ui.calls)
	}
}

func TestCachePropagatesUIError(t *testing.T) {
	bad := AuthorizerFunc(func(context.Context, Request) (Result, error) {
		return Result{}, errors.New("ui gone")
	})
	z := NewCachedAuthorizer(time.Minute, bad)
	if _, err := z.Authorize(context.Background(), Request{}); err == nil {
		t.Fatal("UI 错误应透传")
	}
}

// AuthorizerFunc 测试用函数式授权器。
type AuthorizerFunc func(context.Context, Request) (Result, error)

func (f AuthorizerFunc) Authorize(ctx context.Context, req Request) (Result, error) {
	return f(ctx, req)
}

func TestNormalizeGrants(t *testing.T) {
	in := []Grant{
		{Tool: "a", Project: "p"}, {Tool: "a", Project: "p"}, // 重复
		{Tool: "", Project: "x"},     // 空工具名丢弃
		{Tool: "ghost", Project: ""}, // 未知工具（valid 非 nil）丢弃
		{Tool: "*", Project: ""},     // 通配保留
	}
	valid := func(name string) bool { return name == "a" || name == "b" }
	out := NormalizeGrants(in, valid)
	if len(out) != 2 {
		t.Fatalf("去重/校验后应剩 2 条，得到 %v", out)
	}
	for _, g := range out {
		if g.Tool != "a" && g.Tool != "*" {
			t.Errorf("不应保留未知工具: %+v", g)
		}
	}
	// valid=nil 时不校验工具名（config 层用法）
	if len(NormalizeGrants(in, nil)) != 3 {
		t.Fatal("nil 校验时应保留 3 条（去重后）")
	}
}
