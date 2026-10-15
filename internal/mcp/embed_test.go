package mcp

import (
	"io"
	"log"
	"strings"
	"testing"

	"api-doc-go-client/internal/app"
	"api-doc-go-client/internal/config"
)

// 内嵌 MCP 后端（embed.go）的最小用例：只覆盖「不起监听」的纯逻辑分支与状态机早退路径，
// 保持无网络、无端口占用的 hermetic 特性（真正起服务的端到端在 http_test.go 已覆盖）。

func newEmbedBackend() app.MCPBackend {
	return NewBackend(log.New(io.Discard, "", 0))
}

// toAllowDirs 把 config 白名单映射为 mcp 白名单：丢弃空路径、保留可写位。
func TestToAllowDirs(t *testing.T) {
	in := []config.MCPAllowDir{
		{Path: "/projects/a", Writable: true},
		{Path: "  "}, // 空/纯空白 → 被丢弃
		{Path: "/projects/b", Writable: false},
	}
	got := toAllowDirs(in)
	if len(got) != 2 {
		t.Fatalf("期望 2 条（空路径应丢弃），得到 %d：%v", len(got), got)
	}
	var writable int
	for _, d := range got {
		if strings.TrimSpace(d.Path) == "" {
			t.Fatalf("结果里不应有空路径：%v", d)
		}
		if d.Writable {
			writable++
		}
	}
	if writable != 1 {
		t.Errorf("可写位应仅 a 为 true，得到 %d", writable)
	}
}

// embedHint 按监听范围给不同连接提示。
func TestEmbedHint(t *testing.T) {
	local := embedHint(&app.MCPStatus{CrossHost: false})
	if local == "" || strings.Contains(local, "跨主机") {
		t.Errorf("回环提示不对：%q", local)
	}
	cross := embedHint(&app.MCPStatus{CrossHost: true})
	if !strings.Contains(cross, "跨主机") {
		t.Errorf("跨主机提示应含相应文案：%q", cross)
	}
}

// 未启用：不起监听、Running=false、给「未启用」提示。
func TestEmbedStartDisabled(t *testing.T) {
	b := newEmbedBackend()
	st, err := b.Start(app.MCPRequest{Enabled: false, Addr: "127.0.0.1", Port: 8199})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if st.Running {
		t.Fatal("未启用不应处于监听")
	}
	if st.Hint != "MCP 服务未启用" {
		t.Errorf("提示不对：%q", st.Hint)
	}
}

// 启用但白名单为空：安全默认——不监听、Running=false、提示去授权；Stop 幂等。
func TestEmbedStartEmptyAllow(t *testing.T) {
	b := newEmbedBackend()
	st, err := b.Start(app.MCPRequest{Enabled: true, Addr: "127.0.0.1", Port: 8199})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if st.Running {
		t.Fatal("空白名单不应真正监听")
	}
	if !strings.Contains(st.Hint, "授权") {
		t.Errorf("应提示尚未授权工作目录：%q", st.Hint)
	}
	// 幂等：未起服务时重复 Stop 不应 panic
	b.Stop()
	b.Stop()
}
