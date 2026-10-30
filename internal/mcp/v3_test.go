package mcp_test

// v3 实测回归：weakMatch、空结果 []、method 校验、confirmName、delete_requests。

import (
	"context"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func callV3(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) *sdk.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

func scOf(t *testing.T, res *sdk.CallToolResult) map[string]any {
	t.Helper()
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("应返回 structuredContent，实际 %T", res.StructuredContent)
	}
	return sc
}

func TestSearchWeakMatchFlag(t *testing.T) {
	cs := connectSelf(t)
	mustCall(t, cs, "create_module", map[string]any{"project": "demo", "name": "_w"})
	mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "_t_弱点", "method": "GET", "url": "/w1", "folder": "_w",
	})
	mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "完全无关", "method": "GET", "url": "/w2", "folder": "_w",
	})
	res := callV3(t, cs, "search_requests", map[string]any{"project": "demo", "query": "_t_"})
	sc := scOf(t, res)
	results, _ := sc["results"].([]any)
	if len(results) == 0 {
		t.Fatalf("应有结果：%v", sc)
	}
	for _, it := range results {
		m := it.(map[string]any)
		name := m["name"].(string)
		weak, _ := m["weakMatch"].(bool)
		if name == "_t_弱点" && weak {
			t.Errorf("名称原串命中不应算弱匹配：%v", m)
		}
		if name == "完全无关" && !weak {
			t.Errorf("仅分词/子序列命中应标 weakMatch：%v", m)
		}
	}
}

func TestSearchEmptyResultsIsArray(t *testing.T) {
	cs := connectSelf(t)
	res := callV3(t, cs, "search_requests", map[string]any{"project": "demo", "query": "zzz-绝不存在"})
	sc := scOf(t, res)
	if v, ok := sc["results"]; !ok || v == nil {
		t.Errorf("results 应为 []，不能是 null：%v", sc)
	} else if arr, ok := v.([]any); !ok || len(arr) != 0 {
		t.Errorf("results 应为空数组：%v", v)
	}
}

func TestMethodValidation(t *testing.T) {
	cs := connectSelf(t)
	for _, name := range []string{"create_request", "update_request", "send_request"} {
		args := map[string]any{"project": "demo", "method": "FOO"}
		switch name {
		case "create_request":
			args["name"] = "坏方法"
		case "update_request":
			args["uid"] = "00000000-0000-0000-0000-000000000000"
		case "send_request":
			args["url"] = "http://127.0.0.1/x"
		}
		res := callV3(t, cs, name, args)
		if !res.IsError {
			t.Errorf("%s 的非法 method 应失败：%v", name, res.Content)
			continue
		}
		msg := textOf(res)
		if msg == "" {
			t.Logf("%s content[0] type=%T", name, res.Content[0])
		}
		if !strings.Contains(msg, "[INVALID]") {
			t.Errorf("%s 的非法 method 应 [INVALID]：%q", name, msg)
		}
	}
}

func TestDeleteRequestConfirmAndBatch(t *testing.T) {
	cs := connectSelf(t)
	mustCall(t, cs, "create_module", map[string]any{"project": "demo", "name": "_b"})
	mk := func(name string) (uid, hash string) {
		res := callV3(t, cs, "create_request", map[string]any{
			"project": "demo", "name": name, "method": "GET", "url": "/b", "folder": "_b",
		})
		if res.IsError {
			t.Fatalf("创建失败：%v", res.Content)
		}
		uid = scOf(t, res)["uid"].(string)
		sr := callV3(t, cs, "search_requests", map[string]any{"project": "demo", "query": name})
		for _, it := range scOf(t, sr)["results"].([]any) {
			m := it.(map[string]any)
			if m["uid"] == uid {
				hash, _ = m["hash"].(string)
			}
		}
		return uid, hash
	}
	u1, h1 := mk("批删一")
	u2, h2 := mk("批删二")

	// confirmName 不符 → 拒绝且不删
	if res := callV3(t, cs, "delete_request", map[string]any{
		"project": "demo", "uid": u1, "confirmName": "写错",
	}); !res.IsError || !strings.Contains(textOf(res), "[CONFLICT]") {
		t.Fatalf("confirmName 不符应 [CONFLICT]：%v", res.Content)
	}
	if res := callV3(t, cs, "get_doc", map[string]any{"project": "demo", "uid": u1}); !res.IsError {
		// get_doc 是文档，不该存在；这里只确认 delete 未删——用 search 验证
	}
	sr := callV3(t, cs, "search_requests", map[string]any{"project": "demo", "query": "批删一"})
	if len(scOf(t, sr)["results"].([]any)) == 0 {
		t.Fatalf("confirmName 不符时不应删除")
	}

	// 批量 dryRun 不删
	res := callV3(t, cs, "delete_requests", map[string]any{
		"project": "demo",
		"items":   []map[string]any{{"uid": u1, "ifMatch": h1}, {"uid": u2, "ifMatch": h2}},
		"dryRun":  true,
	})
	sc := scOf(t, res)
	if sc["deleted"] == nil || len(sc["deleted"].([]any)) != 2 || sc["dryRun"] != true {
		t.Fatalf("dryRun 应列出 2 条：%v", sc)
	}
	if len(scOf(t, callV3(t, cs, "search_requests", map[string]any{"project": "demo", "query": "批删"}))["results"].([]any)) != 2 {
		t.Fatalf("dryRun 不应删除")
	}

	// 错 ifMatch 的单条失败，对的一起删
	res = callV3(t, cs, "delete_requests", map[string]any{
		"project": "demo",
		"items":   []map[string]any{{"uid": u1, "ifMatch": "deadbeef"}, {"uid": u2, "ifMatch": h2}},
	})
	sc = scOf(t, res)
	if len(sc["deleted"].([]any)) != 1 || len(sc["failed"].([]any)) != 1 {
		t.Fatalf("批量删除结果不符：%v", sc)
	}
	if len(scOf(t, callV3(t, cs, "search_requests", map[string]any{"project": "demo", "query": "批删"}))["results"].([]any)) != 1 {
		t.Fatalf("应只剩 1 条")
	}
}

// dryRun 收口：create_request / update_request / save+update_response_example 都不落盘。
func TestDryRunWriteTools(t *testing.T) {
	cs := connectSelf(t)
	mustCall(t, cs, "create_module", map[string]any{"project": "demo", "name": "_dry"})

	// create_request dryRun：不产生文件，结构化带 planned
	res := callV3(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "干跑请求", "method": "GET", "url": "/dry", "folder": "_dry", "dryRun": true,
	})
	if res.IsError {
		t.Fatalf("create dryRun 失败：%v", res.Content)
	}
	sc := scOf(t, res)
	if sc["planned"] != "create" || sc["path"] == "" {
		t.Fatalf("create dryRun 结构化不符：%v", sc)
	}
	sr := callV3(t, cs, "search_requests", map[string]any{"project": "demo", "query": "干跑请求"})
	if len(scOf(t, sr)["results"].([]any)) != 0 {
		t.Fatalf("create dryRun 不应落盘")
	}

	// 真实创建后 update_request dryRun：url 不变
	cr := callV3(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "待改请求", "method": "GET", "url": "/before", "folder": "_dry",
	})
	uid := scOf(t, cr)["uid"].(string)
	ur := callV3(t, cs, "update_request", map[string]any{
		"project": "demo", "uid": uid, "url": "/after", "dryRun": true,
	})
	if ur.IsError || scOf(t, ur)["planned"] != "update" {
		t.Fatalf("update dryRun 不符：%v", ur.Content)
	}
	d := callV3(t, cs, "get_request_detail", map[string]any{"project": "demo", "uid": uid})
	if scOf(t, d)["url"] != "/before" {
		t.Fatalf("update dryRun 不应写入：%v", scOf(t, d))
	}

	// save_response_example dryRun：示例数不变
	before := len(scOf(t, callV3(t, cs, "get_request_detail", map[string]any{"project": "demo", "uid": uid}))["examples"].([]any))
	sr = callV3(t, cs, "save_response_example", map[string]any{
		"project": "demo", "uid": uid, "name": "干跑示例", "body": `{"x":1}`, "dryRun": true,
	})
	if sr.IsError || scOf(t, sr)["planned"] != "create" || scOf(t, sr)["path"] == "" {
		t.Fatalf("save example dryRun 不符：%v", sr.Content)
	}
	after := len(scOf(t, callV3(t, cs, "get_request_detail", map[string]any{"project": "demo", "uid": uid}))["examples"].([]any))
	if after != before {
		t.Fatalf("save example dryRun 不应落盘：%d → %d", before, after)
	}

	// 真实保存后 update dryRun：状态码不变
	sa := callV3(t, cs, "save_response_example", map[string]any{
		"project": "demo", "uid": uid, "name": "示例", "status": 200, "body": `{"x":1}`,
	})
	exUID := scOf(t, sa)["uid"].(string)
	uu := callV3(t, cs, "update_response_example", map[string]any{
		"project": "demo", "uid": uid, "exampleUid": exUID, "status": 500, "dryRun": true,
	})
	if uu.IsError || scOf(t, uu)["planned"] != "update" {
		t.Fatalf("update example dryRun 不符：%v", uu.Content)
	}
	exs := scOf(t, callV3(t, cs, "get_request_detail", map[string]any{"project": "demo", "uid": uid}))["examples"].([]any)
	if exs[0].(map[string]any)["status"].(float64) != 200 {
		t.Fatalf("update example dryRun 不应写入：%v", exs)
	}
}
