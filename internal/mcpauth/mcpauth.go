// Package mcpauth 是 MCP 工具「交互式授权」的纯逻辑层：决策模型、持久授权表、
// 15 分钟内存缓存、授权链与无头严格模式。
//
// 单独成包（而不是放 internal/mcp）是因为依赖方向：internal/mcp 已 import internal/app，
// app 不能再反向 import mcp；授权的 UI 桥由 app 实现、guard 由 mcp 调用，
// 两边都只能依赖这个不引用任何内部包的叶子包。
package mcpauth

import (
	"context"
	"strings"
	"sync"
	"time"
)

// Decision 用户对一次风险调用的决策。
type Decision string

const (
	DecisionAllow15m    Decision = "allow_15m"    // 本会话 15 分钟内不再询问
	DecisionAllowAlways Decision = "allow_always" // 始终允许（落盘配置）
	DecisionDeny        Decision = "deny"         // 本次拒绝
)

// Risk 与 internal/mcp 的 RiskLevel 同值（low/medium/high），用字符串解耦两包。
type Risk string

const (
	RiskLow    Risk = "low"
	RiskMedium Risk = "medium"
	RiskHigh   Risk = "high"
)

// Request 一次授权请求（字段会序列化后发给前端弹窗，不得含未脱敏入参）。
type Request struct {
	RequestID   string `json:"requestId"`
	SessionID   string `json:"sessionId"`
	Transport   string `json:"transport"` // "http" | "stdio"
	Tool        string `json:"tool"`
	Risk        Risk   `json:"risk"`
	Write       bool   `json:"write"`
	NetEgress   bool   `json:"netEgress"`
	Project     string `json:"project"`     // 解析后的目标项目；无法解析为空
	ArgsSummary string `json:"argsSummary"` // 脱敏后的入参摘要 JSON
	ClientHint  string `json:"clientHint"`  // 传输侧线索（stdio / 远端地址）
	CreatedAt   int64  `json:"createdAt"`   // unix 毫秒
}

// Result 授权结论。
type Result struct {
	Decision  Decision      `json:"decision"`
	GrantedBy string        `json:"grantedBy"` // interactive | cache_15m | persisted | bypass
	TTL       time.Duration `json:"-"`
	Reason    string        `json:"reason,omitempty"` // deny 原因：user/timeout/cancel/headless-strict/infrastructure
	// RequestID 交互弹窗实例 id（仅 GrantedBy=interactive 时由 UI 桥回填；
	// cache/persisted 命中无弹窗，留空）。审计用它把调用与弹窗对账。
	RequestID string `json:"requestId,omitempty"`
}

// Authorizer 授权器：由 UI 桥（App）、缓存/持久层组合、或无头 DenyAuthorizer 实现。
// 实现必须在 ctx 取消/超时时返回 DecisionDeny，不得永久阻塞。
type Authorizer interface {
	Authorize(ctx context.Context, req Request) (Result, error)
}

// Grant 一条「始终允许」持久授权（落 config.json）。
type Grant struct {
	Tool      string `json:"tool"`    // 工具名；"*" = 所有非只读工具
	Project   string `json:"project"` // 项目基名；"" = 任意项目
	CreatedAt int64  `json:"createdAt"`
}

// Key 授权匹配键（会话维度只参与 15m 缓存，不进持久授权）。
func cacheKey(sessionID, tool, project string) string {
	return strings.Join([]string{sessionID, tool, project}, "|")
}

func grantKey(tool, project string) string {
	return tool + "|" + project
}

// GrantStore 持久授权表的内存镜像（由 App 持有，跨 MCP 服务重启存活），
// 支持通配：tool="*" 或 project=""（任意项目）。
type GrantStore struct {
	mu    sync.RWMutex
	exact map[string]Grant // tool|project
	wild  []Grant          // tool=* 或 project 为空的条目（量小，线性匹配即可）
}

// NewGrantStore 用配置中的授权初始化。
func NewGrantStore(grants []Grant) *GrantStore {
	s := &GrantStore{exact: map[string]Grant{}}
	for _, g := range grants {
		s.add(g)
	}
	return s
}

func (s *GrantStore) add(g Grant) {
	if g.Tool == "*" || g.Project == "" {
		for i, w := range s.wild {
			if w.Tool == g.Tool && w.Project == g.Project {
				s.wild[i] = g
				return
			}
		}
		s.wild = append(s.wild, g)
		return
	}
	s.exact[grantKey(g.Tool, g.Project)] = g
}

// Add 运行期新增一条「始终允许」（用户刚在弹窗里选择）。
func (s *GrantStore) Add(g Grant) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.add(g)
}

// Match 是否命中持久授权。
func (s *GrantStore) Match(tool, project string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.exact[grantKey(tool, project)]; ok {
		return true
	}
	for _, w := range s.wild {
		if (w.Tool == "*" || w.Tool == tool) && (w.Project == "" || w.Project == project) {
			return true
		}
	}
	return false
}

// Remove 删除精确匹配的一条授权（tool="*" / project="" 按字面值匹配）。
func (s *GrantStore) Remove(tool, project string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.exact, grantKey(tool, project))
	for i, w := range s.wild {
		if w.Tool == tool && w.Project == project {
			s.wild = append(s.wild[:i], s.wild[i+1:]...)
			break
		}
	}
}

// Snapshot 返回当前全部授权（设置页列表 / 落盘用）。
func (s *GrantStore) Snapshot() []Grant {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Grant, 0, len(s.exact)+len(s.wild))
	for _, g := range s.exact {
		out = append(out, g)
	}
	out = append(out, s.wild...)
	return out
}

// NormalizeGrants 配置归一：(tool,project) 去重、丢弃空工具名。
// valid 用于校验工具名是否在 T1 元数据表内（"*" 恒合法）；nil 表示不校验。
func NormalizeGrants(grants []Grant, valid func(string) bool) []Grant {
	seen := map[string]bool{}
	out := make([]Grant, 0, len(grants))
	for _, g := range grants {
		g.Tool = strings.TrimSpace(g.Tool)
		g.Project = strings.TrimSpace(g.Project)
		if g.Tool == "" {
			continue
		}
		if g.Tool != "*" && valid != nil && !valid(g.Tool) {
			continue // 未知工具名（可能来自新版本配置）：丢弃，避免误放行
		}
		k := grantKey(g.Tool, g.Project)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, g)
	}
	return out
}

// persistedAuthorizer 持久授权命中即放行；未命中委托 next。
type persistedAuthorizer struct {
	store *GrantStore
	next  Authorizer
}

func (p *persistedAuthorizer) Authorize(ctx context.Context, req Request) (Result, error) {
	if p.store.Match(req.Tool, req.Project) {
		return Result{Decision: DecisionAllowAlways, GrantedBy: "persisted"}, nil
	}
	return p.next.Authorize(ctx, req)
}

// ttlCache 「允许 15 分钟」内存缓存：仅按会话+工具+项目生效，进程重启即失效。
// 用相对计时（每次懒清理过期项），不依赖绝对时间比较，系统时钟回拨不影响正确性。
type ttlCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]time.Time // key -> 过期时刻
	next    Authorizer
}

// NewCachedAuthorizer 包一层 15 分钟缓存。
func NewCachedAuthorizer(ttl time.Duration, next Authorizer) Authorizer {
	return &ttlCache{ttl: ttl, entries: map[string]time.Time{}, next: next}
}

func (c *ttlCache) hit(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	exp, ok := c.entries[key]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(c.entries, key)
		return false
	}
	return true
}

func (c *ttlCache) put(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = time.Now().Add(c.ttl)
}

func (c *ttlCache) Authorize(ctx context.Context, req Request) (Result, error) {
	key := cacheKey(req.SessionID, req.Tool, req.Project)
	if c.hit(key) {
		return Result{Decision: DecisionAllow15m, GrantedBy: "cache_15m", TTL: c.ttl}, nil
	}
	res, err := c.next.Authorize(ctx, req)
	if err != nil {
		return res, err
	}
	if res.Decision == DecisionAllow15m || res.Decision == DecisionAllowAlways {
		// always 也顺手写进短期缓存：持久表虽立即生效，但缓存能减少对 store 的争用。
		c.put(key)
	}
	return res, nil
}

// DenyAuthorizer 无头严格模式：所有非 low 调用一律拒绝（low 在 guard 处已短路，不会进到这里）。
type DenyAuthorizer struct {
	Reason string
}

func (d DenyAuthorizer) Authorize(_ context.Context, _ Request) (Result, error) {
	reason := d.Reason
	if reason == "" {
		reason = "headless-strict"
	}
	return Result{Decision: DecisionDeny, GrantedBy: "bypass", Reason: reason}, nil
}

// NewChain 组装「持久表 → TTL 缓存 → UI 桥」授权链。
func NewChain(store *GrantStore, ttl time.Duration, ui Authorizer) Authorizer {
	return &persistedAuthorizer{store: store, next: NewCachedAuthorizer(ttl, ui)}
}

// DefaultTTL 「允许 15 分钟」的固定时长。
const DefaultTTL = 15 * time.Minute
