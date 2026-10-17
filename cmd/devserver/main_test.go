package main

// P2-23：cmd/devserver 是 e2e 的底座（POST /api/App/<Method> 反射桥 + SSE），
// 它自己零测试时，e2e 失败会先怀疑用例而不怀疑桥。这里覆盖桥的门禁与反射边界。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"api-doc-go-client/internal/app"
)

func TestLoopbackHost(t *testing.T) {
	allow := []string{"", "localhost", "localhost:5173", "127.0.0.1", "127.0.0.1:8175", "[::1]:8175", "::1"}
	for _, h := range allow {
		if !loopbackHost(h) {
			t.Fatalf("loopbackHost(%q) 应放行", h)
		}
	}
	deny := []string{"evil.com", "evil.com:8175", "10.0.0.1", "192.168.1.10:8175", "localhost.evil.com"}
	for _, h := range deny {
		if loopbackHost(h) {
			t.Fatalf("loopbackHost(%q) 应拒绝（DNS rebinding 面）", h)
		}
	}
}

func TestLoopbackOrigin(t *testing.T) {
	allow := []string{"http://localhost:5173", "http://127.0.0.1:8175", "https://[::1]:5173"}
	for _, o := range allow {
		if !loopbackOrigin(o) {
			t.Fatalf("loopbackOrigin(%q) 应放行", o)
		}
	}
	deny := []string{"http://evil.com", "https://127.0.0.1.evil.com", "file:///etc/passwd", "", "javascript:alert(1)"}
	for _, o := range deny {
		if loopbackOrigin(o) {
			t.Fatalf("loopbackOrigin(%q) 应拒绝", o)
		}
	}
}

func TestLoopbackListenAddr(t *testing.T) {
	for _, a := range []string{"127.0.0.1:8175", "localhost:0", "[::1]:8175"} {
		if !loopbackListenAddr(a) {
			t.Fatalf("loopbackListenAddr(%q) 应视为回环", a)
		}
	}
	for _, a := range []string{":8175", "0.0.0.0:8175", "10.0.0.1:8175"} {
		if loopbackListenAddr(a) {
			t.Fatalf("loopbackListenAddr(%q) 不应视为回环（会绑全网卡）", a)
		}
	}
}

// TestGuardAPI test 三重门禁：Host / Origin / method+content-type / token。
func TestGuardAPI(t *testing.T) {
	called := false
	h := guardAPI(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}, "s3cret")

	do := func(method, path, host, origin, token, ct string) *httptest.ResponseRecorder {
		called = false
		req := httptest.NewRequest(method, path, strings.NewReader(`[]`))
		req.Host = host
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if token != "" {
			req.Header.Set("X-Dev-Token", token)
		}
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		w := httptest.NewRecorder()
		h(w, req)
		return w
	}

	cases := []struct {
		name   string
		want   int
		pass   bool
		method string
		host   string
		origin string
		token  string
		ct     string
	}{
		{"正常请求", http.StatusOK, true, http.MethodPost, "127.0.0.1:8175", "http://127.0.0.1:5173", "s3cret", "application/json"},
		{"DNS rebinding Host", http.StatusForbidden, false, http.MethodPost, "evil.com", "", "s3cret", "application/json"},
		{"跨站 Origin", http.StatusForbidden, false, http.MethodPost, "127.0.0.1:8175", "http://evil.com", "s3cret", "application/json"},
		{"缺令牌", http.StatusUnauthorized, false, http.MethodPost, "127.0.0.1:8175", "", "", "application/json"},
		{"错令牌", http.StatusUnauthorized, false, http.MethodPost, "127.0.0.1:8175", "", "wrong", "application/json"},
		{"GET 反射桥", http.StatusMethodNotAllowed, false, http.MethodGet, "127.0.0.1:8175", "", "s3cret", "application/json"},
		{"text/plain 简单请求", http.StatusUnsupportedMediaType, false, http.MethodPost, "127.0.0.1:8175", "", "s3cret", "text/plain"},
	}
	for _, c := range cases {
		w := do(c.method, "/api/App/SaveDoc", c.host, c.origin, c.token, c.ct)
		if w.Code != c.want {
			t.Fatalf("%s: 状态码应为 %d，got %d", c.name, c.want, w.Code)
		}
		if c.pass != called {
			t.Fatalf("%s: 是否放行不符（want pass=%v, got %v）", c.name, c.pass, called)
		}
	}

	// SSE 例外：GET /api/events 不走 POST+JSON 那两条判定
	if w := do(http.MethodGet, "/api/events", "127.0.0.1:8175", "http://127.0.0.1:5173", "s3cret", ""); w.Code != http.StatusOK {
		t.Fatalf("SSE 端点应放行，got %d", w.Code)
	}
}

// TestInvokeRejectsNonExportedAndUnknown 反射桥只允许「大写开头的已存在方法」。
func TestInvokeRejectsNonExportedAndUnknown(t *testing.T) {
	core := &app.App{}
	for _, name := range []string{"", "saveDoc", "nope", "NoSuchMethod"} {
		if _, err := invoke(core, name, httptest.NewRequest(http.MethodPost, "/api/App/x", strings.NewReader("[]"))); err == nil {
			t.Fatalf("invoke(%q) 应报错", name)
		}
	}
	if !isExported("OpenCollection") || isExported("openCollection") || isExported("") {
		t.Fatal("isExported 只应接受大写开头且非空")
	}
}

func TestItoa(t *testing.T) {
	cases := map[int]string{0: "0", 1: "1", -1: "-1", 42: "42", -987: "-987"}
	for in, want := range cases {
		if got := itoa(in); got != want {
			t.Fatalf("itoa(%d) = %q，期望 %q", in, got, want)
		}
	}
}
