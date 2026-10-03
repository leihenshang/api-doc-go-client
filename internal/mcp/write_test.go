package mcp_test

import (
	. "api-doc-go-client/internal/mcp" //nolint:revive // 历史测试文件：沿用未加前缀的调用写法
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"api-doc-go-client/internal/collection"
)

func boolPtr(b bool) *bool    { return &b }
func strPtr(s string) *string { return &s }

func TestCreateRequestVariants(t *testing.T) {
	_, svc := setup(t)

	// 1) 手填参数
	r, err := svc.CreateRequest(CreateRequestInput{
		Project: "demo", Name: "ping", Method: "get", URL: "http://127.0.0.1/ping",
		Headers: []collection.KV{{Name: "X-Test", Value: "1", Enabled: true}},
		Docs:    "探活",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Method != "GET" || r.URL != "http://127.0.0.1/ping" || r.Docs != "探活" {
		t.Errorf("创建结果异常: %+v", r)
	}
	if len(r.Headers) != 1 || r.Headers[0].Name != "X-Test" {
		t.Errorf("headers 未生效: %+v", r.Headers)
	}

	// 2) 缺 name 应报错
	if _, err := svc.CreateRequest(CreateRequestInput{Project: "demo", URL: "x"}); err == nil {
		t.Error("缺 name 应报错")
	}

	// 3) cURL 导入
	r2, err := svc.CreateRequest(CreateRequestInput{
		Project: "demo", Name: "from-curl",
		Curl: `curl -X POST http://127.0.0.1/api/login -H "Content-Type: application/json" -d '{"u":"a"}'`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Method != "POST" || !strings.Contains(r2.URL, "/api/login") {
		t.Errorf("cURL 导入异常: %+v", r2)
	}

	// 4) 复制新建
	r3, err := svc.CreateRequest(CreateRequestInput{Project: "demo", Name: "ping-copy", CopyFromUID: r.UID})
	if err != nil {
		t.Fatal(err)
	}
	if r3.UID == r.UID {
		t.Error("复制新建应产生新的 uid")
	}
	if r3.URL != r.URL {
		t.Errorf("复制应带上原内容: %+v", r3)
	}
}

func TestCreateModuleAndUpdateDelete(t *testing.T) {
	root, svc := setup(t)
	mod, err := svc.CreateModule("demo", "", "api")
	if err != nil || mod != "api" {
		t.Fatalf("创建模块异常: %s %v", mod, err)
	}
	if _, err := os.Stat(filepath.Join(root, "demo", "api", "folder.yml")); err != nil {
		t.Errorf("分组文件未落盘: %v", err)
	}
	nested, err := svc.CreateModule("demo", "api", "user")
	if err != nil || nested != "api/user" {
		t.Fatalf("嵌套模块异常: %s %v", nested, err)
	}
	if _, err := svc.CreateModule("demo", "", ""); err == nil {
		t.Error("空模块名应报错")
	}

	// 建请求 → 改 URL/说明 → 移动模块 → 删除（进 .trash）
	r, err := svc.CreateRequest(CreateRequestInput{Project: "demo", Folder: "api/user", Name: "detail", Method: "GET", URL: "http://x/1"})
	if err != nil {
		t.Fatal(err)
	}
	newURL, newDocs := "http://x/2", "改过的说明"
	up, err := svc.UpdateRequest(UpdateRequestInput{
		Project: "demo", UID: r.UID, URL: &newURL, Docs: &newDocs, Folder: strPtr("api"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if up.URL != newURL || up.Docs != newDocs {
		t.Errorf("更新未生效: %+v", up)
	}
	if !strings.HasPrefix(up.Path, "api/") || strings.Contains(up.Path, "user/") {
		t.Errorf("移动模块未生效: %s", up.Path)
	}

	if err := svc.DeleteRequest("demo", r.UID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "demo", up.Path)); !os.IsNotExist(err) {
		t.Error("请求文件应被移走")
	}
	ents, _ := os.ReadDir(filepath.Join(root, "demo", ".trash"))
	found := false
	for _, e := range ents {
		if strings.Contains(e.Name(), "detail") {
			found = true
		}
	}
	if !found {
		t.Errorf("删除应进 .trash: %+v", ents)
	}
	if err := svc.DeleteRequest("demo", "no-such-uid"); err == nil {
		t.Error("删除不存在的请求应报错")
	}
}

func TestUpdateRequestConflict(t *testing.T) {
	root, svc := setup(t)
	r, err := svc.CreateRequest(CreateRequestInput{Project: "demo", Name: "conflict-demo", Method: "GET", URL: "http://x/1"})
	if err != nil {
		t.Fatal(err)
	}
	// 模拟「别的编辑器改了文件」：绕过客户端直接改磁盘
	f := filepath.Join(root, "demo", r.Path)
	raw, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte(strings.Replace(string(raw), "http://x/1", "http://external/9", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	// AI 先读详情（拿到 hash），此后文件被外部改动
	d, err := svc.GetRequestDetail("demo", r.UID, DetailOptions{})
	if err != nil || d.Hash == "" {
		t.Fatalf("详情应带磁盘 hash: %+v %v", d, err)
	}
	if _, err := svc.Reload("demo"); err != nil { // 重建索引（模拟文件监听已刷新）
		t.Fatal(err)
	}
	bad := "http://mine/2"
	if _, err := svc.UpdateRequest(UpdateRequestInput{Project: "demo", UID: r.UID, URL: &bad, IfMatch: d.Hash}); err == nil {
		t.Fatal("带旧 hash 的更新应被拒绝")
	}
	// 不带 hash 时按「最后写者赢」（与客户端一致），不报冲突
	if _, err := svc.UpdateRequest(UpdateRequestInput{Project: "demo", UID: r.UID, Docs: strPtr("无保护更新")}); err != nil {
		t.Fatalf("不带 if_match 应允许写入: %v", err)
	}
	after, _ := os.ReadFile(f)
	if !strings.Contains(string(after), "http://external/9") {
		t.Error("冲突时不应覆盖磁盘内容")
	}
}

func TestSendRequestAndSaveExample(t *testing.T) {
	root, svc := setup(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"n":1}`))
	}))
	defer srv.Close()

	r, err := svc.CreateRequest(CreateRequestInput{Project: "demo", Name: "live", Method: "GET", URL: srv.URL + "/live"})
	if err != nil {
		t.Fatal(err)
	}
	// save_example=false：只发不存
	out, err := svc.SendRequest(SendRequestInput{Project: "demo", UID: r.UID, SaveExample: boolPtr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != 200 || !strings.Contains(out.Body, `"ok":true`) {
		t.Errorf("发送结果异常: %+v", out)
	}
	if out.Saved {
		t.Error("save_example=false 时不应落盘")
	}
	// 默认落盘为响应示例
	out, err = svc.SendRequest(SendRequestInput{Project: "demo", UID: r.UID, ExampleName: "200 成功"})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Saved || out.ExamplePath == "" {
		t.Fatalf("应保存示例: %+v", out)
	}
	if _, err := os.Stat(filepath.Join(root, "demo", out.ExamplePath)); err != nil {
		t.Errorf("示例文件未落盘: %v", err)
	}
	// 详情里能回看到该示例
	d, err := svc.GetRequestDetail("demo", r.UID, DetailOptions{IncludeBody: true, MaxBodyBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Examples) != 1 || d.Examples[0].Name != "200 成功" || !strings.Contains(d.Examples[0].WithBody, `"ok":true`) {
		t.Errorf("详情应带出示例与响应体: %+v", d.Examples)
	}
	// 临时请求：不落盘、也不存示例
	out, err = svc.SendRequest(SendRequestInput{Project: "demo", Method: "GET", URL: srv.URL + "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != 200 || out.Saved {
		t.Errorf("临时请求结果异常: %+v", out)
	}
	if _, err := svc.SendRequest(SendRequestInput{Project: "demo"}); err == nil {
		t.Error("既无 uid 也无 url 时应报错")
	}
}

func TestSendRequestBodyTruncation(t *testing.T) {
	_, svc := setup(t)
	big := strings.Repeat("x", 5000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()
	out, err := svc.SendRequest(SendRequestInput{
		Project: "demo", Method: "GET", URL: srv.URL, SaveExample: boolPtr(false), MaxBodyBytes: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Truncated || out.OriginalBytes != 5000 || len(out.Body) > 32 {
		t.Errorf("大响应应被裁剪并标注原始字节数: %+v", out)
	}
	// include_body=false 时不返回体
	out, err = svc.SendRequest(SendRequestInput{
		Project: "demo", Method: "GET", URL: srv.URL, SaveExample: boolPtr(false), IncludeBody: boolPtr(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Body != "" || out.Truncated {
		t.Errorf("include_body=false 时不应返回响应体: %+v", out)
	}
	var v any
	_ = json.Unmarshal([]byte(out.Body), &v) // 裁剪后可能不是合法 JSON，解析失败不应 panic
}
