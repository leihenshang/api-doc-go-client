package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newFixtureServer 起一个夹具实例（复用与 main 相同的路由装配，保证测试的就是生产的端点）。
func newFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	registerJSONRoutes(mux)
	registerFaultRoutes(mux)
	registerShapeRoutes(mux)
	registerSessionRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func getJSON[T any](t *testing.T, url string) (int, T) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer res.Body.Close()
	var out T
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

// e2e 的 readiness 探针就是 GET /json/flat，它必须 200。
func TestJSONFlatIsProbe(t *testing.T) {
	srv := newFixtureServer(t)
	code, body := getJSON[map[string]any](t, srv.URL+"/json/flat")
	if code != 200 || body["flat"] != true {
		t.Fatalf("json/flat = %d %v", code, body)
	}
}

// 「字段只追加」用例的基线：withArray 的叶子字段必须含 items.0.id/name，fewer 必须是其子集。
func TestJSONWithArrayAndFewerAreCompatible(t *testing.T) {
	srv := newFixtureServer(t)
	code, withArray := getJSON[map[string]any](t, srv.URL+"/json/withArray")
	if code != 200 {
		t.Fatalf("withArray 状态码 %d", code)
	}
	items, ok := withArray["items"].([]any)
	if !ok || len(items) != 2 || withArray["total"] != float64(2) {
		t.Fatalf("withArray 形状不符: %v", withArray)
	}
	first, _ := items[0].(map[string]any)
	if first["id"] == nil || first["name"] == nil {
		t.Fatalf("withArray.items[0] 应含 id/name: %v", first)
	}

	_, fewer := getJSON[map[string]any](t, srv.URL+"/json/fewer")
	if len(fewer) == 0 {
		t.Fatal("fewer 不应为空")
	}
	for leaf := range fewer {
		// fewer 的每个顶层键都必须能在 withArray 里找到（叶子字段是它的子集）
		if _, ok := withArray[leaf]; !ok {
			t.Fatalf("fewer 的字段 %q 不在 withArray 里，会破坏「只追加」断言", leaf)
		}
	}
}

// /redirect/{n} 跳 n 次后落到 {"hops":0}（用例断言 hops === 0）。
func TestRedirectChainEndsAtZero(t *testing.T) {
	srv := newFixtureServer(t)
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return nil },
	}
	res, err := client.Get(srv.URL + "/redirect/2")
	if err != nil {
		t.Fatalf("GET /redirect/2: %v", err)
	}
	defer res.Body.Close()
	// 跟随到底：最终请求的路径应是 /redirect/0
	if !strings.HasSuffix(res.Request.URL.Path, "/redirect/0") {
		t.Fatalf("最终路径 = %s，期望 /redirect/0", res.Request.URL.Path)
	}
	var body map[string]any
	_ = json.NewDecoder(res.Body).Decode(&body)
	if body["hops"] != float64(0) {
		t.Fatalf("hops = %v，期望 0", body["hops"])
	}

	// 不跟随：拿到 302 本身
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res2, err := noFollow.Get(srv.URL + "/redirect/2")
	if err != nil {
		t.Fatalf("GET(不跟随) /redirect/2: %v", err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusFound {
		t.Fatalf("不跟随时状态码 = %d，期望 302", res2.StatusCode)
	}
}

// 自环分支必须永不收敛（用例靠它触发「超过重定向上限」）。
func TestRedirectLoopNeverSettles(t *testing.T) {
	srv := newFixtureServer(t)
	client := &http.Client{
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return http.ErrUseLastResponse // 三跳后停手，证明它一直在跳
			}
			return nil
		},
	}
	res, err := client.Get(srv.URL + "/redirect/1?loop=1")
	if err != nil {
		t.Fatalf("GET /redirect/1?loop=1: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusFound {
		t.Fatalf("自环应一直是 302，实际 %d", res.StatusCode)
	}
	if !strings.Contains(res.Header.Get("Location"), "loop=1") {
		t.Fatalf("自环应保留查询参数，Location = %q", res.Header.Get("Location"))
	}
}

// 204 不能带响应体（用例断言响应体为空）。
func TestStatus204HasNoBody(t *testing.T) {
	srv := newFixtureServer(t)
	res, err := http.Get(srv.URL + "/status/204")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode != 204 || len(data) != 0 {
		t.Fatalf("/status/204 = %d, body=%q", res.StatusCode, data)
	}

	code, body := getJSON[map[string]any](t, srv.URL+"/status/404")
	if code != 404 || body["status"] != float64(404) {
		t.Fatalf("/status/404 = %d %v", code, body)
	}
}

// 二进制 / 非 UTF-8 / 文本声明的字节必须一致，只有 Content-Type 不同，
// 这样「判二进制 vs 按文本展示」两条分支才是同一个输入。
func TestShapeEndpoints(t *testing.T) {
	srv := newFixtureServer(t)
	res, err := http.Get(srv.URL + "/binary?kb=2")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.Header.Get("Content-Type") != "application/octet-stream" || len(body) != 2048 {
		t.Fatalf("binary: ct=%q len=%d", res.Header.Get("Content-Type"), len(body))
	}

	neutral, err := http.Get(srv.URL + "/charset/latin1")
	if err != nil {
		t.Fatal(err)
	}
	neutralBody, _ := io.ReadAll(neutral.Body)
	neutral.Body.Close()
	if strings.HasPrefix(neutral.Header.Get("Content-Type"), "text/") {
		t.Fatalf("charset/latin1 应为中性类型，实际 %q", neutral.Header.Get("Content-Type"))
	}

	asText, err := http.Get(srv.URL + "/text/latin1")
	if err != nil {
		t.Fatal(err)
	}
	textBody, _ := io.ReadAll(asText.Body)
	asText.Body.Close()
	if !strings.HasPrefix(asText.Header.Get("Content-Type"), "text/") {
		t.Fatalf("text/latin1 应声明 text/*，实际 %q", asText.Header.Get("Content-Type"))
	}
	if string(neutralBody) != string(textBody) {
		t.Fatal("两个端点的字节应完全一致（只有 Content-Type 不同）")
	}
	if len(neutralBody) == 0 {
		t.Fatal("非 UTF-8 端点不应为空")
	}
}

// gzip：客户端（Go）会自动解压，解压后必须是 {"gzipped":true}。
func TestGzipEndpointDecodes(t *testing.T) {
	srv := newFixtureServer(t)
	code, body := getJSON[map[string]any](t, srv.URL+"/gzip")
	if code != 200 || body["gzipped"] != true {
		t.Fatalf("gzip = %d %v", code, body)
	}
}

// Cookie 写入必须是 host-only（不带 Domain）且 Path=/，后续请求才会带上。
func TestCookieSetAndEcho(t *testing.T) {
	srv := newFixtureServer(t)
	res, err := http.Get(srv.URL + "/cookies/set?name=e2eCookie&value=kept")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	raw := res.Header.Get("Set-Cookie")
	if !strings.Contains(raw, "e2eCookie=kept") || strings.Contains(strings.ToLower(raw), "domain=") {
		t.Fatalf("Set-Cookie 不符: %q", raw)
	}
	if !strings.Contains(raw, "Path=/") {
		t.Fatalf("应显式给 Path=/，实际 %q", raw)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/cookies/echo", nil)
	req.AddCookie(&http.Cookie{Name: "e2eCookie", Value: "kept"})
	echoed, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer echoed.Body.Close()
	text, _ := io.ReadAll(echoed.Body)
	if !strings.Contains(string(text), "e2eCookie=kept") {
		t.Fatalf("回显正文 = %q", text)
	}
}

// 认证端点：无头 401、有头 200 且回显 Basic 凭据。
func TestAuthRequire(t *testing.T) {
	srv := newFixtureServer(t)
	res, err := http.Get(srv.URL + "/auth/require")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("无凭据应 401，实际 %d", res.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/auth/require", nil)
	req.SetBasicAuth("e2e", "secret")
	ok, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer ok.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(ok.Body).Decode(&body)
	if ok.StatusCode != 200 || !strings.HasPrefix(body["auth"].(string), "Basic ") {
		t.Fatalf("有凭据应 200 且回显 Basic，实际 %d %v", ok.StatusCode, body)
	}
}

// multipart 回显：contentType 必须是去掉参数的媒体类型（证明客户端带了 boundary）。
func TestMultipartEcho(t *testing.T) {
	srv := newFixtureServer(t)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("field1", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	res, err := http.Post(srv.URL+"/multipart", mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		ContentType string            `json:"contentType"`
		Fields      map[string]string `json:"fields"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.ContentType != "multipart/form-data" {
		t.Fatalf("contentType = %q", out.ContentType)
	}
	if out.Fields["field1"] != "v1" {
		t.Fatalf("fields = %v", out.Fields)
	}

	// 非 multipart 请求应被明确拒绝（而不是当成空表单）
	plain, err := http.Post(srv.URL+"/multipart", "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Body.Close()
	if plain.StatusCode != http.StatusBadRequest {
		t.Fatalf("非 multipart 应 400，实际 %d", plain.StatusCode)
	}
}
