package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"api-doc-go-client/internal/config"
	"api-doc-go-client/internal/mcpauth"
)

// App 实现 mcpauth.Authorizer（Authorize 方法在本文件下方）；MCPBackend.SetAuthorizer
// 直接接收 mcpauth.Authorizer —— mcpauth 是不引用任何内部包的叶子包，app 与 mcp 都可依赖它。

// 授权弹窗超时时长（与 mcp 包 guard 的 authzTimeout 对齐）。
const authzDialogTimeout = 60 * time.Second

// MCPGrant 设置页/门面使用的授权条目（与 mcpauth.Grant 同构，单独定义避免绑定层引用内部类型）。
type MCPGrant struct {
	Tool      string `json:"tool"`
	Project   string `json:"project"`
	CreatedAt int64  `json:"createdAt"`
}

func toGrantDTOs(gs []mcpauth.Grant) []MCPGrant {
	out := make([]MCPGrant, 0, len(gs))
	for _, g := range gs {
		out = append(out, MCPGrant{Tool: g.Tool, Project: g.Project, CreatedAt: g.CreatedAt})
	}
	return out
}

// pendingAuthz 一个进行中的授权弹窗：同 (session,tool,project) 的并发调用共享它。
type pendingAuthz struct {
	req   mcpauth.Request
	timer *time.Timer

	mu      sync.Mutex
	waiters []chan mcpauth.Decision
	done    bool
}

func (p *pendingAuthz) subscribe() chan mcpauth.Decision {
	ch := make(chan mcpauth.Decision, 1)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done {
		ch <- mcpauth.DecisionDeny
		return ch
	}
	p.waiters = append(p.waiters, ch)
	return ch
}

// settle 落定决策并唤醒全部等待者（幂等：重复调用只第一次生效，返回是否本次生效）。
func (p *pendingAuthz) settle(d mcpauth.Decision) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done {
		return false
	}
	p.done = true
	if p.timer != nil {
		p.timer.Stop()
	}
	for _, w := range p.waiters {
		select {
		case w <- d:
		default:
		}
	}
	return true
}

// newAuthzRequestID 生成一次性弹窗实例 id（32 hex）。
func newAuthzRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// interactiveAuthOn 读取交互授权开关；nil（旧配置未写该字段）= 默认开。
func interactiveAuthOn(s config.Settings) bool {
	if s.MCP.InteractiveAuth == nil {
		return true
	}
	return *s.MCP.InteractiveAuth
}

// buildMCPAuthorizer 组装「持久表 → 15m 缓存 → App UI 桥」授权链；开关关闭返回 nil。
func (a *App) buildMCPAuthorizer() mcpauth.Authorizer {
	a.mu.Lock()
	on := interactiveAuthOn(a.settings)
	grants := a.settings.MCP.Grants
	a.mu.Unlock()
	if !on {
		return nil
	}
	a.mu.Lock()
	a.mcpGrants = mcpauth.NewGrantStore(grants)
	store := a.mcpGrants
	a.mu.Unlock()
	return mcpauth.NewChain(store, mcpauth.DefaultTTL, a)
}

// Authorize 实现 mcpauth.Authorizer：发弹窗事件并等待前端应答 / 超时 / 调用取消。
// 同 (session,tool,project) 的并发调用共享同一弹窗，一次决策全部生效。
func (a *App) Authorize(ctx context.Context, req mcpauth.Request) (mcpauth.Result, error) {
	key := req.SessionID + "|" + req.Tool + "|" + req.Project

	a.authzMu.Lock()
	if a.mcpPending == nil {
		a.mcpPending = map[string]*pendingAuthz{}
		a.mcpInflight = map[string]*pendingAuthz{}
	}
	// 已有同 key 弹窗：直接挂同一个等待者，不再发事件。
	if p, ok := a.mcpInflight[key]; ok {
		ch := p.subscribe()
		a.authzMu.Unlock()
		return waitAuthz(ctx, ch), nil
	}
	p := &pendingAuthz{req: req}
	req.RequestID = newAuthzRequestID()
	p.req = req
	ch := p.subscribe()
	a.mcpPending[req.RequestID] = p
	a.mcpInflight[key] = p
	p.timer = time.AfterFunc(authzDialogTimeout, func() {
		if p.settle(mcpauth.DecisionDeny) {
			a.finishAuthz(p, mcpauth.DecisionDeny, "timeout")
		}
	})
	a.authzMu.Unlock()

	a.emitAppEvent("mcp:authz-request", req)
	res := waitAuthz(ctx, ch)
	res.RequestID = req.RequestID // 供 T3 审计把调用与弹窗对账
	return res, nil
}

// waitAuthz 等待应答；ctx 取消（连接断开/请求取消）按拒绝处理，不替用户做别的决定。
func waitAuthz(ctx context.Context, ch chan mcpauth.Decision) mcpauth.Result {
	select {
	case d := <-ch:
		return mcpauth.Result{Decision: d, GrantedBy: "interactive", Reason: "user"}
	case <-ctx.Done():
		return mcpauth.Result{Decision: mcpauth.DecisionDeny, Reason: "cancel"}
	}
}

// finishAuthz 弹窗落定后的一次性清理：摘表、回发 settled 事件（通知前端关闭/置超时态）。
func (a *App) finishAuthz(p *pendingAuthz, d mcpauth.Decision, reason string) {
	key := p.req.SessionID + "|" + p.req.Tool + "|" + p.req.Project
	a.authzMu.Lock()
	delete(a.mcpPending, p.req.RequestID)
	delete(a.mcpInflight, key)
	a.authzMu.Unlock()
	a.emitAppEvent("mcp:authz-settled", map[string]any{
		"requestId": p.req.RequestID,
		"decision":  string(d),
		"reason":    reason,
	})
}

// AnswerMCPAuthz 前端对某个授权请求给出决策（Wails 绑定方法 / devserver 反射桥）。
func (a *App) AnswerMCPAuthz(requestID string, decision string) error {
	d := mcpauth.Decision(decision)
	switch d {
	case mcpauth.DecisionAllow15m, mcpauth.DecisionAllowAlways, mcpauth.DecisionDeny:
	default:
		return fmt.Errorf("非法授权决策 %q", decision)
	}
	a.authzMu.Lock()
	p := a.mcpPending[requestID]
	a.authzMu.Unlock()
	if p == nil {
		return errors.New("授权请求不存在或已结束（可能已超时）")
	}

	effective := d
	if d == mcpauth.DecisionAllowAlways {
		if err := a.persistGrant(p.req.Tool, p.req.Project); err != nil {
			// 配置写盘失败：本次按 15 分钟放行，不阻塞操作（设计 §9 异常处理）。
			effective = mcpauth.DecisionAllow15m
		}
	}
	if !p.settle(effective) {
		return errors.New("授权请求已结束")
	}
	a.finishAuthz(p, effective, "user")
	return nil
}

// persistGrant 新增一条「始终允许」：内存表立即生效 + 落盘配置。
func (a *App) persistGrant(tool, project string) error {
	a.mu.Lock()
	store := a.grantStoreLocked()
	store.Add(mcpauth.Grant{Tool: tool, Project: project, CreatedAt: time.Now().Unix()})
	a.settings.MCP.Grants = store.Snapshot()
	s := a.settings.Normalize()
	a.mu.Unlock()
	return config.Save(s)
}

// grantStoreLocked 返回持久授权表（惰性用当前设置里的 grants 播种；调用方持 a.mu）。
func (a *App) grantStoreLocked() *mcpauth.GrantStore {
	if a.mcpGrants == nil {
		a.mcpGrants = mcpauth.NewGrantStore(a.settings.MCP.Grants)
	}
	return a.mcpGrants
}

// ListMCPGrants 返回「始终允许」授权清单（设置页管理用）。
func (a *App) ListMCPGrants() []MCPGrant {
	a.mu.Lock()
	store := a.grantStoreLocked()
	a.mu.Unlock()
	return toGrantDTOs(store.Snapshot())
}

// RemoveMCPGrant 删除一条持久授权（tool+project 精确匹配），立即生效并落盘。
func (a *App) RemoveMCPGrant(tool, project string) error {
	a.mu.Lock()
	store := a.grantStoreLocked()
	store.Remove(tool, project)
	a.settings.MCP.Grants = store.Snapshot()
	s := a.settings.Normalize()
	a.mu.Unlock()
	return config.Save(s)
}

// ClearMCPGrants 清空全部持久授权。
func (a *App) ClearMCPGrants() error {
	a.mu.Lock()
	a.mcpGrants = mcpauth.NewGrantStore(nil)
	a.settings.MCP.Grants = nil
	s := a.settings.Normalize()
	a.mu.Unlock()
	return config.Save(s)
}
