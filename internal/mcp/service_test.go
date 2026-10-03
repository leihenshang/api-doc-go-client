package mcp_test

import (
	. "api-doc-go-client/internal/mcp" //nolint:revive // 历史测试文件：沿用未加前缀的调用写法
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// setup 建一个项目（含 manifest），返回 root 与 Service。
func setup(t *testing.T) (string, *Service) {
	t.Helper()
	root := t.TempDir()
	newProjectDir(t, root, "demo", "uid-demo")
	reg, err := NewRegistry(root, testNewApp)
	if err != nil {
		t.Fatal(err)
	}
	return root, NewService(reg)
}

func TestListProjectsAndModules(t *testing.T) {
	_, svc := setup(t)
	if _, _, err := svc.Reg().App("demo"); err != nil { // 打开以填充统计
		t.Fatal(err)
	}
	items, skipped, err := svc.ListProjects("", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Path != "demo" || items[0].UID != "uid-demo" {
		t.Fatalf("项目列表异常: %+v", items)
	}
	if skipped != 0 {
		t.Errorf("不应有跳过项: %d", skipped)
	}
	// 模糊过滤
	items, _, err = svc.ListProjects("dem", false)
	if err != nil || len(items) != 1 {
		t.Errorf("模糊查询应命中 1 个: %+v %v", items, err)
	}
	items, _, err = svc.ListProjects("zzz", false)
	if err != nil || len(items) != 0 {
		t.Errorf("无关查询应返回空: %+v %v", items, err)
	}
}

func TestModulesAndSearchAndDetail(t *testing.T) {
	_, svc := setup(t)
	a, _, err := svc.Reg().App("demo")
	if err != nil {
		t.Fatal(err)
	}
	// 建两个模块 + 三个请求
	if err := a.CreateFolder("", "api"); err != nil {
		t.Fatal(err)
	}
	if err := a.CreateFolder("api", "user"); err != nil {
		t.Fatal(err)
	}
	mk := func(folder, name, method, url, docs string) string {
		r, err := a.CreateRequest(folder, name, method)
		if err != nil {
			t.Fatal(err)
		}
		r.URL = url
		r.Docs = docs
		if err := a.SaveRequest(r); err != nil {
			t.Fatal(err)
		}
		return r.UID
	}
	ping := mk("", "ping", "GET", "http://127.0.0.1:1/ping", "探活接口")
	mk("api", "user-list", "GET", "http://127.0.0.1:1/users", "用户列表")
	uidOrder := mk("api/user", "order-detail", "GET", "http://127.0.0.1:1/orders/1", "订单详情")

	// 模块树
	mods, err := svc.GetProjectModules("demo", "", true)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]int{}
	for _, m := range mods.Modules {
		names[m.Path] = m.Requests
	}
	// api 模块下有 user-list，api/user 下有 order-detail，根目录有 ping
	if names["api"] != 1 || names["api/user"] != 1 || names["(根目录)"] != 1 {
		t.Errorf("模块请求数统计异常: %+v", mods.Modules)
	}
	if mods.Total != 3 {
		t.Errorf("请求总数应为 3: %d", mods.Total)
	}
	// 模块名模糊
	// "user" 同时命中 user-list（名称）与 order-detail（路径 api/user）
	mods, err = svc.GetProjectModules("demo", "user", false)
	if err != nil || mods.Matched != 2 {
		t.Errorf("模块内模糊匹配应命中 2 个: %+v %v", mods, err)
	}

	// 搜索：名称 / URL / docs 三种命中
	for _, tc := range []struct{ q, wantName string }{
		{"order", "order-detail"},
		{"orders/1", "order-detail"},
		{"用户列表", "user-list"},
		{"探活", "ping"},
	} {
		ms, _, err := svc.SearchRequests(tc.q, "demo", 10)
		if err != nil {
			t.Fatalf("搜索 %q 失败: %v", tc.q, err)
		}
		if len(ms) == 0 || ms[0].Name != tc.wantName {
			t.Errorf("搜索 %q 期望首位 %q，实际 %+v", tc.q, tc.wantName, ms)
		}
	}
	if _, _, err := svc.SearchRequests("", "demo", 10); err == nil {
		t.Error("空 query 应报错")
	}

	// 详情：headers / docs / 空示例
	d, err := svc.GetRequestDetail("demo", ping, DetailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "ping" || d.Method != "GET" || d.Docs != "探活接口" {
		t.Errorf("详情字段异常: %+v", d)
	}
	if len(d.Headers) == 0 {
		t.Error("详情应带出 headers")
	}
	if len(d.Examples) != 0 {
		t.Errorf("新请求不该有示例: %+v", d.Examples)
	}
	// 关联文档：走 API 建一条 docs 条目（不手写 frontmatter，格式以集合层为准）
	doc, err := a.CreateDoc("ping 接口说明")
	if err != nil {
		t.Fatal(err)
	}
	doc.Content = "正文"
	if err := a.SaveDoc(doc); err != nil {
		t.Fatal(err)
	}
	d, err = svc.GetRequestDetail("demo", ping, DetailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.RelatedDocs) == 0 {
		t.Errorf("应关联到 docs/ping.md: %+v", d)
	}
	_ = uidOrder
}

func TestTruncate(t *testing.T) {
	s := strings.Repeat("接口", 100) // 200 个汉字 = 600 字节
	got, truncated, total := Truncate(s, 10)
	if !truncated || total != 600 {
		t.Errorf("应被截断: truncated=%v total=%d", truncated, total)
	}
	if len(got) > 10 {
		t.Errorf("截断后字节数应 <=10: %d", len(got))
	}
	if _, tr, _ := Truncate("abc", 10); tr {
		t.Error("未超限不应标记截断")
	}
	// 显式 0 走默认（8 KiB）：600 字节不截断，超大串才截断
	if _, tr, _ := Truncate(s, 0); tr {
		t.Error("600 字节不应被默认 8 KiB 截断")
	}
	if _, tr, _ := Truncate(strings.Repeat("x", 9000), 0); !tr {
		t.Error("超过默认上限时应截断")
	}
}

func TestSearchAgainstLiveServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	_, svc := setup(t)
	a, _, err := svc.Reg().App("demo")
	if err != nil {
		t.Fatal(err)
	}
	r, err := a.CreateRequest("", "live", "GET")
	if err != nil {
		t.Fatal(err)
	}
	r.URL = srv.URL + "/live"
	if err := a.SaveRequest(r); err != nil {
		t.Fatal(err)
	}
	ms, _, err := svc.SearchRequests("live", "demo", 5)
	if err != nil || len(ms) != 1 {
		t.Fatalf("应能搜到刚建的请求: %+v %v", ms, err)
	}
}
