package app

// U5：分享链接解析与「分享接口 → 可发送请求」的字段映射。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseShareURL(t *testing.T) {
	ok := []struct {
		in, base, token string
	}{
		{"http://127.0.0.1:8080/share/abc123", "http://127.0.0.1:8080", "abc123"},
		{"https://api.example.com/share/xyz", "https://api.example.com", "xyz"},
		{"127.0.0.1:8080/share/tok", "http://127.0.0.1:8080", "tok"},
		{"http://h/share/abc/", "http://h", "abc"},
	}
	for _, c := range ok {
		base, token, err := parseShareURL(c.in)
		if err != nil {
			t.Fatalf("parseShareURL(%q) 报错: %v", c.in, err)
		}
		if base != c.base || token != c.token {
			t.Fatalf("parseShareURL(%q) = (%q,%q)，期望 (%q,%q)", c.in, base, token, c.base, c.token)
		}
	}

	bad := []string{"", "abc123", "http://h/project/1", "http://h/share/"}
	for _, in := range bad {
		if _, _, err := parseShareURL(in); err == nil {
			t.Fatalf("parseShareURL(%q) 应报错", in)
		}
	}
}

func TestShareAPIToRequest(t *testing.T) {
	a := ShareApi{
		ID:                    7,
		ApiName:               "登录",
		HttpMethodType:        "post",
		URL:                   "{{host}}/login",
		Description:           "# 说明",
		HttpRequestSummary:    "登录接口",
		HttpRequestParams:     `[{"name":"q","value":"1","enabled":true}]`,
		HttpRequestHeader:     `[{"name":"X-A","value":"1","enabled":true}]`,
		HttpRequestBodyType:   "json",
		HttpRequestBody:       `{"a":1}`,
		HttpRequestBodyForm:   `[{"name":"f","value":"v","enabled":true}]`,
		HttpRequestBodyDescs:  `{"f":"字段说明"}`,
		HttpAuth:              `{"type":"bearer","token":"t"}`,
		HttpScript:            `{"preRequest":"// x"}`,
		ScriptVars:            `[{"name":"v","value":"1","enabled":true}]`,
		ScriptAsserts:         `[{"expr":"status==200"}]`,
		HttpReturnType:        "object",
		HttpReturnParams:      `[{"fieldName":"id","type":"integer"}]`,
		HttpReturnSample:      `{"id":1}`,
		HttpReturnContentType: "json",
	}
	r := shareAPIToRequest(a)

	if r.Method != "POST" || r.URL != "{{host}}/login" || r.Name != "登录" {
		t.Fatalf("基础字段不符: %+v", r)
	}
	if r.Summary != "登录接口" || r.Docs != "# 说明" {
		t.Fatalf("概述/说明未映射: %+v", r)
	}
	if len(r.Params) != 1 || r.Params[0].Name != "q" || !r.Params[0].Enabled {
		t.Fatalf("查询参数未映射: %+v", r.Params)
	}
	if len(r.Headers) != 1 || r.Headers[0].Name != "X-A" {
		t.Fatalf("请求头未映射: %+v", r.Headers)
	}
	if r.Body.Type != "json" || r.Body.Raw != `{"a":1}` {
		t.Fatalf("请求体未映射: %+v", r.Body)
	}
	if len(r.Body.Form) != 1 || r.Body.Descs["f"] != "字段说明" {
		t.Fatalf("表单/字段说明未映射: %+v", r.Body)
	}
	if r.Auth == nil || r.Auth.Type != "bearer" || r.Auth.Token != "t" {
		t.Fatalf("认证未映射: %+v", r.Auth)
	}
	if r.Script == nil || r.Script.PreRequest != "// x" {
		t.Fatalf("脚本未映射: %+v", r.Script)
	}
	if len(r.VarsPreRequest) != 1 || r.VarsPreRequest[0].Name != "v" {
		t.Fatalf("前置变量未映射: %+v", r.VarsPreRequest)
	}
	if len(r.Asserts) != 1 {
		t.Fatalf("断言未映射: %+v", r.Asserts)
	}
	if r.RespDoc == nil || r.RespDoc.Type != "object" || r.RespDoc.Sample != `{"id":1}` || len(r.RespDoc.Params) != 1 {
		t.Fatalf("响应文档未映射: %+v", r.RespDoc)
	}
}

// 脏数据（非法 JSON）不能把整条接口弄崩：解析失败的列按「未设置」处理。
func TestShareAPIToRequestTolerantToBadJSON(t *testing.T) {
	r := shareAPIToRequest(ShareApi{
		ID: 1, ApiName: "x", HttpMethodType: "get", URL: "/x",
		HttpRequestParams: "{not-an-array", HttpAuth: "not-json", HttpScript: "oops",
	})
	if r.Method != "GET" || len(r.Params) != 0 || r.Auth != nil || r.Script != nil {
		t.Fatalf("非法 JSON 应被忽略: %+v", r)
	}
}

// U5 端到端（进程内）：真实 HTTP 拉取分享数据 + 真实发送。
func TestFetchShareAndSend(t *testing.T) {
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/share/tok123", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200, "msg": "success",
			"data": map[string]any{
				"project": map[string]any{"id": 1, "title": "分享项目", "description": "说明", "version": "v1"},
				"groups":  []any{map[string]any{"id": 1, "p_id": 0, "title": "分组", "type": 1, "priority": 0}},
				"apis": []any{map[string]any{
					"id": 7, "group_id": 1, "api_name": "分享接口", "http_method_type": "GET",
					"url": srv.URL + "/json/flat", "http_request_summary": "分享用",
					"http_request_header": `[{"name":"X-S","value":"1","enabled":true}]`,
				}},
				"docs": []any{map[string]any{"id": 9, "group_id": 1, "title": "分享文档", "content": "# 正文"}},
			},
		})
	})
	mux.HandleFunc("/json/flat", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-S") != "1" {
			w.WriteHeader(400)
			return
		}
		_, _ = w.Write([]byte(`{"flat":true}`))
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	core, _ := newTestApp(t)
	b, err := core.FetchShare(srv.URL + "/share/tok123")
	if err != nil {
		t.Fatalf("FetchShare: %v", err)
	}
	if b.Project.Title != "分享项目" || len(b.Apis) != 1 || len(b.Docs) != 1 {
		t.Fatalf("分享数据不符: %+v", b)
	}
	if b.BaseURL != srv.URL {
		t.Fatalf("BaseURL 应回填服务器地址: %q", b.BaseURL)
	}

	// 真实发送：请求头从分享数据里带出来，服务端据此返回 200。
	res, err := core.SendSharedRequest(b.Apis[0], "")
	if err != nil {
		t.Fatalf("SendSharedRequest: %v", err)
	}
	if res.Status != 200 {
		t.Fatalf("发送应返回 200（请求头来自分享数据），实际 %d body=%s", res.Status, res.Body)
	}

	// 失效令牌：服务端 404 → 要给出可读错误，而不是 panic/空数据。
	if _, err := core.FetchShare(srv.URL + "/share/missing"); err == nil {
		t.Fatal("无效令牌应报错")
	}
}
