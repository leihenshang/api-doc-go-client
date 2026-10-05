package runner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"api-doc-go-client/internal/collection"
)

// 头注入：头名/头值里的 CR/LF/空字符必须在客户端就被拒绝，
// 否则「拼接请求」的语义会变成「注入额外请求头 / 拆分请求」。
func TestSendRejectsHeaderInjection(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	cases := []collection.KV{
		{Name: "X-Ok", Value: "ok\r\nX-Injected: 1", Enabled: true},
		{Name: "X-Bad\r\nX-Another: 1", Value: "v", Enabled: true},
		{Name: "X-Nul", Value: "a\x00b", Enabled: true},
		{Name: "Bad Header", Value: "v", Enabled: true},
	}
	for _, h := range cases {
		r := collection.Request{UID: "u", Method: "GET", URL: srv.URL, Headers: []collection.KV{h}}
		if _, err := Send(context.Background(), r, nil, DefaultOptions()); err == nil {
			t.Errorf("非法请求头 %q=%q 应被拒绝", h.Name, h.Value)
		}
	}
	// API Key 认证的头名同样要校验
	r := collection.Request{
		UID: "u", Method: "GET", URL: srv.URL,
		Auth: &collection.Auth{Type: "apikey", Key: "X-Evil\r\nX-Injected: 1", Value: "v"},
	}
	if _, err := Send(context.Background(), r, nil, DefaultOptions()); err == nil {
		t.Error("API Key 头名含 CRLF 应被拒绝")
	}
}

// 响应体超过 maxBodySize 时必须标记截断并给出真实大小，而不是静默截断。
func TestSendMarksTruncatedBody(t *testing.T) {
	big := strings.Repeat("a", maxBodySize+1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", itoa(len(big)))
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()

	res, err := Send(context.Background(), collection.Request{UID: "u", Method: "GET", URL: srv.URL}, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !res.Truncated {
		t.Fatal("超限响应未被标记为截断")
	}
	if res.Size != maxBodySize {
		t.Fatalf("截断后 size 应为 %d，实际 %d", maxBodySize, res.Size)
	}
	if res.FullSize != len(big) {
		t.Fatalf("fullSize 应为 %d，实际 %d", len(big), res.FullSize)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("截断时应有告警")
	}
}

// 小响应不应被标记截断（避免误报）。
func TestSendSmallBodyNotTruncated(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()
	res, err := Send(context.Background(), collection.Request{UID: "u", Method: "GET", URL: srv.URL}, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Truncated || res.FullSize != 0 {
		t.Fatalf("普通响应不应标记截断: %+v", res)
	}
}

// 跨域重定向必须丢弃凭据类自定义头（Authorization 由标准库处理，X-Api-Key 只能我们自己丢）。
func TestCrossHostRedirectDropsCredentialHeaders(t *testing.T) {
	var leaked []string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.Header.Get("X-Api-Key"); v != "" {
			leaked = append(leaked, "X-Api-Key="+v)
		}
		if v := r.Header.Get("Authorization"); v != "" {
			leaked = append(leaked, "Authorization="+v)
		}
		if v := r.Header.Get("X-Trace"); v != "" {
			leaked = append(leaked, "X-Trace="+v)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer other.Close()

	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/landing", http.StatusFound)
	}))
	defer first.Close()

	r := collection.Request{UID: "u", Method: "GET", URL: first.URL, Headers: []collection.KV{
		{Name: "X-Api-Key", Value: "secret-key", Enabled: true},
		{Name: "Authorization", Value: "Bearer secret-token", Enabled: true},
		{Name: "X-Trace", Value: "keep-me", Enabled: true},
	}}
	res, err := Send(context.Background(), r, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	for _, got := range leaked {
		if !strings.HasPrefix(got, "X-Trace=") {
			t.Errorf("跨域重定向泄露了凭据头: %s", got)
		}
	}
	if len(res.Warnings) == 0 {
		t.Error("丢弃凭据头时应回传告警（用户需要知道凭据没跟过去）")
	}
}

// 同源重定向不应丢头（否则会误伤正常的登录跳转）。
func TestSameHostRedirectKeepsCredentialHeaders(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/end", http.StatusFound)
			return
		}
		seen = r.Header.Get("X-Api-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := collection.Request{UID: "u", Method: "GET", URL: srv.URL + "/start", Headers: []collection.KV{
		{Name: "X-Api-Key", Value: "secret-key", Enabled: true},
	}}
	if _, err := Send(context.Background(), r, nil, DefaultOptions()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if seen != "secret-key" {
		t.Fatalf("同源重定向不应丢弃自定义头，实际 %q", seen)
	}
}

// multipart 请求即使手写了显式 Content-Type，也必须用带 boundary 的派生值覆盖：
// 否则服务端拿不到 boundary，整个表单解析失败。
func TestMultipartOverridesExplicitContentType(t *testing.T) {
	var gotCT string
	var formValue string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		_ = r.ParseMultipartForm(1 << 20)
		formValue = r.FormValue("field")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := collection.Request{
		UID: "u", Method: "POST", URL: srv.URL,
		Headers: []collection.KV{{Name: "Content-Type", Value: "application/json", Enabled: true}},
		Body:    collection.Body{Type: "multipart", Form: []collection.KV{{Name: "field", Value: "v1", Enabled: true}}},
	}
	if _, err := Send(context.Background(), r, nil, DefaultOptions()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !strings.HasPrefix(gotCT, "multipart/form-data; boundary=") {
		t.Fatalf("Content-Type 应被覆盖为带 boundary 的派生值，实际 %q", gotCT)
	}
	if formValue != "v1" {
		t.Fatalf("服务端未解析出表单字段，实际 %q", formValue)
	}
}

// itoa 避免为一个断言引入 strconv 的额外依赖面（测试内部用）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
