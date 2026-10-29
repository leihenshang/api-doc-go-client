package mcp_test

// 评审 P0-2/P0-3：create_request 冲突策略 + 接口写操作的结构化输出。

import (
	"context"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCreateRequestConflictAndStructured(t *testing.T) {
	cs := connectSelf(t)
	callRaw := func(name string, args map[string]any) *sdk.CallToolResult {
		t.Helper()
		res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return res
	}

	// ① 默认 error：同名第二次 → [CONFLICT]
	r1 := callRaw("create_request", map[string]any{
		"project": "demo", "name": "重名接口", "method": "GET", "url": "/v1", "folder": "_dup",
	})
	if r1.IsError {
		t.Fatalf("首次创建失败：%v", r1.Content)
	}
	sc, ok := r1.StructuredContent.(map[string]any)
	if !ok || sc["uid"] == "" || sc["path"] == "" || sc["method"] != "GET" {
		t.Fatalf("create_request 应返回结构化 uid/path/method：%v", r1.StructuredContent)
	}
	uid1, _ := sc["uid"].(string)

	r2 := callRaw("create_request", map[string]any{
		"project": "demo", "name": "重名接口", "method": "GET", "url": "/v2", "folder": "_dup",
	})
	if !r2.IsError || !strings.Contains(textOf(r2), "[CONFLICT]") || !strings.Contains(textOf(r2), uid1) {
		t.Errorf("重名应 [CONFLICT] 且带已有 uid：%v", r2.Content)
	}

	// ② suffix：允许并存，路径加 -2、uid 不同
	r3 := callRaw("create_request", map[string]any{
		"project": "demo", "name": "重名接口", "method": "GET", "url": "/v2", "folder": "_dup", "onConflict": "suffix",
	})
	if r3.IsError {
		t.Fatalf("suffix 创建失败：%v", r3.Content)
	}
	sc3 := r3.StructuredContent.(map[string]any)
	if sc3["uid"] == uid1 || !strings.Contains(sc3["path"].(string), "-2") {
		t.Errorf("suffix 结果不符：%v", sc3)
	}

	// ③ uid 幂等 + overwrite：同 uid 覆盖，复用 path
	fixed := "bbbbbbbb-1111-2222-3333-444444444444"
	if res := callRaw("create_request", map[string]any{
		"project": "demo", "name": "固定接口", "method": "POST", "url": "/a", "folder": "_dup", "uid": fixed,
	}); res.IsError {
		t.Fatalf("固定 uid 创建失败：%v", res.Content)
	}
	res := callRaw("create_request", map[string]any{
		"project": "demo", "name": "固定接口", "method": "POST", "url": "/b", "folder": "_dup",
		"uid": fixed, "onConflict": "overwrite",
	})
	if res.IsError {
		t.Fatalf("overwrite 失败：%v", res.Content)
	}
	scOV := res.StructuredContent.(map[string]any)
	if scOV["uid"] != fixed || scOV["url"] != "/b" {
		t.Errorf("overwrite 结构化不符：%v", scOV)
	}

	// ④ update / delete 也返回结构化
	up := callRaw("update_request", map[string]any{
		"project": "demo", "uid": fixed, "url": "/c",
	})
	scUp, ok := up.StructuredContent.(map[string]any)
	if up.IsError || !ok || scUp["url"] != "/c" || scUp["uid"] != fixed {
		t.Errorf("update_request 结构化不符：%v %v", up.Content, up.StructuredContent)
	}
	// delete_request 的 ifMatch
	hash := ""
	for _, it := range callRaw("search_requests", map[string]any{"project": "demo", "query": "固定接口"}).StructuredContent.(map[string]any)["results"].([]any) {
		m := it.(map[string]any)
		if m["uid"] == fixed {
			hash, _ = m["hash"].(string)
		}
	}
	if bad := callRaw("delete_request", map[string]any{"project": "demo", "uid": fixed, "ifMatch": "deadbeef"}); !bad.IsError || !strings.Contains(textOf(bad), "[CONFLICT]") {
		t.Errorf("delete_request ifMatch 不符应 [CONFLICT]：%v", bad.Content)
	}
	del := callRaw("delete_request", map[string]any{"project": "demo", "uid": fixed, "ifMatch": hash})
	scDel, ok := del.StructuredContent.(map[string]any)
	if del.IsError || !ok || scDel["uid"] != fixed || scDel["deleted"] != true || scDel["path"] == "" {
		t.Errorf("delete_request 结构化不符：%v %v", del.Content, del.StructuredContent)
	}
}

// textOf 取失败结果的文本（isError 时 StructuredContent 为空）。
func textOf(res *sdk.CallToolResult) string {
	if res == nil || len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(*sdk.TextContent); ok {
		return tc.Text
	}
	return ""
}
