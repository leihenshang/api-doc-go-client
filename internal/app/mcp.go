package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"api-doc-go-client/internal/config"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// errNoMCPBackend 未注入内嵌 MCP 实现（纯浏览器态 / 未开启该功能的构建）。
var errNoMCPBackend = errors.New("当前构建未启用内嵌 MCP 服务")

// newMCPToken 生成 32 位十六进制随机令牌（128 bit 熵）。
func newMCPToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano()) // 读不到随机源时兜底
	}
	return hex.EncodeToString(b)
}

// isLoopbackHost 监听地址是否只在回环上。
func isLoopbackHost(addr string) bool {
	switch strings.ToLower(strings.TrimSpace(addr)) {
	case "", "0.0.0.0", "::", "[::]":
		return false
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	if ip := net.ParseIP(strings.TrimSpace(addr)); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// parentDir 取父目录；取不到时返回空串（根目录）。
func parentDir(p string) string {
	if p == "" {
		return ""
	}
	return filepath.Dir(p)
}

// MCPStatus 内嵌 MCP 服务的运行状态（设置页「MCP 服务」分区展示）。
type MCPStatus struct {
	Enabled   bool     `json:"enabled"`   // 设置里是否启用
	Running   bool     `json:"running"`   // 此刻是否真的在监听
	Addr      string   `json:"addr"`      // 监听地址（设置值）
	Port      int      `json:"port"`      // 监听端口（设置值）
	URL       string   `json:"url"`       // 端点 URL（运行中才有）
	Health    string   `json:"health"`    // 健康检查地址（运行中才有）
	Token     string   `json:"token"`     // Bearer 令牌（前端据此复制连接配置）
	ReadOnly  bool     `json:"readOnly"`  // 只读模式：不注册创建/修改/删除类工具
	Origins   []string `json:"origins"`   // 允许的浏览器来源（Origin 白名单）
	Root      string   `json:"root"`      // 项目根目录（当前集合的父目录）
	Projects  int      `json:"projects"`  // 可见项目数
	CrossHost bool     `json:"crossHost"` // 监听非回环地址（跨主机 / WSL / 局域网）
	Error     string   `json:"error"`     // 启动失败原因（端口占用等）；正常为空
	Hint      string   `json:"hint"`      // 连接提示（WSL / 浏览器端 / 防火墙等）
}

// MCPRequest 一次「按设置启停」的入参（MCPRequest 由设置 + 当前集合推导）。
type MCPRequest struct {
	Enabled      bool
	Addr         string
	Port         int
	Token        string
	ReadOnly     bool
	AllowOrigins []string
	// Root 项目根目录：内嵌服务用「当前集合的父目录」，
	// 这样「项目 = 含 opencollection.yml 的目录」语义与独立 mcpserver 一致。
	Root string
}

// MCPBackend 内嵌 MCP 服务的实现契约。
//
// 为什么用注入而不是让 app 直接 import internal/mcp：internal/mcp 依赖 internal/app
// （项目的打开/发送走 App），反向 import 会成环。实现放在 internal/mcp，
// 由 main / devserver 在启动时注入（见 internal/mcp/embed.go）。
type MCPBackend interface {
	// Start 按入参启停服务：Enabled=false 即停止（幂等）。
	// 失败时返回错误，且不返回半启动状态。
	Start(req MCPRequest) (*MCPStatus, error)
	// Stop 停止服务（幂等）。
	Stop()
}

// SetMCPBackend 注入内嵌 MCP 服务实现（main / devserver 启动时调用）。
func (a *App) SetMCPBackend(b MCPBackend) {
	a.mu.Lock()
	a.mcpBackend = b
	a.mu.Unlock()
}

// ApplyMCPSettings 依据当前设置启停内嵌 MCP 服务（设置保存后调用；幂等）。
func (a *App) ApplyMCPSettings() *MCPStatus { return a.applyMCP("") }

// RegenerateMCPToken 换一个新的访问令牌（先落盘再重启，令牌立即生效）。
func (a *App) RegenerateMCPToken() (*MCPStatus, error) {
	a.mu.Lock()
	s := a.settings
	backend := a.mcpBackend
	a.mu.Unlock()

	if backend == nil {
		return nil, errNoMCPBackend
	}
	s.MCP.Token = newMCPToken()
	if err := config.Save(s.Normalize()); err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.settings = s
	a.mu.Unlock()
	return a.applyMCP(s.MCP.Token), nil
}

// MCPStatusText 返回当前状态快照（设置页打开时拉一次）。
//
// 方法名带 Text 是为了避开与类型名 MCPStatus 同名（Wails 侧同名会与生成的类型撞名）。
func (a *App) MCPStatusText() *MCPStatus {
	a.mu.Lock()
	cur := a.mcpStatus
	cfg := a.settings.MCP
	backend := a.mcpBackend
	a.mu.Unlock()
	if cur != nil {
		return cur
	}
	st := &MCPStatus{
		Enabled: cfg.Enabled, Addr: cfg.Addr, Port: cfg.Port, Token: cfg.Token,
		ReadOnly: cfg.ReadOnly, Origins: cfg.AllowOrigins,
		CrossHost: !isLoopbackHost(cfg.Addr),
	}
	switch {
	case backend == nil:
		st.Error = errNoMCPBackend.Error()
	case !cfg.Enabled:
		st.Hint = "MCP 服务未启用"
	}
	return st
}

// applyMCP 重启内嵌 MCP 服务。newToken 非空时先换令牌（设置页「重新生成」用）。
//
// 「启用但令牌为空」时自动生成并**落盘**：只保存在内存里的话，下一次
// SaveSettings（例如切主题）或重启应用都会再生成一个新令牌，已连上的 AI 客户端
// 会突然 401，且用户看不出发生了什么。
func (a *App) applyMCP(newToken string) *MCPStatus {
	a.mu.Lock()
	cfg := a.settings.MCP
	if newToken != "" {
		cfg.Token = newToken
	}
	generated := false
	if cfg.Enabled && strings.TrimSpace(cfg.Token) == "" {
		cfg.Token = newMCPToken()
		generated = true
	}
	a.settings.MCP = cfg
	persist := a.settings
	backend := a.mcpBackend
	root := ""
	if a.coll != nil {
		root = parentDir(a.coll.Dir)
	}
	a.mu.Unlock()

	// 落盘放锁外（要做文件 IO）；失败不阻断服务启动，但要让用户看到
	persistErr := ""
	if generated {
		if err := config.Save(persist.Normalize()); err != nil {
			persistErr = fmt.Sprintf("令牌已生成但写入配置失败（重启后会变）: %v", err)
		}
	}

	// 未注入实现（纯浏览器/devserver 未开启 MCP 的构建）：如实回报，不静默
	if backend == nil {
		st := &MCPStatus{Enabled: cfg.Enabled, Addr: cfg.Addr, Port: cfg.Port, Token: cfg.Token,
			ReadOnly: cfg.ReadOnly, Origins: cfg.AllowOrigins, Error: errNoMCPBackend.Error()}
		a.setMCPStatus(st)
		return st
	}

	st, err := backend.Start(MCPRequest{
		Enabled:      cfg.Enabled,
		Addr:         cfg.Addr,
		Port:         cfg.Port,
		Token:        cfg.Token,
		ReadOnly:     cfg.ReadOnly,
		AllowOrigins: cfg.AllowOrigins,
		Root:         root,
	})
	if err != nil {
		st = &MCPStatus{
			Enabled: cfg.Enabled, Addr: cfg.Addr, Port: cfg.Port, Token: cfg.Token,
			ReadOnly: cfg.ReadOnly, Origins: cfg.AllowOrigins, Root: root,
			CrossHost: !isLoopbackHost(cfg.Addr),
			Error:     err.Error(),
		}
	}
	if persistErr != "" {
		st.Error = strings.TrimSpace(strings.Join([]string{st.Error, persistErr}, "；"))
	}
	a.setMCPStatus(st)
	return st
}

func (a *App) setMCPStatus(st *MCPStatus) {
	a.mu.Lock()
	a.mcpStatus = st
	a.mu.Unlock()
}

// Shutdown 退出前收尾（Wails OnShutdown）：停内嵌 MCP 服务与集合监听。
//
// 为什么重要：wails dev 的进程树清理在 Windows 上不可靠（实测 taskkill /T 也清不掉它拉起的
// app 进程），app 若残留会一直占着 MCP 端口，别人连不上也以为服务还开着。
func (a *App) Shutdown(_ context.Context) {
	// 先关掉 signalQuit：告诉信号处理器「已经有人在收尾」，别再自己退一次
	a.mu.Lock()
	if a.signalQuit != nil {
		select {
		case <-a.signalQuit: // 已关闭
		default:
			close(a.signalQuit)
		}
	}
	backend, coll := a.mcpBackend, a.coll
	a.mu.Unlock()
	if backend != nil {
		backend.Stop() // 释放 MCP 监听端口
	}
	if coll != nil {
		coll.StopWatch()
	}
}

// watchSignals 处理 SIGINT / SIGTERM。
//
// 为什么不是「收到就退」：`wails dev` 的 Ctrl+C 会**同时**打到 CLI 和它的子进程（同一控制台进程组）。
// app 若立刻退出，CLI 只会收到一个非零退出码（它只在退出码为 0 时才把主循环置退出），
// 于是第一次 Ctrl+C 常常「应用没了、CLI 还在」。
// 这里改成：收到信号后等 sigGrace，若这段时间没人来杀我们（说明不是被 wails dev 收尾），就自己退出，
// 两种场景都干净：wails dev 场景由 CLI 主导（毫秒级就会被 TASKKILL），独立跑 exe 场景延迟 sigGrace 后自退。
func (a *App) watchSignals() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		a.mu.Lock()
		quit := a.signalQuit
		a.mu.Unlock()
		select {
		case <-quit: // 正常路径：OnShutdown 会关掉这个 channel
		case <-time.After(sigGrace):
			a.mu.Lock()
			ctx := a.ctx
			a.mu.Unlock()
			if ctx != nil {
				runtime.Quit(ctx) // 关窗 → wails.Run 返回 → 进程退出
			} else {
				os.Exit(0)
			}
		}
	}()
}

const sigGrace = 1500 * time.Millisecond
