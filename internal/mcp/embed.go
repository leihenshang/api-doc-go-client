package mcp

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	"api-doc-go-client/internal/app"
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
	// newApp 给每个项目一个独立实例（App 一次只能打开一个集合，多项目必须各自一个）。
	newApp NewAppFunc
}

// NewBackend 构造内嵌 MCP 服务实现。logger 为 nil 时丢弃日志。
func NewBackend(logger *log.Logger) app.MCPBackend {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &embedBackend{logger: logger, newApp: defaultNewApp}
}

// Start 按设置启停服务：Enabled=false 即停止（幂等）。
func (b *embedBackend) Start(req app.MCPRequest) (*app.MCPStatus, error) {
	// 先停旧实例：地址/端口/只读变更后旧监听必须下线
	b.Stop()

	st := &app.MCPStatus{
		Enabled:   req.Enabled,
		Addr:      req.Addr,
		Port:      req.Port,
		Token:     req.Token,
		ReadOnly:  req.ReadOnly,
		Origins:   req.AllowOrigins,
		Root:      req.Root,
		CrossHost: !IsLoopbackAddr(req.Addr),
	}
	switch {
	case !req.Enabled:
		st.Hint = "MCP 服务未启用"
		return st, nil
	case req.Root == "":
		st.Error = "尚未打开集合：先在客户端里打开一个集合目录"
		return st, nil
	}
	addr := net.JoinHostPort(req.Addr, strconv.Itoa(req.Port))
	// 端口预检（先占一下再放开）：把「端口被占用」变成同步错误返回给设置页，
	// 否则真正的监听错误发生在 ServeHTTP 的内部 goroutine 里，界面只能显示「未知错误」。
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		st.Error = fmt.Sprintf("无法监听 %s：%v", addr, err)
		st.Hint = "端口可能已被占用（常见于另一个 mcpserver 实例），换一个端口试试"
		return st, nil
	}
	_ = ln.Close()

	srv, svc, err := NewServer(Config{
		Root:     req.Root,
		ReadOnly: req.ReadOnly,
		Logger:   b.logger,
		NewApp:   b.newApp,
	})
	if err != nil {
		st.Error = fmt.Sprintf("装配 MCP 服务失败：%v", err)
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
	st.Projects = len(svc.Projects())
	shown := req.Addr
	if req.Addr == "0.0.0.0" || req.Addr == "::" {
		shown = "127.0.0.1" // 全网卡监听时用回环地址访问最省事
	}
	st.URL = "http://" + net.JoinHostPort(shown, strconv.Itoa(req.Port)) + "/mcp"
	st.Health = "http://" + net.JoinHostPort(shown, strconv.Itoa(req.Port)) + "/healthz"
	st.Hint = embedHint(st)
	return st, nil
}

// Stop 停止服务（幂等；等待优雅关闭，超时不等）。
func (b *embedBackend) Stop() {
	b.mu.Lock()
	cancel, done := b.cancel, b.done
	b.cancel, b.done, b.addr = nil, nil, ""
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
