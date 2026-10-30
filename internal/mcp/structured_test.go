package mcp_test

// 批次 3（清单 20261030）：列表类工具的 structuredContent + send_request 默认不落示例。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestStructuredListTools(t *testing.T) {
	cs := connectSelf(t)
	uid := uidAfter(t, mustCall(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "结构化", "folder": "docs", "content": "# 结构",
	}))
	mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "结构化接口", "method": "GET", "url": "/struct",
	})

	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.IsError {
			t.Fatalf("%s 失败：%v", name, res.Content)
		}
		sc, ok := res.StructuredContent.(map[string]any)
		if !ok {
			t.Fatalf("%s 应返回 structuredContent，实际 %T", name, res.StructuredContent)
		}
		return sc
	}

	// list_docs：总数/返回数/offset + docs[].uid/hash
	sc := call("list_docs", map[string]any{"project": "demo"})
	if sc["total"].(float64) < 1 || sc["returned"].(float64) < 1 {
		t.Errorf("list_docs 结构化计数不符：%v", sc)
	}
	docs, _ := sc["docs"].([]any)
	if len(docs) == 0 {
		t.Fatalf("list_docs 结构化 docs 为空：%v", sc)
	}
	first, _ := docs[0].(map[string]any)
	if first["uid"] != uid || len(first["hash"].(string)) != 64 {
		t.Errorf("docs[0] 应带 uid/hash：%v", first)
	}

	// search_requests：results[].uid + total
	sc = call("search_requests", map[string]any{"project": "demo", "query": "结构化接口"})
	if sc["total"].(float64) < 1 {
		t.Errorf("search_requests 结构化 total 不符：%v", sc)
	}
	results, _ := sc["results"].([]any)
	if len(results) == 0 {
		t.Fatalf("search_requests 结构化 results 为空")
	}

	// 其余列表类：至少有约定的集合键
	for _, c := range []struct {
		name, key string
		args      map[string]any
	}{
		{"list_projects", "projects", map[string]any{}},
		{"get_project_modules", "modules", map[string]any{"project": "demo"}},
		{"list_envs", "envs", map[string]any{"project": "demo"}},
		{"search_docs", "docs", map[string]any{"project": "demo", "pattern": "结构"}},
	} {
		sc = call(c.name, c.args)
		if _, ok := sc[c.key]; !ok {
			t.Errorf("%s 结构化应含 %q：%v", c.name, c.key, sc)
		}
	}

	// outputSchema 只在列表类工具上声明
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"list_projects": true, "list_docs": true, "list_envs": true,
		"list_tools":          true,
		"get_project_modules": true, "search_requests": true, "search_docs": true,
		// 写/单条读也补：调用方拿 uid/path/hash 无需二次查询
		"create_doc": true, "update_doc": true, "delete_doc": true,
		"create_request": true, "update_request": true, "delete_request": true, "import_collection": true,
		"update_requests": true,
		// 其余写工具（统一写契约：所有落盘工具都带 outputSchema/structuredContent）
		"create_module": true, "rename_module": true, "delete_module": true,
		"create_project": true, "rename_project": true, "delete_project": true,
		"save_doc_image":        true,
		"save_response_example": true, "update_response_example": true, "delete_response_example": true,
		"create_env": true, "rename_env": true, "delete_env": true,
		"set_env_var": true, "delete_env_var": true,
		"delete_requests": true, "send_request": true,
		// get_doc / get_request_detail 单条与批量共用同一工具（两种形态），不声明 outputSchema。
	}
	for _, tool := range tools.Tools {
		has := tool.OutputSchema != nil
		if want[tool.Name] != has {
			t.Errorf("%s: outputSchema=%v，期望 %v", tool.Name, has, want[tool.Name])
		}
	}
	_ = strings.TrimSpace
}

// send_request：状态/耗时/大小/正文结构化。
func TestStructuredSendRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	cs := connectSelf(t)
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "send_request", Arguments: map[string]any{
		"project": "demo", "method": "GET", "url": srv.URL,
	}})
	if err != nil || res.IsError {
		t.Fatalf("send_request 失败：%v %v", err, res.Content)
	}
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok || sc["status"].(float64) != 200 || sc["size"].(float64) == 0 || sc["saved"] != false {
		t.Fatalf("send_request 结构化不符：%v", res.StructuredContent)
	}
}

// get_docs：批量结构化（单篇失败只在该条带 error，其它照常返回）。
func TestStructuredGetDocs(t *testing.T) {
	cs := connectSelf(t)
	uid := uidAfter(t, mustCall(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "批量结构", "folder": "docs", "content": "# 批量",
	}))
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "get_doc", Arguments: map[string]any{
		"project": "demo", "uids": []string{uid, "00000000-0000-0000-0000-000000000000"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("批量读取不应整体失败：%v", res.Content)
	}
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok || sc["total"].(float64) != 2 || sc["returned"].(float64) != 1 {
		t.Fatalf("get_docs 结构化计数不符：%v", res.StructuredContent)
	}
	docs, _ := sc["docs"].([]any)
	if len(docs) != 2 {
		t.Fatalf("docs 应含 2 条：%v", docs)
	}
	hit := false
	failed := false
	for _, d := range docs {
		m := d.(map[string]any)
		if m["uid"] == uid && m["content"] == "# 批量" && len(m["hash"].(string)) == 64 {
			hit = true
		}
		if m["uid"] == "00000000-0000-0000-0000-000000000000" && strings.Contains(m["error"].(string), "[NOT_FOUND]") {
			failed = true
		}
	}
	if !hit || !failed {
		t.Errorf("get_docs 条目不符：%v", docs)
	}
}

// save_doc_image：结构化去重标记。
func TestStructuredSaveDocImage(t *testing.T) {
	cs := connectSelf(t)
	call := func() map[string]any {
		res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "save_doc_image", Arguments: map[string]any{
			"project": "demo", "data": sdTestPNG, "name": "结构图",
		}})
		if err != nil || res.IsError {
			t.Fatalf("save_doc_image 失败：%v %v", err, res.Content)
		}
		sc, ok := res.StructuredContent.(map[string]any)
		if !ok {
			t.Fatalf("应返回 structuredContent，实际 %T", res.StructuredContent)
		}
		return sc
	}
	first := call()
	second := call()
	if first["sha256"] != second["sha256"] || len(first["sha256"].(string)) != 64 {
		t.Errorf("sha256 应稳定：%v vs %v", first["sha256"], second["sha256"])
	}
	if second["deduped"] != true {
		t.Errorf("同图二次保存应 deduped=true：%v", second)
	}
	if first["markdown"] != "![结构图]("+first["url"].(string)+")" {
		t.Errorf("markdown 片段不符：%v", first)
	}
}

// 错误态也带 structuredContent：{error:{code,message}}。
func TestStructuredErrorResult(t *testing.T) {
	cs := connectSelf(t)
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "get_doc", Arguments: map[string]any{
		"project": "demo", "uid": "00000000-0000-0000-0000-000000000000",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("应失败")
	}
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("错误态应带 structuredContent，实际 %T", res.StructuredContent)
	}
	e, _ := sc["error"].(map[string]any)
	if e["code"] != "NOT_FOUND" || e["message"] == "" {
		t.Errorf("错误结构不符：%v", sc)
	}
}
