package app

import (
	"testing"

	"api-doc-go-client/internal/config"
)

// isolateConfigDir 把配置目录指到临时目录（Windows 看 %AppData%，类 Unix 看 XDG_CONFIG_HOME）。
func isolateConfigDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("AppData", dir)
}

// fakeMCPBackend 只记录 Start 收到的配置，不真的监听端口。
type fakeMCPBackend struct {
	last   MCPRequest
	starts int
}

func (f *fakeMCPBackend) Start(req MCPRequest) (*MCPStatus, error) {
	f.last = req
	f.starts++
	return &MCPStatus{
		Enabled: req.Enabled, Running: req.Enabled,
		Addr: req.Addr, Port: req.Port, Token: req.Token,
		ReadOnly: req.ReadOnly, Origins: req.AllowOrigins, Root: req.Root,
	}, nil
}

func (f *fakeMCPBackend) Stop() {}

// 启用内嵌 MCP 且令牌为空时自动生成的令牌必须**落盘**：
// 生成发生在写回 a.settings 之后的话，切主题（setTheme → SaveSettings 写空令牌）
// 或应用重启都会换一份新令牌，已连上的 AI 客户端直接 401（而且用户看不出原因）。
func TestApplyMCPPersistsGeneratedToken(t *testing.T) {
	isolateConfigDir(t)
	a := NewApp()
	backend := &fakeMCPBackend{}
	a.SetMCPBackend(backend)

	a.mu.Lock()
	a.settings.MCP.Enabled = true
	a.settings.MCP.Addr = "127.0.0.1"
	a.settings.MCP.Port = 8199
	a.settings.MCP.Token = ""
	a.mu.Unlock()

	st := a.applyMCP("")
	if backend.last.Token == "" {
		t.Fatal("启用且令牌为空时应自动生成令牌")
	}
	if len(backend.last.Token) != 32 {
		t.Fatalf("令牌应为 32 位十六进制，实际 %q", backend.last.Token)
	}
	if st.Token != backend.last.Token {
		t.Fatalf("状态里的令牌与下发的不一致: %q vs %q", st.Token, backend.last.Token)
	}

	onDisk, err := config.Load()
	if err != nil {
		t.Fatalf("读取落盘设置: %v", err)
	}
	if onDisk.MCP.Token != backend.last.Token {
		t.Fatalf("令牌未落盘：磁盘 %q，实际下发 %q", onDisk.MCP.Token, backend.last.Token)
	}
	if !onDisk.MCP.Enabled {
		t.Fatal("MCP 启用状态应一并落盘")
	}

	// 再启一次（模拟「切主题触发 SaveSettings → applyMCP」）：令牌必须保持不变
	before := backend.last.Token
	_ = a.applyMCP("")
	if backend.last.Token != before {
		t.Fatalf("重复应用设置不应轮换令牌：%q → %q", before, backend.last.Token)
	}
}

// 显式指定的令牌（设置页填写 / RegenerateMCPToken 落盘后）应原样下发，且不被再次轮换。
// 落盘由 SaveSettings / RegenerateMCPToken 负责（applyMCP 只在令牌为空时补生成并落盘）。
func TestApplyMCPWithExplicitToken(t *testing.T) {
	isolateConfigDir(t)
	a := NewApp()
	backend := &fakeMCPBackend{}
	a.SetMCPBackend(backend)

	a.mu.Lock()
	a.settings.MCP.Enabled = true
	a.settings.MCP.Token = "fixed-token"
	persist := a.settings
	a.mu.Unlock()
	if err := config.Save(persist); err != nil {
		t.Fatalf("落盘设置: %v", err)
	}

	st := a.applyMCP("")
	if backend.last.Token != "fixed-token" || st.Token != "fixed-token" {
		t.Fatalf("显式令牌应原样下发: %+v", backend.last)
	}
	onDisk, err := config.Load()
	if err != nil {
		t.Fatalf("读取落盘设置: %v", err)
	}
	if onDisk.MCP.Token != "fixed-token" {
		t.Fatalf("显式令牌被改写: %q", onDisk.MCP.Token)
	}
}
