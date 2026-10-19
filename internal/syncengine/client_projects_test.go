package syncengine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 项目发现：带 PAT 请求 GET /api/project，按统一 {code,msg,data} 解包。
func TestProjects(t *testing.T) {
	var gotAuth, gotPath, gotPageSize string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotPageSize = r.URL.Query().Get("page_size")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200, "msg": "success",
			"data": map[string]any{
				"items": []map[string]any{
					{"id": 7, "title": "支付中台"},
					{"id": 9, "title": "开放平台"},
				},
				"total": 2,
			},
		})
	}))
	defer srv.Close()

	ps, err := New(srv.URL, "pat_test", "dev-1").Projects()
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	if gotPath != "/api/project" {
		t.Fatalf("请求路径应为 /api/project，got %q", gotPath)
	}
	if gotAuth != "Bearer pat_test" {
		t.Fatalf("必须带 PAT，got %q", gotAuth)
	}
	if gotPageSize != "200" {
		t.Fatalf("page_size 应取服务端上限 200，got %q", gotPageSize)
	}
	if len(ps) != 2 || ps[0].ID != 7 || ps[0].Title != "支付中台" {
		t.Fatalf("项目解析不对: %+v", ps)
	}
}

// 业务码非 200（令牌无效/无权限）必须报错，不能当成空列表。
func TestProjectsRejectsNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 401, "msg": "令牌无效"})
	}))
	defer srv.Close()

	if _, err := New(srv.URL, "pat_bad", "dev-1").Projects(); err == nil {
		t.Fatal("403/401 应报错")
	}
}
