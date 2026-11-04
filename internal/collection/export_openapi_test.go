package collection

// U4：OpenAPI 3.0 导出（路径/方法/参数/响应样例），并保证是合法 JSON。

import (
	"encoding/json"
	"testing"
)

func TestExportOpenAPI(t *testing.T) {
	c := newTestCollection(t)
	c.Name = "shop"

	if err := c.CreateFolder("", "用户"); err != nil {
		t.Fatalf("建目录: %v", err)
	}
	r, err := c.CreateRequest("用户", "用户-列表", "GET")
	if err != nil {
		t.Fatalf("建请求: %v", err)
	}
	r.URL = "{{host}}/api/users?page=1"
	r.Summary = "用户列表"
	r.Docs = "## 说明"
	r.Params = []KV{{Name: "page", Value: "1", Enabled: true, Description: "页码"}}
	r.Headers = []KV{{Name: "X-Token", Value: "t", Enabled: true}}
	r.RespDoc = &ResponseDoc{Type: "object", Sample: `{"id":1}`, Status: 200}
	if err := c.SaveRequest(r); err != nil {
		t.Fatalf("保存请求: %v", err)
	}

	// gRPC 请求不应出现在 OpenAPI 里（用门面创建，避免 Path 为空被当成重命名）
	g, err := c.CreateRequest("", "grpc-调用", MethodGRPC)
	if err != nil {
		t.Fatalf("建 gRPC 请求: %v", err)
	}
	g.GRPC = &GrpcBlock{Target: "127.0.0.1:9", Service: "s", Method: "M"}
	if err := c.SaveRequest(g); err != nil {
		t.Fatalf("保存 gRPC 请求: %v", err)
	}

	raw, err := c.ExportOpenAPI()
	if err != nil {
		t.Fatalf("ExportOpenAPI: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("导出结果不是合法 JSON: %v\n%s", err, raw)
	}
	if doc["openapi"] == nil {
		t.Fatalf("缺少 openapi 版本字段: %s", raw)
	}
	paths, _ := doc["paths"].(map[string]any)
	item, ok := paths["/api/users"].(map[string]any)
	if !ok {
		t.Fatalf("paths 里没有 /api/users: %s", raw)
	}
	get, ok := item["get"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 get 操作: %s", raw)
	}
	if get["summary"] != "用户列表" {
		t.Fatalf("summary 未带出: %v", get["summary"])
	}
	params, _ := get["parameters"].([]any)
	var hasQuery, hasHeader bool
	for _, raw := range params {
		p, _ := raw.(map[string]any)
		if p["name"] == "page" && p["in"] == "query" {
			hasQuery = true
		}
		if p["name"] == "X-Token" && p["in"] == "header" {
			hasHeader = true
		}
	}
	if !hasQuery || !hasHeader {
		t.Fatalf("查询参数/请求头未映射: %s", raw)
	}
	responses, _ := get["responses"].(map[string]any)
	if _, ok := responses["200"]; !ok {
		t.Fatalf("响应样例未带出: %s", raw)
	}
}

func TestOpenAPIPathExtraction(t *testing.T) {
	cases := map[string]string{
		"{{host}}/api/users":      "/api/users",
		"{{host}}/api/users?x=1":  "/api/users",
		"https://a.com/v1/x#frag": "/v1/x",
		"/plain/path":             "/plain/path",
		"{{host}}":                "/",
		"":                        "/",
	}
	for in, want := range cases {
		if got := openAPIPath(in); got != want {
			t.Errorf("openAPIPath(%q) = %q，期望 %q", in, got, want)
		}
	}
}
