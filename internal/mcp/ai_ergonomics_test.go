package mcp_test

// AI 易用性回归：断链修复、按 path 定位、批量更新、upsert、追加写入、提示类。
// 每个用例都从「AI 实际会怎么调」出发，断言的是调用方能看到的文本/结构化结果。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// 详情里的「相关文档条目」必须带 uid，否则 AI 没法顺着读；docs 段要给出回写入口。
func TestDetailDocsHints(t *testing.T) {
	cs := connectSelf(t)
	uid := uidOfCreate(t, mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "用户列表", "method": "GET", "url": "/users",
		"docs": "# 用户列表\n\n说明正文",
	}))
	out := mustCall(t, cs, "get_request_detail", map[string]any{
		"project": "demo", "uid": uid, "sections": []string{"docs"},
	})
	if !strings.Contains(out, "update_request.section=docs") {
		t.Errorf("docs 段应给出回写入口：%s", out)
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
			{"uid": u1, "summary": "第一张卡片", "patch": map[string]any{
				"params": []map[string]any{{"name": "page", "value": "1"}},
			}},
			{"uid": u2, "summary": "第二张卡片"},
			{"uid": "00000000-0000-0000-0000-000000000000", "summary": "不存在"},
		},
	})
	updated, _ := sc["updated"].([]any)
	failed, _ := sc["failed"].([]any)
	if len(updated) != 2 || len(failed) != 1 {
		t.Fatalf("批量结果不符：updated=%v failed=%v", updated, failed)
	}
	// 失败条目要带稳定错误码与下标（不是只有一段中文）
	if fm, _ := failed[0].(map[string]any); fm["code"] != "NOT_FOUND" || fm["index"] != float64(2) {
		t.Errorf("failed[] 应带 code=NOT_FOUND 与 index=2：%v", failed[0])
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
		"patch": map[string]any{"bodyFields": []map[string]any{
			{"name": "id", "type": "string", "required": true, "description": "用户 ID"},
		}},
	})
	// 第二次只传说明：行内字段级合并，type/required 必须保留（step 5 的回归点）
	mustCall(t, cs, "update_request", map[string]any{
		"project": "demo", "uid": uid,
		"patch": map[string]any{"bodyFields": []map[string]any{{"name": "id", "description": "用户 ID（已改）"}}},
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
		"project": "demo", "uid": uid,
		"patch": map[string]any{"headers": []map[string]any{{"name": "X-Token", "value": "a"}}},
	})
	mustCall(t, cs, "update_request", map[string]any{
		"project": "demo", "uid": uid,
		"patch": map[string]any{"headers": []map[string]any{{"name": "X-Token", "value": "b"}}},
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
		"params": []map[string]any{{"name": "a", "value": "1", "enabled": true}},
		"patch":  map[string]any{"params": []map[string]any{{"name": "b", "value": "2"}}},
	}); !isErr || !strings.Contains(text, "互斥") {
		t.Errorf("params 与 patch.params 应互斥：%s", text)
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

// get_doc 批量也支持按 paths（与 uids 二选一）。
func TestBatchDocsByPaths(t *testing.T) {
	cs := connectSelf(t)
	sc := callStructured(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "甲文档", "fileName": "doc-a", "content": "# 甲",
	})
	p1, _ := sc["path"].(string)
	sc = callStructured(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "乙文档", "fileName": "doc-b", "content": "# 乙",
	})
	p2, _ := sc["path"].(string)

	out := mustCall(t, cs, "get_doc", map[string]any{"project": "demo", "paths": []string{p1, p2}})
	if !strings.Contains(out, "甲文档") || !strings.Contains(out, "乙文档") || !strings.Contains(out, "成功 2 / 2") {
		t.Errorf("按 paths 批量读取不符：%s", out)
	}
}

// delete_requests：confirm 不匹配整批拒绝；条目支持 path。
func TestDeleteRequestsConfirmAndPath(t *testing.T) {
	cs := connectSelf(t)
	uid := uidOfCreate(t, mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "待删请求", "method": "GET", "url": "/del",
	}))
	sc := callStructured(t, cs, "get_request_detail", map[string]any{
		"project": "demo", "uid": uid, "sections": []string{"overview"},
	})
	hash, _ := sc["hash"].(string)
	reqPath, _ := sc["path"].(string)

	if text, isErr := callTool(t, cs, "delete_requests", map[string]any{
		"project": "demo", "confirm": "错的项目名",
		"items": []map[string]any{{"uid": uid, "ifMatch": hash}}, "dryRun": true,
	}); !isErr || !strings.Contains(text, "confirm") {
		t.Errorf("confirm 不匹配应整批拒绝：%s", text)
	}

	out := mustCall(t, cs, "delete_requests", map[string]any{
		"project": "demo", "confirm": "demo",
		"items": []map[string]any{{"path": reqPath, "ifMatch": hash}}, "dryRun": true,
	})
	if !strings.Contains(out, "1/1") {
		t.Errorf("按 path 的批量预演不符：%s", out)
	}
}

// insertAfter.anchorHeading：插到该小节末尾、下一个同级标题之前。
func TestInsertAfterHeading(t *testing.T) {
	cs := connectSelf(t)
	uid := uidAfter(t, mustCall(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "标题插入",
		"content": "## 附录\n\n旧内容\n\n## 其它\n\n别的内容",
	}))
	mustCall(t, cs, "update_doc", map[string]any{
		"project": "demo", "uid": uid,
		"insertAfter": map[string]any{"anchorHeading": "## 附录", "text": "新增一行"},
	})
	out := mustCall(t, cs, "get_doc", map[string]any{"project": "demo", "uid": uid})
	if !strings.Contains(out, "新增一行") {
		t.Fatalf("anchorHeading 未插入：%s", out)
	}
	if strings.Index(out, "新增一行") > strings.Index(out, "## 其它") {
		t.Errorf("应插在「## 其它」之前：%s", out)
	}
}

// 静态 resource：模板 / 错误码 / 集合结构契约。
func TestStaticResources(t *testing.T) {
	cs := connectSelf(t)
	res, err := cs.ListResources(context.Background(), &sdk.ListResourcesParams{})
	if err != nil {
		t.Fatal(err)
	}
	uris := map[string]bool{}
	for _, r := range res.Resources {
		uris[r.URI] = true
	}
	for _, u := range []string{"apidoc://doc-template", "apidoc://error-codes", "apidoc://collection-format"} {
		if !uris[u] {
			t.Errorf("缺少 resource %s：%v", u, uris)
		}
	}
	rr, err := cs.ReadResource(context.Background(), &sdk.ReadResourceParams{URI: "apidoc://error-codes"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rr.Contents) == 0 || !strings.Contains(rr.Contents[0].Text, "NOT_FOUND") {
		t.Errorf("错误码 resource 内容不符：%+v", rr.Contents)
	}
}

// update_requests 的 items 支持按 path 定位；成功条目回填解析后的 uid。
func TestUpdateRequestsByPath(t *testing.T) {
	cs := connectSelf(t)
	mustCall(t, cs, "create_module", map[string]any{"project": "demo", "name": "api"})
	uid := uidOfCreate(t, mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "folder": "api", "name": "按路径批改", "method": "GET", "url": "/bp",
	}))

	sc := callStructured(t, cs, "update_requests", map[string]any{
		"project": "demo",
		"items": []map[string]any{
			{"path": "api/按路径批改.yml", "summary": "批量按路径"},
			{"path": "api/nope.yml", "summary": "不存在"},
		},
	})
	updated, _ := sc["updated"].([]any)
	failed, _ := sc["failed"].([]any)
	if len(updated) != 1 || updated[0] != uid {
		t.Fatalf("按 path 批量应回填解析后的 uid：updated=%v", updated)
	}
	if len(failed) != 1 {
		t.Fatalf("失败条目数不符：%v", failed)
	}
	if fm, _ := failed[0].(map[string]any); fm["path"] != "api/nope.yml" || fm["code"] != "NOT_FOUND" {
		t.Errorf("失败条目应带 path 与 code：%v", failed[0])
	}

	out := mustCall(t, cs, "get_request_detail", map[string]any{
		"project": "demo", "uid": uid, "sections": []string{"overview"},
	})
	if !strings.Contains(out, "批量按路径") {
		t.Errorf("按 path 的批量修改未落盘：%s", out)
	}
}

// delete_requests 的逐条 confirmName：填错只拒绝该条。
func TestDeleteRequestsConfirmName(t *testing.T) {
	cs := connectSelf(t)
	uid := uidOfCreate(t, mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "确认删除", "method": "GET", "url": "/cn",
	}))
	sc := callStructured(t, cs, "get_request_detail", map[string]any{
		"project": "demo", "uid": uid, "sections": []string{"overview"},
	})
	hash, _ := sc["hash"].(string)

	bad := callStructured(t, cs, "delete_requests", map[string]any{
		"project": "demo",
		"items":   []map[string]any{{"uid": uid, "ifMatch": hash, "confirmName": "别的名字"}},
		"dryRun":  true,
	})
	failed, _ := bad["failed"].([]any)
	if len(failed) != 1 {
		t.Fatalf("confirmName 不匹配应失败：%v", bad)
	}
	if fm, _ := failed[0].(map[string]any); !strings.Contains(fm["error"].(string), "confirmName") {
		t.Errorf("失败原因应点名 confirmName：%v", failed[0])
	}

	ok := callStructured(t, cs, "delete_requests", map[string]any{
		"project": "demo",
		"items":   []map[string]any{{"uid": uid, "ifMatch": hash, "confirmName": "确认删除"}},
		"dryRun":  true,
	})
	if deleted, _ := ok["deleted"].([]any); len(deleted) != 1 {
		t.Errorf("confirmName 匹配应通过：%v", ok)
	}
}

// anchorHeading 归一化匹配：全角空格、闭合式 ##、尾部 * 都能命中；找不到时列文内标题。
func TestAnchorHeadingNormalized(t *testing.T) {
	cs := connectSelf(t)
	uid := uidAfter(t, mustCall(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "标题容错",
		"content": "## 附录 A\n\n旧内容\n\n## 其它\n\n别的内容",
	}))

	mustCall(t, cs, "update_doc", map[string]any{
		"project": "demo", "uid": uid,
		"insertAfter": map[string]any{"anchorHeading": "##\u3000附录 A ##", "text": "归一化插入"},
	})
	out := mustCall(t, cs, "get_doc", map[string]any{"project": "demo", "uid": uid})
	if !strings.Contains(out, "归一化插入") {
		t.Fatalf("归一化后应命中标题：%s", out)
	}

	text, isErr := callTool(t, cs, "update_doc", map[string]any{
		"project": "demo", "uid": uid,
		"insertAfter": map[string]any{"anchorHeading": "## 不存在", "text": "x"},
	})
	if !isErr || !strings.Contains(text, "文内标题") || !strings.Contains(text, "## 附录 A") {
		t.Errorf("找不到标题时应列出文内标题：%s", text)
	}
}

// 业务错误必须自带稳定错误码：参数/找不到类不能落成 [INTERNAL]。
func TestErrorCodesClassified(t *testing.T) {
	cs := connectSelf(t)
	reqUID := uidOfCreate(t, mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "错误码请求", "method": "GET", "url": "/ec",
	}))
	docUID := uidAfter(t, mustCall(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "错误码文档",
	}))
	mustCall(t, cs, "create_env", map[string]any{"project": "demo", "name": "ecenv"})

	missing := "00000000-0000-0000-0000-000000000000"
	cases := []struct {
		label string
		tool  string
		args  map[string]any
		want  string
	}{
		{"update_doc 非法 section", "update_doc",
			map[string]any{"project": "demo", "uid": docUID, "section": "bogus"}, "[INVALID]"},
		{"update_doc 越界字段", "update_doc",
			map[string]any{"project": "demo", "uid": docUID, "section": "meta", "content": "x"}, "[INVALID]"},
		{"set_env_var 非法变量名", "set_env_var",
			map[string]any{"project": "demo", "env": "ecenv", "name": "1bad", "value": "v"}, "[INVALID]"},
		{"delete_env_var 环境不存在", "delete_env_var",
			map[string]any{"project": "demo", "env": "没这个环境", "name": "X"}, "[NOT_FOUND]"},
		{"update_response_example 示例不存在", "update_response_example",
			map[string]any{"project": "demo", "uid": reqUID, "exampleUid": missing, "name": "x"}, "[NOT_FOUND]"},
		{"send_request 请求不存在", "send_request",
			map[string]any{"project": "demo", "uid": missing}, "[NOT_FOUND]"},
	}
	for _, c := range cases {
		text, isErr := callTool(t, cs, c.tool, c.args)
		if !isErr {
			t.Errorf("%s：应失败，实际成功", c.label)
			continue
		}
		if !strings.Contains(text, c.want) {
			t.Errorf("%s：期望 %s，实际 %s", c.label, c.want, text)
		}
	}
}

// import_collection 的正文超过上限应回 [LIMIT]，而不是吞进内存后 OOM/超时。
func TestImportCollectionSizeLimit(t *testing.T) {
	cs := connectSelf(t)
	big := strings.Repeat("x", (10<<20)+16)
	text, isErr := callTool(t, cs, "import_collection", map[string]any{
		"project": "demo", "format": "apidocgo", "text": big,
	})
	if !isErr || !strings.Contains(text, "[LIMIT]") {
		t.Errorf("超限正文应回 [LIMIT]：%s", text)
	}
}

// section 的参数错误必须是 [INVALID]（不是服务故障 [INTERNAL]）。
func TestSectionErrorsAreInvalid(t *testing.T) {
	cs := connectSelf(t)
	uid := uidOfCreate(t, mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "section 校验", "method": "GET", "url": "/sec",
	}))

	text, isErr := callTool(t, cs, "update_request", map[string]any{
		"project": "demo", "uid": uid, "section": "bogus", "summary": "x",
	})
	if !isErr || !strings.Contains(text, "[INVALID]") {
		t.Errorf("非法 section 值应回 [INVALID]：%s", text)
	}

	text, isErr = callTool(t, cs, "update_request", map[string]any{
		"project": "demo", "uid": uid, "section": "docs", "summary": "越界",
	})
	if !isErr || !strings.Contains(text, "[INVALID]") {
		t.Errorf("section 越界字段应回 [INVALID]：%s", text)
	}

	// 三个节点都应被接受（描述里列的就是这三个），各传自己范围内的字段。
	for _, c := range []struct {
		sec  string
		args map[string]any
	}{
		{"request", map[string]any{"summary": "节点 request"}},
		{"response", map[string]any{"respDoc": map[string]any{"sample": "{\"ok\":true}"}}},
		{"docs", map[string]any{"docs": "# 节点 docs"}},
	} {
		args := map[string]any{"project": "demo", "uid": uid, "section": c.sec}
		for k, v := range c.args {
			args[k] = v
		}
		if text, isErr := callTool(t, cs, "update_request", args); isErr {
			t.Errorf("section=%s 应被接受：%s", c.sec, text)
		}
	}
}

// patch 显式传空数组 = 清空该表（与整表覆盖一致）；不传 = 不动。
func TestPatchEmptyArrayClears(t *testing.T) {
	cs := connectSelf(t)
	uid := uidOfCreate(t, mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "patch 清空", "method": "GET", "url": "/pc",
		"headers": []map[string]any{{"name": "X-Clear", "value": "1"}},
		"params":  []map[string]any{{"name": "pclear", "value": "1"}},
	}))

	// 空数组：应清空；只传 params 时 headers 不受影响。
	callStructured(t, cs, "update_request", map[string]any{
		"project": "demo", "uid": uid,
		"patch": map[string]any{"params": []any{}},
	})
	out := mustCall(t, cs, "get_request_detail", map[string]any{
		"project": "demo", "uid": uid, "sections": []string{"overview"},
	})
	if strings.Contains(out, "pclear") {
		t.Errorf("patch.params=[] 应清空参数表：%s", out)
	}
	if !strings.Contains(out, "X-Clear") {
		t.Errorf("未传的 patch.headers 不该被动：%s", out)
	}
}

// T1：create_request 的目标 folder 不存在必须报错，不能静默建目录。
func TestCreateRequestFolderMustExist(t *testing.T) {
	cs := connectSelf(t)
	text, isErr := callTool(t, cs, "create_request", map[string]any{
		"project": "demo", "folder": "no-such", "name": "x", "method": "GET", "url": "/x",
	})
	if !isErr || !strings.Contains(text, "[NOT_FOUND]") {
		t.Errorf("folder 不存在应回 [NOT_FOUND]：%s", text)
	}
	// dryRun 也不该建
	if _, isErr := callTool(t, cs, "create_request", map[string]any{
		"project": "demo", "folder": "no-such2", "name": "y", "method": "GET", "url": "/y", "dryRun": true,
	}); !isErr {
		t.Error("dryRun 下 folder 不存在也应报错")
	}
}

// T2：create_module 的 name 允许写完整路径（自动拆 parent + 末段）。
func TestCreateModuleSplitsPath(t *testing.T) {
	cs := connectSelf(t)
	mustCall(t, cs, "create_module", map[string]any{"project": "demo", "name": "api"})
	mustCall(t, cs, "create_module", map[string]any{"project": "demo", "name": "api/user"})
	mustCall(t, cs, "create_module", map[string]any{"project": "demo", "name": "api/user/profile"})

	// 上级不存在 → [NOT_FOUND]
	text, isErr := callTool(t, cs, "create_module", map[string]any{"project": "demo", "name": "nope/x"})
	if !isErr || !strings.Contains(text, "[NOT_FOUND]") {
		t.Errorf("上级模块不存在应回 [NOT_FOUND]：%s", text)
	}
	// 末段非法 → [INVALID]
	text, isErr = callTool(t, cs, "create_module", map[string]any{"project": "demo", "name": "api/.."})
	if !isErr || !strings.Contains(text, "[INVALID]") {
		t.Errorf("非法模块名应回 [INVALID]：%s", text)
	}
}

// T3：update_doc 移动分组不存在时报错，且不能先写正文（避免半成功）。
func TestUpdateDocMoveRequiresFolder(t *testing.T) {
	cs := connectSelf(t)
	uid := uidAfter(t, mustCall(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "移动校验", "content": "# 原内容",
	}))

	text, isErr := callTool(t, cs, "update_doc", map[string]any{
		"project": "demo", "uid": uid, "content": "# 新内容", "folder": "no-such",
	})
	if !isErr || !strings.Contains(text, "[NOT_FOUND]") {
		t.Errorf("目标分组不存在应回 [NOT_FOUND]：%s", text)
	}
	out := mustCall(t, cs, "get_doc", map[string]any{"project": "demo", "uid": uid})
	if strings.Contains(out, "# 新内容") {
		t.Error("移动失败时不应写入正文（避免半成功）")
	}
}

// T4：replaces 片段没命中是参数错，应回 [INVALID]。
func TestReplacesMissIsInvalid(t *testing.T) {
	cs := connectSelf(t)
	uid := uidAfter(t, mustCall(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "替换校验", "content": "# 正文",
	}))
	text, isErr := callTool(t, cs, "update_doc", map[string]any{
		"project": "demo", "uid": uid,
		"replaces": []map[string]any{{"old": "不存在的片段", "new": "x"}},
	})
	if !isErr || !strings.Contains(text, "[INVALID]") {
		t.Errorf("replaces 未命中应回 [INVALID]：%s", text)
	}
}

// create_doc：显式 folder 必须存在；不传 folder 时默认 docs/ 仍自动建（默认值不是调用方选的）。
func TestCreateDocFolderMustExist(t *testing.T) {
	cs := connectSelf(t)
	// ① 不传 folder → docs/ 由工具兜底创建
	mustCall(t, cs, "create_doc", map[string]any{"project": "demo", "name": "默认目录"})

	// ② 显式不存在的目录 → [NOT_FOUND]
	text, isErr := callTool(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "落错地方", "folder": "no-such",
	})
	if !isErr || !strings.Contains(text, "[NOT_FOUND]") {
		t.Errorf("显式 folder 不存在应回 [NOT_FOUND]：%s", text)
	}

	// ③ 显式 docs（纯文档目录，没有 folder.yml）→ 应被接受
	if text, isErr := callTool(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "显式文档目录", "folder": "docs",
	}); isErr {
		t.Errorf("已存在的纯文档目录应被接受：%s", text)
	}
}

// Q-2：insertAfter 的入参错误（未匹配 / 命中多处 / 互斥）都必须回 [INVALID]。
func TestInsertAfterErrorsAreInvalid(t *testing.T) {
	cs := connectSelf(t)
	uid := uidAfter(t, mustCall(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "插入校验", "content": "# 标题\n\n唯一段落\n\n重复段\n\n重复段",
	}))

	cases := []struct {
		label string
		args  map[string]any
	}{
		{"anchor 未匹配", map[string]any{"insertAfter": map[string]any{"anchor": "不存在的锚点", "text": "x"}}},
		{"anchor 命中多处", map[string]any{"insertAfter": map[string]any{"anchor": "重复段", "text": "x"}}},
		{"occurrence 越界", map[string]any{"insertAfter": map[string]any{"anchor": "唯一段落", "text": "x", "occurrence": 5}}},
		{"anchorHeading 未匹配", map[string]any{"insertAfter": map[string]any{"anchorHeading": "## 没有这个标题", "text": "x"}}},
		{"content/append 互斥", map[string]any{"content": "a", "append": "b"}},
		{"append 为空", map[string]any{"append": ""}},
	}
	for _, c := range cases {
		args := map[string]any{"project": "demo", "uid": uid}
		for k, v := range c.args {
			args[k] = v
		}
		if text, isErr := callTool(t, cs, "update_doc", args); !isErr || !strings.Contains(text, "[INVALID]") {
			t.Errorf("%s 应回 [INVALID]：%s", c.label, text)
		}
	}
}

// Q-1：relatedDocs 已移除——schema 与结构化输出都不应再出现该字段。
func TestRelatedDocsRemoved(t *testing.T) {
	cs := connectSelf(t)
	uid := uidOfCreate(t, mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "关联字段", "method": "GET", "url": "/rel",
	}))
	mustCall(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "关联字段说明", "content": "# 文档",
	})
	sc := callStructured(t, cs, "get_request_detail", map[string]any{"project": "demo", "uid": uid})
	if _, ok := sc["relatedDocs"]; ok {
		t.Errorf("relatedDocs 已移除，不应再出现在结构化输出：%v", sc)
	}

	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "get_request_detail" {
			continue
		}
		raw, _ := json.Marshal(tool.OutputSchema)
		if strings.Contains(string(raw), "relatedDocs") {
			t.Errorf("outputSchema 仍声明 relatedDocs：%s", raw)
		}
	}
}
