package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

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

// fpToken 生成确定性的 32 位十六进制令牌（指纹用例只需稳定可区分）。
func fpToken(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// freePort 取一个空闲端口（先占后放）：监听用例不用 Port:0，
// 因为状态里的 URL 按设置端口拼接，绑 :0 时真实端口不在快照里、无法探活。
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("取空闲端口: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// 指纹只认影响服务的字段：完全相同的请求指纹一致；
// 任一可变字段（端口/令牌/白名单/交互开关/来源）变化必须不一致。
func TestRequestFingerprint(t *testing.T) {
	interactive := true
	base := app.MCPRequest{
		Enabled: true, Addr: "127.0.0.1", Port: 8199, Token: fpToken("t"),
		Allow:        []config.MCPAllowDir{{Path: "/projects/a", Writable: true}},
		AllowOrigins: []string{"http://localhost:5173"},
	}
	if got := requestFingerprint(base); got != requestFingerprint(base) {
		t.Fatalf("相同请求指纹应一致：%q vs %q", got, requestFingerprint(base))
	}
	cases := []struct {
		name string
		mod  func(*app.MCPRequest)
	}{
		{"端口", func(r *app.MCPRequest) { r.Port = 8200 }},
		{"令牌", func(r *app.MCPRequest) { r.Token = fpToken("other") }},
		{"白名单可写位", func(r *app.MCPRequest) { r.Allow[0].Writable = false }},
		{"交互授权开关", func(r *app.MCPRequest) { r.InteractiveAuth = &interactive }},
		{"来源白名单", func(r *app.MCPRequest) { r.AllowOrigins = nil }},
		{"启用开关", func(r *app.MCPRequest) { r.Enabled = false }},
	}
	for _, c := range cases {
		mod := c.mod
		diff := base
		// Allow 是切片：不拷贝底层数组的话，mod 会连带改掉 base（两个指纹仍相等且污染后续用例）
		diff.Allow = append([]config.MCPAllowDir(nil), base.Allow...)
		mod(&diff)
		if requestFingerprint(diff) == requestFingerprint(base) {
			t.Errorf("%s 变化应改变指纹", c.name)
		}
	}
}

// 空地址与 127.0.0.1 同义（normalizeAddr 的兜底语义）：不该被当成配置变更。
func TestRequestFingerprintAddrEquivalence(t *testing.T) {
	a := app.MCPRequest{Enabled: true, Port: 8199, Token: fpToken("t")}
	b := a
	b.Addr = "127.0.0.1"
	if requestFingerprint(a) != requestFingerprint(b) {
		t.Error("空地址与回环地址应同指纹")
	}
}

// 配置未变时重复 Start 不得重启监听：URL 不变且能实际探活。
// （历史行为是先 Stop 再重建，设置页「没改内容点保存」会把 AI 客户端的长连接会话掉断。）
func TestEmbedStartSameFingerprintNoRestart(t *testing.T) {
	b := newEmbedBackend()
	req := app.MCPRequest{
		Enabled: true, Addr: "127.0.0.1", Port: freePort(t),
		Token: fpToken("run"),
		Allow: []config.MCPAllowDir{{Path: t.TempDir(), Writable: true}},
	}
	st1, err := b.Start(req)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !st1.Running || st1.URL == "" {
		t.Fatalf("应处于监听：%+v", st1)
	}
	st2, err := b.Start(req)
	if err != nil {
		t.Fatalf("重复 Start: %v", err)
	}
	if st2.URL != st1.URL || !st2.Running {
		t.Fatalf("相同指纹不应换监听：%q vs %q", st1.URL, st2.URL)
	}
	// 端口预检与监听之间存在竞态（绑 :0 后放开再重绑），这里探活确认真实服务仍在线
	httpc := &http.Client{Timeout: 2 * time.Second}
	resp, err := httpc.Get(st2.Health)
	if err != nil {
		t.Fatalf("探活 %s: %v", st2.Health, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("探活状态码 %d", resp.StatusCode)
	}
	// 变令牌 + 变端口 → 完整重启（监听换了端口，URL 必换）；
	// 只变令牌的场景由指纹用例覆盖（同一端口重启后 URL 相同，不能拿它当证据）
	hanged := req
	hanged.Token = fpToken("other")
	hanged.Port = freePort(t)
	st3, err := b.Start(hanged)
	if err != nil {
		t.Fatalf("变配置 Start: %v", err)
	}
	if st3.URL == st1.URL {
		t.Fatalf("令牌变化应重启并换监听：%q", st3.URL)
	}
	b.Stop()
	// Stop 后同一请求必须重新起（指纹已随 Stop 清空，不能因快照残留而空转）
	st4, err := b.Start(req)
	if err != nil {
		t.Fatalf("Stop 后 Start: %v", err)
	}
	if !st4.Running {
		t.Fatalf("Stop 后应能重新监听：%+v", st4)
	}
	b.Stop()
}

// 未启用/空白名单的早退分支也要记指纹：重复相同请求直接返回快照（幂等，不反复 Stop）。
func TestEmbedStartUnrunningIdempotent(t *testing.T) {
	b := newEmbedBackend()
	req := app.MCPRequest{Enabled: false, Addr: "127.0.0.1", Port: 8199}
	st1, err := b.Start(req)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	st2, err := b.Start(req)
	if err != nil {
		t.Fatalf("重复 Start: %v", err)
	}
	if st2.Running || st2.Hint != st1.Hint {
		t.Fatalf("未启用的重复请求应返回同一快照：%+v", st2)
	}
	// Refresh 在未运行时空转，不得 panic
	b.Refresh()
}
