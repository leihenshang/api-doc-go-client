package mcp_test

// AI 易用性回归：断链修复、按 path 定位、批量更新、upsert、追加写入、提示类。
// 每个用例都从「AI 实际会怎么调」出发，断言的是调用方能看到的文本/结构化结果。

import (
	"strings"
	"testing"
)

// 详情里的「相关文档条目」必须带 uid，否则 AI 没法顺着读；docs 段要给出回写入口。
func TestDetailDocsHintsAndRelatedUID(t *testing.T) {
	cs := connectSelf(t)
	uid := uidOfCreate(t, mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "用户列表", "method": "GET", "url": "/users",
		"docs": "# 用户列表\n\n说明正文",
	}))
	mustCall(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "用户列表说明", "content": "# 说明",
	})

	out := mustCall(t, cs, "get_request_detail", map[string]any{
		"project": "demo", "uid": uid, "sections": []string{"docs"},
	})
	if !strings.Contains(out, "update_request.section=docs") {
		t.Errorf("docs 段应给出回写入口：%s", out)
	}
	if !strings.Contains(out, "用户列表说明") || !strings.Contains(out, "uid=") {
		t.Errorf("相关文档条目应带 uid：%s", out)
	}
}

// get_doc / update_doc 支持按 path 定位（带 .md 与不带都认），找不到时给候选。
func TestDocByPath(t *testing.T) {
	cs := connectSelf(t)
	sc := callStructured(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "路径文档", "fileName": "path-guide", "content": "# 路径",
	})
	path, _ := sc["path"].(string)
	if path == "" {
		t.Fatal("create_doc 未返回 path")
	}
	out := mustCall(t, cs, "get_doc", map[string]any{"project": "demo", "path": path})
	if !strings.Contains(out, "路径文档") {
		t.Errorf("按 path 读不到文档：%s", out)
	}
	// 不带 .md 也认
	noExt := strings.TrimSuffix(path, ".md")
	mustCall(t, cs, "update_doc", map[string]any{
		"project": "demo", "path": noExt, "append": "## 追加",
	})
	out = mustCall(t, cs, "get_doc", map[string]any{"project": "demo", "path": path})
	if !strings.Contains(out, "## 追加") {
		t.Errorf("按 path 追加失败：%s", out)
	}
	// uid 与 path 同时给 → [INVALID]
	if text, isErr := callTool(t, cs, "get_doc", map[string]any{
		"project": "demo", "uid": "x", "path": path,
	}); !isErr || !strings.Contains(text, "[INVALID]") {
		t.Errorf("uid 与 path 同时给应报 [INVALID]：%s", text)
	}
	// 找不到 → [NOT_FOUND] + 候选
	text, isErr := callTool(t, cs, "get_doc", map[string]any{"project": "demo", "path": "docs/nope.md"})
	if !isErr || !strings.Contains(text, "[NOT_FOUND]") || !strings.Contains(text, path) {
		t.Errorf("找不到文档应给候选：%s", text)
	}
}

// update_doc 的 append / insertAfter / 互斥 / section 守卫。
func TestUpdateDocContentModes(t *testing.T) {
	cs := connectSelf(t)
	uid := uidAfter(t, mustCall(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "长文", "content": "# 标题\n\n旧段落",
	}))

	mustCall(t, cs, "update_doc", map[string]any{"project": "demo", "uid": uid, "append": "## 追加段落"})
	mustCall(t, cs, "update_doc", map[string]any{
		"project": "demo", "uid": uid,
		"insertAfter": map[string]any{"anchor": "## 追加段落", "text": "补充说明"},
	})
	out := mustCall(t, cs, "get_doc", map[string]any{"project": "demo", "uid": uid})
	if !strings.Contains(out, "补充说明") || !strings.Contains(out, "## 追加段落") {
		t.Errorf("追加/插入结果不符：%s", out)
	}

	// 两种正文变更方式同时给 → 互斥
	if text, isErr := callTool(t, cs, "update_doc", map[string]any{
		"project": "demo", "uid": uid, "content": "x", "append": "y",
	}); !isErr || !strings.Contains(text, "互斥") {
		t.Errorf("content 与 append 应互斥：%s", text)
	}
	// section=meta 却传正文 → 越界
	if text, isErr := callTool(t, cs, "update_doc", map[string]any{
		"project": "demo", "uid": uid, "section": "meta", "content": "x",
	}); !isErr || !strings.Contains(text, "越界字段") {
		t.Errorf("section=meta 应拒绝正文：%s", text)
	}
	// anchor 命中多处 → 拒绝
	mustCall(t, cs, "update_doc", map[string]any{"project": "demo", "uid": uid, "append": "重复"})
	mustCall(t, cs, "update_doc", map[string]any{"project": "demo", "uid": uid, "append": "重复"})
	if text, isErr := callTool(t, cs, "update_doc", map[string]any{
		"project": "demo", "uid": uid,
		"insertAfter": map[string]any{"anchor": "重复", "text": "x"},
	}); !isErr || !strings.Contains(text, "多处") {
		t.Errorf("anchor 命中多处应拒绝：%s", text)
	}
}

// search_docs 的 includeHash 与 search_requests 口径一致。
func TestSearchDocsIncludeHash(t *testing.T) {
	cs := connectSelf(t)
	mustCall(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "搜索目标", "content": "# 唯一关键词QZX",
	})
	out := mustCall(t, cs, "search_docs", map[string]any{
		"project": "demo", "pattern": "唯一关键词QZX", "includeHash": true,
	})
	if !strings.Contains(out, "可直接回传 update_doc.ifMatch") {
		t.Errorf("includeHash 应回填 hash：%s", out)
	}
}

// update_requests 批量：单条失败不影响其它；成功的条目真的落盘。
func TestBulkUpdateRequests(t *testing.T) {
	cs := connectSelf(t)
	u1 := uidOfCreate(t, mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "批量一", "method": "GET", "url": "/a",
	}))
	u2 := uidOfCreate(t, mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "批量二", "method": "GET", "url": "/b",
	}))

	sc := callStructured(t, cs, "update_requests", map[string]any{
		"project": "demo",
		"items": []map[string]any{
			// 故意不传 enabled：默认应为 true（step 1 的回归点）
			{"uid": u1, "summary": "第一张卡片", "paramsUpsert": []map[string]any{{"name": "page", "value": "1"}}},
			{"uid": u2, "summary": "第二张卡片"},
			{"uid": "00000000-0000-0000-0000-000000000000", "summary": "不存在"},
		},
	})
	updated, _ := sc["updated"].([]any)
	failed, _ := sc["failed"].([]any)
	if len(updated) != 2 || len(failed) != 1 {
		t.Fatalf("批量结果不符：updated=%v failed=%v", updated, failed)
	}
	// 失败条目要带稳定错误码（不是只有一段中文）
	if fm, _ := failed[0].(map[string]any); fm["code"] != "NOT_FOUND" {
		t.Errorf("failed[].code 应为 NOT_FOUND：%v", failed[0])
	}
	out := mustCall(t, cs, "get_request_detail", map[string]any{
		"project": "demo", "uid": u1, "sections": []string{"overview"},
	})
	if !strings.Contains(out, "第一张卡片") || !strings.Contains(out, "page = 1") {
		t.Errorf("批量更新未落盘：%s", out)
	}
}

// paramsUpsert / headersUpsert / bodyFieldsUpsert：同名覆盖不重复，新行追加。
func TestUpsertSemantics(t *testing.T) {
	cs := connectSelf(t)
	uid := uidOfCreate(t, mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "upsert 接口", "method": "POST", "url": "/u",
	}))

	mustCall(t, cs, "update_request", map[string]any{
		"project": "demo", "uid": uid,
		"bodyFieldsUpsert": []map[string]any{{"name": "id", "type": "string", "required": true, "description": "用户 ID"}},
	})
	// 第二次只传说明：行内字段级合并，type/required 必须保留（step 5 的回归点）
	mustCall(t, cs, "update_request", map[string]any{
		"project": "demo", "uid": uid,
		"bodyFieldsUpsert": []map[string]any{{"name": "id", "description": "用户 ID（已改）"}},
	})
	out := mustCall(t, cs, "get_request_detail", map[string]any{
		"project": "demo", "uid": uid, "sections": []string{"bodyFields"},
	})
	if strings.Count(out, "| id |") != 1 || !strings.Contains(out, "已改") {
		t.Errorf("bodyFieldsUpsert 应同名覆盖：%s", out)
	}
	if !strings.Contains(out, "| id | 是 | string |") {
		t.Errorf("只改说明不应清掉 type/required：%s", out)
	}

	mustCall(t, cs, "update_request", map[string]any{
		"project": "demo", "uid": uid, "headersUpsert": []map[string]any{{"name": "X-Token", "value": "a"}},
	})
	mustCall(t, cs, "update_request", map[string]any{
		"project": "demo", "uid": uid, "headersUpsert": []map[string]any{{"name": "X-Token", "value": "b"}},
	})
	out = mustCall(t, cs, "get_request_detail", map[string]any{
		"project": "demo", "uid": uid, "sections": []string{"overview"},
	})
	if strings.Count(out, "X-Token:") != 1 || !strings.Contains(out, "X-Token: b") {
		t.Errorf("headersUpsert 应同名覆盖：%s", out)
	}

	// 整表覆盖与 upsert 互斥
	if text, isErr := callTool(t, cs, "update_request", map[string]any{
		"project": "demo", "uid": uid,
		"params":       []map[string]any{{"name": "a", "value": "1", "enabled": true}},
		"paramsUpsert": []map[string]any{{"name": "b", "value": "2", "enabled": true}},
	}); !isErr || !strings.Contains(text, "互斥") {
		t.Errorf("params 与 paramsUpsert 应互斥：%s", text)
	}
}

// 请求体是 json 但字段表为空时给提醒（不阻断写盘）。
func TestFieldHintOnCreate(t *testing.T) {
	cs := connectSelf(t)
	out := mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "提示接口", "method": "POST", "url": "/hint",
		"body": map[string]any{"type": "json", "raw": "{}"},
	})
	if !strings.Contains(out, "body.fields 为空") {
		t.Errorf("空字段表应提醒：%s", out)
	}
	out = mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "有字段", "method": "POST", "url": "/hint2",
		"body":       map[string]any{"type": "json", "raw": "{}"},
		"bodyFields": []map[string]any{{"name": "id", "type": "string"}},
	})
	if strings.Contains(out, "body.fields 为空") {
		t.Errorf("有字段表不应提醒：%s", out)
	}
}

// list_tools 的常见任务速查：无 filter 时输出，带 filter 时省略（避免噪音）。
func TestListToolsCheatSheet(t *testing.T) {
	cs := connectSelf(t)
	out := mustCall(t, cs, "list_tools", map[string]any{})
	if !strings.Contains(out, "常见任务") || !strings.Contains(out, "apidoc://doc-template") {
		t.Errorf("list_tools 应带任务速查：%s", out)
	}
	if !strings.Contains(out, "update_requests") {
		t.Errorf("工具清单应含 update_requests：%s", out)
	}
	filtered := mustCall(t, cs, "list_tools", map[string]any{"filter": "doc"})
	if strings.Contains(filtered, "常见任务") {
		t.Errorf("带 filter 不应输出速查：%s", filtered)
	}
}

// 请求按 path 定位（详情/修改/删除），找不到时列候选。
func TestRequestByPath(t *testing.T) {
	cs := connectSelf(t)
	mustCall(t, cs, "create_module", map[string]any{"project": "demo", "name": "api"})
	uid := uidOfCreate(t, mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "按路径", "method": "GET", "url": "/p", "folder": "api",
	}))

	out := mustCall(t, cs, "get_request_detail", map[string]any{
		"project": "demo", "path": "api/按路径.yml",
	})
	if !strings.Contains(out, "按路径") {
		t.Errorf("按 path 读不到详情：%s", out)
	}
	out = mustCall(t, cs, "get_request_detail", map[string]any{
		"project": "demo", "path": "api/按路径",
	})
	if !strings.Contains(out, uid) {
		t.Errorf("不带 .yml 也应命中：%s", out)
	}

	mustCall(t, cs, "update_request", map[string]any{
		"project": "demo", "path": "api/按路径", "summary": "路径改的概述",
	})
	out = mustCall(t, cs, "get_request_detail", map[string]any{
		"project": "demo", "uid": uid, "sections": []string{"overview"},
	})
	if !strings.Contains(out, "路径改的概述") {
		t.Errorf("update_request 按 path 未生效：%s", out)
	}

	if text, isErr := callTool(t, cs, "delete_request", map[string]any{
		"project": "demo", "path": "api/按路径", "dryRun": true,
	}); isErr || !strings.Contains(text, "dryRun") {
		t.Errorf("delete_request 按 path 预演失败：%s", text)
	}

	// 找不到 → [NOT_FOUND] + 候选
	text, isErr := callTool(t, cs, "get_request_detail", map[string]any{
		"project": "demo", "path": "api/nope.yml",
	})
	if !isErr || !strings.Contains(text, "[NOT_FOUND]") || !strings.Contains(text, "候选") {
		t.Errorf("找不到请求应给候选：%s", text)
	}
}

// insertAfter：anchor 多处时默认拒绝，occurrence 指定第几处可精确插入。
func TestInsertAfterOccurrence(t *testing.T) {
	cs := connectSelf(t)
	uid := uidAfter(t, mustCall(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "多标题", "content": "## 小节\n\n甲\n\n## 小节\n\n乙",
	}))

	text, isErr := callTool(t, cs, "update_doc", map[string]any{
		"project": "demo", "uid": uid,
		"insertAfter": map[string]any{"anchor": "## 小节", "text": "插在第二处"},
	})
	if !isErr || !strings.Contains(text, "occurrence") {
		t.Errorf("未指定 occurrence 且多处命中应拒绝并提示 occurrence：%s", text)
	}

	mustCall(t, cs, "update_doc", map[string]any{
		"project": "demo", "uid": uid,
		"insertAfter": map[string]any{"anchor": "## 小节", "text": "插在第二处", "occurrence": 2},
	})
	out := mustCall(t, cs, "get_doc", map[string]any{"project": "demo", "uid": uid})
	if strings.Index(out, "乙") < strings.Index(out, "插在第二处") {
		t.Errorf("occurrence=2 应插在第二处、乙之前：%s", out)
	}
}
