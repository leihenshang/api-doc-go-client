package mcp_test

// 批次 3（清单 20261030）：列表类工具的 structuredContent + send_request 默认不落示例。

import (
	"context"
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
		"get_project_modules": true, "search_requests": true, "search_docs": true,
		// 写/单条读也补：调用方拿 uid/path/hash 无需二次查询
		"create_doc": true, "update_doc": true, "delete_doc": true, "get_doc": true,
	}
	for _, tool := range tools.Tools {
		has := tool.OutputSchema != nil
		if want[tool.Name] != has {
			t.Errorf("%s: outputSchema=%v，期望 %v", tool.Name, has, want[tool.Name])
		}
	}
	_ = strings.TrimSpace
}
