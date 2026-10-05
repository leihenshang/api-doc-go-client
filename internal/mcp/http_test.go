package mcp

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTokenOK(t *testing.T) {
	cases := []struct {
		header, want string
		ok           bool
	}{
		{"Bearer secret123", "secret123", true},
		{"bearer secret123", "secret123", true}, // 前缀大小写不敏感
		{"BEARER  secret123 ", "secret123", true},
		{"Bearer wrong", "secret123", false},
		{"secret123", "secret123", false}, // 缺 Bearer 前缀
		{"", "secret123", false},
		{"Bearer ", "secret123", false},
	}
	for _, c := range cases {
		if got := tokenOK(c.header, c.want); got != c.ok {
			t.Errorf("tokenOK(%q, %q)=%v want %v", c.header, c.want, got, c.ok)
		}
	}
}

func TestOriginAllowed(t *testing.T) {
	cases := []struct {
		name   string
		origin string
		allow  []string
		want   bool
	}{
		{"非浏览器客户端无 Origin", "", nil, true},
		{"未配置白名单时拒绝浏览器来源", "http://evil.test", nil, false},
		{"精确匹配", "http://localhost:5173", []string{"http://localhost:5173"}, true},
		{"大小写与结尾斜杠无关", "http://LocalHost:5173/", []string{"http://localhost:5173"}, true},
		{"通配", "http://any.test", []string{"*"}, true},
		{"不在列表", "http://evil.test", []string{"http://localhost:5173"}, false},
		{"空 Origin 在白名单下也放行", "", []string{"*"}, true},
	}
	for _, c := range cases {
		if got := originAllowed(c.origin, c.allow); got != c.want {
			t.Errorf("%s: originAllowed(%q, %v)=%v want %v", c.name, c.origin, c.allow, got, c.want)
		}
	}
}

// 兜底必须是回环：漏传地址时不能把集合暴露到局域网（跨主机要在配置里显式写 0.0.0.0）。
func TestNormalizeAddr(t *testing.T) {
	cases := map[string]string{
		"":                 "127.0.0.1:8189",
		":9000":            "127.0.0.1:9000",
		"127.0.0.1:8189":   "127.0.0.1:8189",
		"0.0.0.0:8189":     "0.0.0.0:8189",
		" 127.0.0.1:8189 ": "127.0.0.1:8189",
	}
	for in, want := range cases {
		if got := normalizeAddr(in); got != want {
			t.Errorf("normalizeAddr(%q)=%q want %q", in, got, want)
		}
	}
}

// TestGuardTokenAndOrigin 覆盖跨主机场景的两道闸：令牌 + 来源。
func TestGuardTokenAndOrigin(t *testing.T) {
	const token = "s3cr3t-token"
	var reached int
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached++
		w.WriteHeader(http.StatusOK)
	})
	guard := withHTTPGuard(next, HTTPOptions{Token: token}, discardLogger())

	req := func(method, origin, auth string) *http.Request {
		r := httptest.NewRequest(method, "/mcp", strings.NewReader("{}"))
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if auth != "" {
			r.Header.Set("Authorization", auth)
		}
		return r
	}

	// 无令牌 → 401，且请求不能到达 MCP 端点
	w := httptest.NewRecorder()
	guard.ServeHTTP(w, req(http.MethodPost, "", ""))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("无令牌: status=%d want 401", w.Code)
	}
	if reached != 0 {
		t.Errorf("无令牌时不应到达后端（reached=%d）", reached)
	}

	// 令牌正确 → 放行
	w = httptest.NewRecorder()
	guard.ServeHTTP(w, req(http.MethodPost, "", "Bearer "+token))
	if w.Code != http.StatusOK || reached != 1 {
		t.Errorf("正确令牌: status=%d reached=%d want 200/1", w.Code, reached)
	}

	// 令牌正确但 Origin 未允许 → 403（DNS rebinding 防护）
	guard = withHTTPGuard(next, HTTPOptions{Token: token, AllowOrigins: []string{"http://localhost:5173"}}, discardLogger())
	w = httptest.NewRecorder()
	guard.ServeHTTP(w, req(http.MethodPost, "http://evil.test", "Bearer "+token))
	if w.Code != http.StatusForbidden {
		t.Errorf("恶意 Origin: status=%d want 403", w.Code)
	}

	// Origin 在白名单内 → 放行并回 CORS 头
	w = httptest.NewRecorder()
	guard.ServeHTTP(w, req(http.MethodPost, "http://localhost:5173", "Bearer "+token))
	if w.Code != http.StatusOK {
		t.Errorf("白名单 Origin: status=%d want 200", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("CORS 头: %q want http://localhost:5173", got)
	}

	// 浏览器预检：不带令牌也应通过（否则浏览器的 CORS 预检就挂了，正文请求再带令牌）
	w = httptest.NewRecorder()
	guard.ServeHTTP(w, req(http.MethodOptions, "http://localhost:5173", ""))
	if w.Code != http.StatusNoContent {
		t.Errorf("预检: status=%d want 204", w.Code)
	}
	if !strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Errorf("预检未允许 Authorization 头: %q", w.Header().Get("Access-Control-Allow-Headers"))
	}
	if w.Header().Get("Access-Control-Expose-Headers") != "Mcp-Session-Id" {
		t.Errorf("预检未暴露 Mcp-Session-Id: %q", w.Header().Get("Access-Control-Expose-Headers"))
	}
}

// TestHealthzNoAuth 确认健康检查不要求令牌（远端先探通端口），且不泄露项目信息。
func TestHealthzNoAuth(t *testing.T) {
	w := httptest.NewRecorder()
	healthz(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	var body struct {
		OK      bool   `json:"ok"`
		Service string `json:"service"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是 JSON: %v", err)
	}
	if !body.OK || body.Service != "api-doc-go" || body.Version != serverVersion {
		t.Errorf("healthz 内容异常: %+v", body)
	}
}

// discardLogger 测试用静默 logger。
func discardLogger() *log.Logger { return log.New(io.Discard, "", 0) }
