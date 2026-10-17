package mocksrv

import (
	"fmt"
	"io"
	"net"
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

// TestStopWaitsForServeGoroutine P1-11：Stop 必须等 Serve goroutine 真正退出。
// 只关 listener 不等的话，反复启停会累积孤儿 goroutine；这里用「启停 N 次后
// 再断言端口可被立刻重新绑定」间接证明旧的 Serve 已经退出（端口未被占用）。
func TestStopWaitsForServeGoroutine(t *testing.T) {
	c, err := collection.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	s := New(c)
	for i := 0; i < 20; i++ {
		st, err := s.Start(0)
		if err != nil {
			t.Fatalf("Start #%d: %v", i, err)
		}
		if !st.Running || st.Port == 0 {
			t.Fatalf("Start 状态异常: %+v", st)
		}
		port := st.Port
		if err := s.Stop(); err != nil {
			t.Fatalf("Stop #%d: %v", i, err)
		}
		// Stop 返回后该端口必须立刻可以重新监听：说明监听 socket 已释放。
		ln, lerr := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if lerr != nil {
			t.Fatalf("第 %d 次停止后端口仍被占用: %v", i, lerr)
		}
		_ = ln.Close()
		// 重复 Stop 必须安全（多根并存时可能被两条路径各停一次）
		if err := s.Stop(); err != nil {
			t.Fatalf("重复 Stop 应无错: %v", err)
		}
	}
}
