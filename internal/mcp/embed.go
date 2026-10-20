package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"api-doc-go-client/internal/app"
	"api-doc-go-client/internal/config"
	"api-doc-go-client/internal/mcpaudit"
	"api-doc-go-client/internal/mcpauth"
)

// embedStopTimeout 停服等待上限（ServeHTTP 收到 ctx 取消后做优雅关闭）。
const embedStopTimeout = 3 * time.Second

// embedBackend 是内嵌 MCP 服务的实现（app.MCPBackend）。
//
// 为什么实现放在 internal/mcp 而不是 internal/app：internal/mcp 需要 App 的集合运行时
// （打开 / 发送 / 保存），若 app 反向依赖就会成环。改由 main / devserver 启动时注入
// （app.SetMCPBackend），两侧共用同一份实现。
type embedBackend struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	addr   string
	logger *log.Logger
	// fp/status/last 最近一次 Start 的配置指纹与状态快照：
	// 指纹相同且服务仍在监听时跳过「停→重启」（见 Start），
	// 避免无关的设置保存把已连上的 AI 客户端会话掐断。
	fp     string
	last   *app.MCPRequest
	status *app.MCPStatus
	// svc 运行中的服务层（Refresh 重扫项目用；Stop 时清空）。
	svc *Service
	// newApp 给每个项目一个独立实例（App 一次只能打开一个集合，多项目必须各自一个）。
	newApp NewAppFunc
	// authz 风险工具交互授权器（T2）：App.Startup 注入；nil = 不拦截。
	authz mcpauth.Authorizer
	// auditor 调用审计器（T3）：App.Startup 注入；nil = 不落审计。
	auditor mcpaudit.Auditor
}

// SetAuthorizer 注入交互授权器（App 在 Startup 时把自己作为 UI 桥注入）。
func (b *embedBackend) SetAuthorizer(z mcpauth.Authorizer) {
	b.mu.Lock()
	b.authz = z
	b.mu.Unlock()
}

// SetAuditor 注入调用审计器（T3）。
func (b *embedBackend) SetAuditor(a mcpaudit.Auditor) {
	b.mu.Lock()
	b.auditor = a
	b.mu.Unlock()
}

// toAllowDirs 把设置里的白名单转成 MCP 层的类型（两层各自定义，避免 config 反向依赖 mcp）。
func toAllowDirs(list []config.MCPAllowDir) []AllowDir {
	out := make([]AllowDir, 0, len(list))
	for _, a := range list {
		out = append(out, AllowDir{Path: a.Path, Writable: a.Writable})
	}
	return NormalizeAllow(out)
}

// NewBackend 构造内嵌 MCP 服务实现。logger 为 nil 时丢弃日志。
func NewBackend(logger *log.Logger) app.MCPBackend {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &embedBackend{logger: logger, newApp: defaultNewApp}
}

// requestFingerprint 计算影响监听与工具面的配置指纹（规范化后取 sha256）。
//
// 只纳入会改变服务实例的字段：地址/端口/令牌/只读/开关/来源白名单/目录授权 + 交互授权开关
// （授权链在装配时固化进工具守卫，开关变了必须重启才生效；Grants 内容是共享表，不需要）。
// 「保存但未改动」的请求在这里归一化后逐项相等：空地址与 127.0.0.1 同义、
// 白名单按规范化后的序列参与比较。
func requestFingerprint(req app.MCPRequest) string {
	addr := strings.TrimSpace(req.Addr)
	if addr == "" {
		addr = "127.0.0.1" // normalizeAddr 的兜底语义，指纹须一致
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "enabled=%t\naddr=%s\nport=%d\ntoken=%s\nreadonly=%t\ninteractive=%t\n",
		req.Enabled, addr, req.Port, req.Token, req.ReadOnly, req.InteractiveAuth != nil && *req.InteractiveAuth)
	for _, o := range req.AllowOrigins {
		fmt.Fprintf(&sb, "origin=%s\n", o)
	}
	for _, d := range toAllowDirs(req.Allow) {
		fmt.Fprintf(&sb, "allow=%s|w=%t\n", d.Path, d.Writable)
	}
	sum := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(sum[:])
}

// stillRunning 服务 goroutine 是否还活着（done 未关闭）。
// 调用方持 b.mu；监听协程异常退出（done 已关）时这里返回 false，让 Start 走完整重启。
func (b *embedBackend) stillRunning() bool {
	done := b.done
	if done == nil {
		return false
	}
	select {
	case <-done:
		return false
	default:
		return true
	}
}

// Start 按设置启停服务：Enabled=false 即停止（幂等）。
//
// 配置指纹与运行中实例一致时直接返回状态快照，不重启监听：
// 设置页「没改内容点保存」、以及应用生命周期里多处无条件重放设置，
// 若照常 stop→start 会把 AI 客户端的长连接会话（SSE）全部掐断。
func (b *embedBackend) Start(req app.MCPRequest) (*app.MCPStatus, error) {
	fp := requestFingerprint(req)
	allow := toAllowDirs(req.Allow)
	b.mu.Lock()
	if b.fp == fp && b.stillRunning() {
		st := *b.statusOfLocked() // 返回副本，调用方改不动内部快照
		b.mu.Unlock()
		return &st, nil
	}
	b.mu.Unlock()
	// 与运行中实例不同（或没在跑）：先停旧实例，地址/端口/只读等变更后旧监听必须下线
	b.Stop()

	st := &app.MCPStatus{
		Enabled:   req.Enabled,
		Addr:      req.Addr,
		Port:      req.Port,
		Token:     req.Token,
		ReadOnly:  req.ReadOnly,
		Origins:   req.AllowOrigins,
		Allow:     req.Allow,
		CrossHost: !IsLoopbackAddr(req.Addr),
	}
	if len(allow) > 0 {
		st.Root = allow[0].Path // 展示用：第一个授权目录
	}
	switch {
	case !req.Enabled:
		st.Hint = "MCP 服务未启用"
		b.record(fp, req, st, nil)
		return st, nil
	case len(allow) == 0:
		// 安全默认：白名单为空 = 不授权任何目录。服务照常启动（AI 能拿到明确提示），
		// 但读写不到任何集合 —— 需要用时就到设置里添加工作目录。
		st.Running = false
		st.Hint = "尚未授权任何工作目录：AI 目前无法访问集合。请在「设置 → MCP 服务 → 可访问的工作目录」里添加。"
		b.record(fp, req, st, nil)
		return st, nil
	}
	addr := net.JoinHostPort(req.Addr, strconv.Itoa(req.Port))
	// 端口预检（先占一下再放开）：把「端口被占用」变成同步错误返回给设置页，
	// 否则真正的监听错误发生在 ServeHTTP 的内部 goroutine 里，界面只能显示「未知错误」。
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		st.Error = fmt.Sprintf("无法监听 %s：%v", addr, err)
		st.Hint = "端口可能已被占用（常见于另一个 mcpserver 实例），换一个端口试试"
		b.record(fp, req, st, nil) // 失败也记指纹：但 Running=false，下次 Start 不会被跳过（仍会重试）
		return st, nil
	}
	_ = ln.Close()

	b.mu.Lock()
	authz := b.authz
	auditor := b.auditor
	b.mu.Unlock()
	srv, svc, err := NewServer(Config{
		Allow:      allow,
		ReadOnly:   req.ReadOnly,
		Logger:     b.logger,
		NewApp:     b.newApp,
		Authorizer: authz,
		Auditor:    auditor,
		Transport:  "http",
		ClientHint: "http",
	})
	if err != nil {
		st.Error = fmt.Sprintf("装配 MCP 服务失败：%v", err)
		b.record(fp, req, st, nil)
		return st, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := ServeHTTP(ctx, srv, HTTPOptions{
			Addr:         addr,
			Path:         "/mcp",
			Token:        req.Token,
			AllowOrigins: req.AllowOrigins,
			Logger:       b.logger,
		}); err != nil && ctx.Err() == nil {
			b.logger.Printf("HTTP 服务退出: %v", err)
		}
	}()

	b.mu.Lock()
	b.cancel, b.done, b.addr = cancel, done, addr
	b.mu.Unlock()

	st.Running = true
	st.Projects = svc.ProjectCount()
	shown := req.Addr
	if req.Addr == "0.0.0.0" || req.Addr == "::" {
		shown = "127.0.0.1" // 全网卡监听时用回环地址访问最省事
	}
	st.URL = "http://" + net.JoinHostPort(shown, strconv.Itoa(req.Port)) + "/mcp"
	st.Health = "http://" + net.JoinHostPort(shown, strconv.Itoa(req.Port)) + "/healthz"
	st.Hint = embedHint(st)
	b.record(fp, req, st, svc)
	return st, nil
}

// record 保存本次 Start 的指纹、请求与状态快照（未运行/失败的分支也记，
// 但跳过重启还要求 stillRunning，失败时下次 Start 仍会完整重试）。
func (b *embedBackend) record(fp string, req app.MCPRequest, st *app.MCPStatus, svc *Service) {
	snapshot := *st
	cloned := req // 浅拷贝即可：指纹与状态只读，不回写切片
	b.mu.Lock()
	b.fp, b.last, b.status, b.svc = fp, &cloned, &snapshot, svc
	b.mu.Unlock()
}

// statusOfLocked 返回状态快照（调用方持 b.mu；record 保证与 fp 同步存在）。
func (b *embedBackend) statusOfLocked() *app.MCPStatus {
	if b.status != nil {
		return b.status
	}
	return &app.MCPStatus{}
}

// Refresh 不重启服务地重扫项目目录并刷新状态计数。
//
// 为什么需要：打开/切换/关闭工作目录后，AI 侧 list_projects 应能看到新项目；
// 但目录授权白名单来自设置（MCP 可访问的工作目录），根变化本身不改变服务配置，
// 为这件事重启会把 AI 客户端的长连接会话掉断——只重扫即可。
func (b *embedBackend) Refresh() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.last == nil || !b.stillRunning() || b.svc == nil {
		return // 未在运行：无事可做（下次 Start 按新配置完整重建）
	}
	if err := b.svc.Reg().Reload(); err != nil {
		b.logger.Printf("MCP 重扫项目失败: %v", err)
		return
	}
	if b.status != nil {
		b.status.Projects = b.svc.ProjectCount()
	}
}

// Stop 停止服务（幂等；等待优雅关闭，超时不等）。
func (b *embedBackend) Stop() {
	b.mu.Lock()
	cancel, done := b.cancel, b.done
	b.cancel, b.done, b.addr = nil, nil, ""
	b.fp, b.last, b.svc, b.status = "", nil, nil, nil
	b.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	select {
	case <-done:
	case <-time.After(embedStopTimeout):
	}
}

// embedHint 按监听范围给连接提示。
func embedHint(st *app.MCPStatus) string {
	if !st.CrossHost {
		return "本机访问：AI 工具里填上面的地址与令牌即可；要让 WSL / 局域网访问，把监听地址改成 0.0.0.0"
	}
	return "跨主机访问：WSL2 用 mirrored 模式时连 127.0.0.1，NAT 模式用宿主 IP（在 WSL 里执行 ip route show default 取网关）；" +
		"另一台机器用本机的局域网 IP。首次可能需要放行 Windows 防火墙（专用网络）。"
}
