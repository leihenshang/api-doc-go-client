package mocksrv

import (
	"io"
	"net/http"
	"testing"

	"api-doc-go-client/internal/collection"
)

// 示例里缺 status（YAML 默认 0）时不能直接把 0 交给 net/http：WriteHeader(0) 会 panic，
// Mock 连接被重置。这里断言回退到 200 且响应体照常回放。
func TestMockToleratesMissingStatus(t *testing.T) {
	dir := t.TempDir()
	c, err := collection.Open(dir)
	if err != nil {
		t.Fatalf("打开集合: %v", err)
	}
	defer c.StopWatch()
	r, err := c.CreateRequest("", "ping", "GET")
	if err != nil {
		t.Fatalf("建请求: %v", err)
	}
	r.URL = "/ping"
	if err := c.SaveRequest(r); err != nil {
		t.Fatalf("保存请求: %v", err)
	}
	// 存一条 status=0（缺字段）的示例：LatestExample 只要求 body 非空
	if _, err := c.SaveResponseExample(r.UID, "zero", collection.ExampleRequest{Method: "GET", URL: "/ping"}, collection.ExampleResponse{
		Status: 0, ContentType: "application/json", Body: `{"ok":true}`,
	}); err != nil {
		t.Fatalf("保存示例: %v", err)
	}

	s := New(c)
	st, err := s.Start(0)
	if err != nil {
		t.Fatalf("启动 Mock: %v", err)
	}
	defer func() { _ = s.Stop() }()

	resp, err := http.Get(st.URL + "/ping")
	if err != nil {
		t.Fatalf("请求 Mock: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("非法状态码应回退 200，实际 %d（body=%s）", resp.StatusCode, body)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("应回放示例响应体，实际 %s", body)
	}
}
