package runner

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"api-doc-go-client/internal/collection"
)

// echo 服务：把收到的 method/query/headers/body 原样返回，用于断言"替换与组装"是否正确。
func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"method":  r.Method,
			"query":   r.URL.Query(),
			"header":  map[string]string{"Authorization": r.Header.Get("Authorization"), "Content-Type": r.Header.Get("Content-Type")},
			"bodyRaw": string(body),
		})
	}))
}

// redirector 提供 /r1 → /r2 → /ok 的重定向链。
func redirector(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"final":true}`))
	})
	mux.HandleFunc("/r2", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ok", http.StatusFound)
	})
	mux.HandleFunc("/r1", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/r2", http.StatusFound)
	})
	return httptest.NewServer(mux)
}

func TestSendGetWithSubstitution(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()
	r := collection.Request{
		UID: "u1", Method: "get",
		URL:    "{{host}}/users",
		Params: []collection.KV{{Name: "name", Value: "{{user}}", Enabled: true}, {Name: "off", Value: "x", Enabled: false}},
		Headers: []collection.KV{
			{Name: "Authorization", Value: "Bearer {{token}}", Enabled: true},
			{Name: "X-Off", Value: "y", Enabled: false},
		},
	}
	vars := map[string]string{"host": srv.URL, "user": "alice", "token": "t-123"}
	res, err := Send(context.Background(), r, vars, DefaultOptions())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Status != 200 || res.TimeMS < 0 {
		t.Fatalf("结果异常: %+v", res)
	}
	var echo map[string]any
	if err := json.Unmarshal([]byte(res.Body), &echo); err != nil {
		t.Fatalf("响应非 JSON: %q", res.Body)
	}
	if echo["method"] != "GET" {
		t.Fatalf("method: %v", echo["method"])
	}
	q := echo["query"].(map[string]any)
	if fmt.Sprint(q["name"]) != "[alice]" {
		t.Fatalf("query.name: %v", q["name"])
	}
	if _, ok := q["off"]; ok {
		t.Fatalf("禁用参数不应发送")
	}
	h := echo["header"].(map[string]any)
	if h["Authorization"] != "Bearer t-123" {
		t.Fatalf("Authorization: %v", h["Authorization"])
	}
}

func TestSendJSONBodyAndHeaderCase(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()
	r := collection.Request{
		UID: "u2", Method: "POST", URL: srv.URL + "/create",
		Headers: []collection.KV{{Name: "content-type", Value: "application/json", Enabled: true}},
		Body:    collection.Body{Type: "json", Raw: `{"name":"{{user}}","ts":{{$timestamp}}}`},
	}
	res, err := Send(context.Background(), r, map[string]string{"user": "bob", "$timestamp": "1727400000"}, DefaultOptions())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	var echo map[string]any
	_ = json.Unmarshal([]byte(res.Body), &echo)
	h := echo["header"].(map[string]any)
	if h["Content-Type"] != "application/json" {
		t.Fatalf("Content-Type: %v", h["Content-Type"])
	}
	if !strings.Contains(res.Body, "bob") {
		t.Fatalf("body 未替换: %q", res.Body)
	}
	if !regexp.MustCompile(`ts\\":1\d{9}`).MatchString(res.Body) {
		t.Fatalf("$timestamp 未替换为 10 位数字: %q", res.Body)
	}
}

func TestSendForm(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()
	r := collection.Request{
		UID: "u3", Method: "POST", URL: srv.URL + "/login",
		Body: collection.Body{Type: "form", Form: []collection.KV{
			{Name: "user", Value: "carol", Enabled: true}, {Name: "pwd", Value: "p", Enabled: true},
		}},
	}
	res, err := Send(context.Background(), r, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	var echo map[string]any
	_ = json.Unmarshal([]byte(res.Body), &echo)
	h := echo["header"].(map[string]any)
	if h["Content-Type"] != "application/x-www-form-urlencoded" {
		t.Fatalf("form Content-Type: %v", h["Content-Type"])
	}
	if !strings.Contains(res.Body, "user=carol") {
		t.Fatalf("form 未编码: %q", res.Body)
	}
}

func TestMissingVarKeptAndReported(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()
	r := collection.Request{UID: "u4", Method: "GET", URL: srv.URL + "/x?missing={{notdefined}}"}
	res, err := Send(context.Background(), r, map[string]string{}, DefaultOptions())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !strings.Contains(res.Body, "notdefined") {
		t.Fatalf("未定义变量应原样保留: %q", res.Body)
	}
}

// 自签证书：默认校验应失败，开启 InsecureSSL 后成功。
func TestInsecureSSLPolicy(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	r := collection.Request{UID: "u5", Method: "GET", URL: srv.URL + "/x"}

	if _, err := Send(context.Background(), r, nil, DefaultOptions()); err == nil {
		t.Fatalf("未开启忽略证书时应因证书校验失败")
	}
	opt := DefaultOptions()
	opt.InsecureSSL = true
	res, err := Send(context.Background(), r, nil, opt)
	if err != nil {
		t.Fatalf("开启忽略证书后仍失败: %v", err)
	}
	if res.Status != 200 {
		t.Fatalf("status: %d", res.Status)
	}
}

func TestFollowRedirects(t *testing.T) {
	srv := redirector(t)
	defer srv.Close()
	r := collection.Request{UID: "u6", Method: "GET", URL: srv.URL + "/r1"}

	opt := DefaultOptions()
	opt.InsecureSSL = true
	res, err := Send(context.Background(), r, nil, opt)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Status != 200 || !strings.Contains(res.Body, "final") {
		t.Fatalf("应跟随重定向到最终响应: %+v", res)
	}

	stop := DefaultOptions()
	stop.FollowRedirects = false
	res, err = Send(context.Background(), r, nil, stop)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Status != http.StatusFound {
		t.Fatalf("关闭跟随后应返回 302，实际 %d", res.Status)
	}
}

func TestMaxRedirects(t *testing.T) {
	srv := redirector(t)
	defer srv.Close()
	opt := DefaultOptions()
	opt.MaxRedirects = 1
	_, err := Send(context.Background(), collection.Request{UID: "u7", Method: "GET", URL: srv.URL + "/r1"}, nil, opt)
	if err == nil {
		t.Fatalf("超过最大重定向次数应报错")
	}
}

func TestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte("late"))
	}))
	defer srv.Close()
	opt := DefaultOptions()
	opt.Timeout = 50 * time.Millisecond
	if _, err := Send(context.Background(), collection.Request{UID: "u8", Method: "GET", URL: srv.URL}, nil, opt); err == nil {
		t.Fatalf("超时应报错")
	}
}

// 取消：ctx 取消后 Send 应立刻返回「请求已取消」。
func TestCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		_, _ = w.Write([]byte("late"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	opt := DefaultOptions()
	opt.Timeout = 5 * time.Second
	_, err := Send(ctx, collection.Request{UID: "u-cancel", Method: "GET", URL: srv.URL}, nil, opt)
	if err == nil {
		t.Fatalf("取消后应报错")
	}
	if !strings.Contains(err.Error(), "取消") {
		t.Fatalf("取消错误文案不符: %v", err)
	}
}

func TestAuth(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	basic := collection.Request{UID: "u9", Method: "GET", URL: srv.URL, Auth: &collection.Auth{Type: "basic", Username: "u", Password: "p"}}
	res, err := Send(context.Background(), basic, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("basic: %v", err)
	}
	if !strings.Contains(res.Body, "Basic dTpw") {
		t.Fatalf("basic 认证头缺失: %q", res.Body)
	}

	bearer := collection.Request{UID: "u10", Method: "GET", URL: srv.URL, Auth: &collection.Auth{Type: "bearer", Token: "{{tok}}"}}
	res, err = Send(context.Background(), bearer, map[string]string{"tok": "abc"}, DefaultOptions())
	if err != nil {
		t.Fatalf("bearer: %v", err)
	}
	if !strings.Contains(res.Body, "Bearer abc") {
		t.Fatalf("bearer 认证头缺失: %q", res.Body)
	}

	apiKey := collection.Request{UID: "u11", Method: "GET", URL: srv.URL, Auth: &collection.Auth{Type: "apikey", Key: "X-Api-Key", Value: "k1", In: "query"}}
	res, err = Send(context.Background(), apiKey, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("apikey: %v", err)
	}
	if !strings.Contains(res.Body, "X-Api-Key") {
		t.Fatalf("apikey 查询参数缺失: %q", res.Body)
	}
}

func TestMultipartBody(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()
	r := collection.Request{
		UID: "u12", Method: "POST", URL: srv.URL + "/upload",
		Body: collection.Body{Type: "multipart", Form: []collection.KV{{Name: "field", Value: "v1", Enabled: true}}},
	}
	res, err := Send(context.Background(), r, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	var echo map[string]any
	_ = json.Unmarshal([]byte(res.Body), &echo)
	h := echo["header"].(map[string]any)
	if !strings.HasPrefix(fmt.Sprint(h["Content-Type"]), "multipart/form-data; boundary=") {
		t.Fatalf("multipart Content-Type: %v", h["Content-Type"])
	}
	if !strings.Contains(res.Body, "name=\\\"field\\\"") {
		t.Fatalf("multipart 字段缺失: %q", res.Body)
	}
}

// multipart 文件 part：type=file 时 value 为路径，读盘后按文件字段上传。
func TestMultipartFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("file-content"), 0o644); err != nil {
		t.Fatalf("写临时文件: %v", err)
	}
	srv := echoServer(t)
	defer srv.Close()
	r := collection.Request{
		UID: "u12f", Method: "POST", URL: srv.URL + "/upload",
		Body: collection.Body{Type: "multipart", Form: []collection.KV{
			{Name: "file", Value: path, Enabled: true, Type: "file"},
			{Name: "note", Value: "hi", Enabled: true, Type: "text"},
		}},
	}
	res, err := Send(context.Background(), r, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !strings.Contains(res.Body, "hello.txt") {
		t.Fatalf("文件名缺失: %q", res.Body)
	}
	if !strings.Contains(res.Body, "file-content") {
		t.Fatalf("文件内容缺失: %q", res.Body)
	}
	if !strings.Contains(res.Body, "name=\\\"note\\\"") {
		t.Fatalf("文本字段缺失: %q", res.Body)
	}
}

func TestBinaryResponse(t *testing.T) {
	raw := []byte{0x89, 'P', 'N', 'G', 0x00, 0x1a, 0xff}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(raw)
	}))
	defer srv.Close()
	res, err := Send(context.Background(), collection.Request{UID: "u13", Method: "GET", URL: srv.URL}, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !res.Binary {
		t.Fatalf("应识别为二进制响应")
	}
	if res.ContentType != "image/png" || res.Size != len(raw) {
		t.Fatalf("元信息异常: %+v", res)
	}
	got, err := base64.StdEncoding.DecodeString(res.Body)
	if err != nil || string(got) != string(raw) {
		t.Fatalf("base64 还原失败: %v", err)
	}
}

// 请求级 settings 覆盖全局策略。
func TestRequestSettingsOverride(t *testing.T) {
	srv := redirector(t)
	defer srv.Close()
	no := false
	r := collection.Request{
		UID: "u14", Method: "GET", URL: srv.URL + "/r1",
		Settings: &collection.RequestSettings{FollowRedirects: &no},
	}
	res, err := Send(context.Background(), r, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Status != http.StatusFound {
		t.Fatalf("请求级 followRedirects=false 未生效，status=%d", res.Status)
	}

	// 请求级开启忽略证书，覆盖全局默认（关闭）
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer tlsSrv.Close()
	yes := true
	insecure := collection.Request{
		UID: "u15", Method: "GET", URL: tlsSrv.URL,
		Settings: &collection.RequestSettings{InsecureSSL: &yes},
	}
	if _, err := Send(context.Background(), insecure, nil, DefaultOptions()); err != nil {
		t.Fatalf("请求级 insecureSsl 未生效: %v", err)
	}
}

// 未开启忽略证书时，自定义 Transport 不应被 TLS 之外的场景破坏（回归）。
func TestPlainHTTPSStillWorks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("plain"))
	}))
	defer srv.Close()
	opt := DefaultOptions()
	opt.InsecureSSL = true // 即使开启，也应能正常访问 http
	res, err := Send(context.Background(), collection.Request{UID: "u16", Method: "GET", URL: srv.URL}, nil, opt)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Body != "plain" {
		t.Fatalf("body: %q", res.Body)
	}
}
