package mcp_test

// MCP 优化清单（doc/mcp/mcp优化清单20261028.md）的端到端回归：
// 全部通过内存 server 调工具，断言「AI 真正读到的文本」，而不是绕过渲染层的结构体断言。

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// T1：exactMatch 整词过滤 + fields 限定 + snippet。
func TestSearchRequestsExactFieldsAndSnippet(t *testing.T) {
	cs := connectSelf(t)
	// A：正文真含长标识符；B：只有拆开的 ministry / reporting 片段（模糊模式容易误报）
	callTool(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "上报单位", "method": "GET",
		"url": "{{host}}/ministryreportingunits/list", "docs": "部侧上报单位列表",
	})
	callTool(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "部侧", "method": "GET",
		"url": "{{host}}/ministry/list", "docs": "reporting units 说明",
	})

	fuzzy, _ := callTool(t, cs, "search_requests", map[string]any{"project": "demo", "query": "ministryreportingunits"})
	exact, isErr := callTool(t, cs, "search_requests", map[string]any{
		"project": "demo", "query": "ministryreportingunits", "exactMatch": true,
	})
	if isErr {
		t.Fatalf("exact 搜索失败：%s", exact)
	}
	if !strings.Contains(exact, "整串匹配") {
		t.Errorf("exact 模式应在结果里标注：%s", exact)
	}
	if !strings.Contains(exact, "上报单位") {
		t.Errorf("整串匹配应命中真实包含该标识符的请求：\n%s", exact)
	}
	if strings.Contains(exact, "[GET] 部侧") {
		t.Errorf("整串匹配不应命中只有片段的请求：\n%s", exact)
	}
	if !strings.Contains(exact, "↳") || !strings.Contains(exact, "ministryreportingunits") {
		t.Errorf("命中应带 snippet（含命中片段）：\n%s", exact)
	}
	if !strings.Contains(fuzzy, "命中") {
		t.Errorf("模糊模式应返回结果：%s", fuzzy)
	}
	if strings.Contains(fuzzy, " · hash ") {
		t.Errorf("默认不应带 hash（省体积）：\n%s", fuzzy)
	}
	withHash, _ := callTool(t, cs, "search_requests", map[string]any{
		"project": "demo", "query": "ministryreportingunits", "exactMatch": true, "includeHash": true,
	})
	if !strings.Contains(withHash, " · hash ") {
		t.Errorf("includeHash=true 时每条应附完整 hash：\n%s", withHash)
	}

	// fields 只搜 docs：按说明命中的请求应出现，按 URL 的片段不应出现
	docsOnly, _ := callTool(t, cs, "search_requests", map[string]any{
		"project": "demo", "query": "部侧上报", "fields": []string{"docs"},
	})
	if !strings.Contains(docsOnly, "上报单位") {
		t.Errorf("docs 字段应命中说明文本：\n%s", docsOnly)
	}

	// 未知 fields 直接报错（不静默忽略）
	bad, isErr := callTool(t, cs, "search_requests", map[string]any{
		"project": "demo", "query": "x", "fields": []string{"bogus"},
	})
	if !isErr || !strings.Contains(bad, "可选值") {
		t.Errorf("未知 fields 应报错并列出可选值：%s", bad)
	}
}

// T2：文档正文搜索（行号 + 片段 + 整词）。
func TestSearchDocsTool(t *testing.T) {
	cs := connectSelf(t)
	created, isErr := callTool(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "部署说明",
		"content": "# 部署\n\n第一段：minstry 拼写错误\n第二段：ministryreportingunits 正确出现\n",
	})
	if isErr {
		t.Fatalf("create_doc 失败：%s", created)
	}
	out, isErr := callTool(t, cs, "search_docs", map[string]any{"project": "demo", "pattern": "ministryreportingunits"})
	if isErr || !strings.Contains(out, "部署说明") || !strings.Contains(out, "L4") {
		t.Fatalf("文档搜索应返回文档、行号与片段：\n%s", out)
	}
	if !strings.Contains(out, "uid ") {
		t.Errorf("结果应带 uid：\n%s", out)
	}
	// 整词：片段里的单词边界检查（minstry 不该被当成 ministryreportingunits）
	exact, _ := callTool(t, cs, "search_docs", map[string]any{
		"project": "demo", "pattern": "ministryreportingunits", "exactMatch": true,
	})
	if strings.Contains(exact, "L3") {
		t.Errorf("整词模式不应命中拼写错误那行：\n%s", exact)
	}
	if !strings.Contains(exact, "L4") {
		t.Errorf("整词模式应命中正确那行：\n%s", exact)
	}
	miss, _ := callTool(t, cs, "search_docs", map[string]any{"project": "demo", "pattern": "不存在的词xyz"})
	if !strings.Contains(miss, "没有文档包含") {
		t.Errorf("无命中应给明确提示：%s", miss)
	}
}

// T3 + T5：sections 分段（体积收敛）、未知分段报错、hash 为完整值。
func TestDetailSectionsAndFullHash(t *testing.T) {
	cs := connectSelf(t)
	created, _ := callTool(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "分段", "method": "POST", "url": "/x",
		"summary": "一句话概述", "docs": "说明正文第一行",
		"bodyType": "json", "bodyRaw": `{"a":1}`,
		"bodyFields": []map[string]any{{"name": "a", "type": "number", "required": true, "example": "1"}},
	})
	uid := uidOfCreate(t, created)

	full, isErr := callTool(t, cs, "get_request_detail", map[string]any{"project": "demo", "uid": uid})
	if isErr {
		t.Fatalf("detail 失败：%s", full)
	}
	if !strings.Contains(full, "请求体字段") || !strings.Contains(full, "概述：一句话概述") {
		t.Errorf("不传 sections 应保持全量：\n%s", full)
	}
	// hash 必须是完整 64 位（或至少不带省略号）：可直接回传 ifMatch
	hash := fieldAfter(full, "hash ")
	if len(hash) < 32 || strings.Contains(hash, "…") {
		t.Errorf("hash 应为完整值：%q", hash)
	}

	docsOnly, isErr := callTool(t, cs, "get_request_detail", map[string]any{
		"project": "demo", "uid": uid, "sections": []string{"docs"},
	})
	if isErr {
		t.Fatalf("sections=docs 失败：%s", docsOnly)
	}
	if strings.Contains(docsOnly, "概述：") || strings.Contains(docsOnly, "请求体字段（") {
		t.Errorf("sections=docs 不应带其它分段：\n%s", docsOnly)
	}
	if !strings.Contains(docsOnly, "说明正文第一行") {
		t.Errorf("docs 段应含说明正文：\n%s", docsOnly)
	}
	if len(docsOnly) >= 3000 {
		t.Errorf("只取 docs 时应远小于 3000 字符，实际 %d", len(docsOnly))
	}

	bad, isErr := callTool(t, cs, "get_request_detail", map[string]any{
		"project": "demo", "uid": uid, "sections": []string{"bogus"},
	})
	if !isErr || !strings.Contains(bad, "可选值") {
		t.Errorf("未知 section 应报错：%s", bad)
	}
}

// T4：批量详情（按 uid 分组、上限 10）。
func TestBatchRequestDetails(t *testing.T) {
	cs := connectSelf(t)
	uid1 := uidOfCreate(t, mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "批一", "method": "GET", "url": "/a",
	}))
	uid2 := uidOfCreate(t, mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "批二", "method": "GET", "url": "/b",
	}))
	out, isErr := callTool(t, cs, "get_request_detail", map[string]any{
		"project": "demo", "uids": []string{uid1, uid2}, "sections": []string{"overview"},
	})
	if isErr {
		t.Fatalf("批量详情失败：%s", out)
	}
	for _, uid := range []string{uid1, uid2} {
		if !strings.Contains(out, "===== "+uid+" =====") {
			t.Errorf("结果缺少 uid %s 分组：\n%s", uid, out)
		}
	}
	if !strings.Contains(out, "成功 2 / 2") {
		t.Errorf("应统计成功数：\n%s", out)
	}

	tooMany := make([]string, 11)
	for i := range tooMany {
		tooMany[i] = "uid"
	}
	bad, isErr := callTool(t, cs, "get_request_detail", map[string]any{"project": "demo", "uids": tooMany})
	if !isErr || !strings.Contains(bad, "最多 10 个") {
		t.Errorf("超过 10 个应直接报错：%s", bad)
	}
}

// T6 + T7 + T8：文档局部替换、落盘确认、ifMatch 冲突给出当前 hash。
func TestUpdateDocReplacesPreviewAndConflictHash(t *testing.T) {
	cs := connectSelf(t)
	callTool(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "局部改",
		"content": "第一行保持\n第二行：旧值\n第三行保持\n",
	})
	docs, _ := callTool(t, cs, "list_docs", map[string]any{"project": "demo", "query": "局部改"})
	uid := uidAfter(t, docs)

	out, isErr := callTool(t, cs, "update_doc", map[string]any{
		"project": "demo", "uid": uid,
		"replaces": []map[string]any{{"old": "旧值", "new": "新值"}},
	})
	if isErr {
		t.Fatalf("replaces 更新失败：%s", out)
	}
	if !strings.Contains(out, "落盘确认") || !strings.Contains(out, "新值") {
		t.Errorf("更新响应应带落盘确认与前几行：\n%s", out)
	}
	if !strings.Contains(out, "第 2–2 行") {
		t.Errorf("替换第 2 行时应标注改动行：\n%s", out)
	}
	after, _ := callTool(t, cs, "get_doc", map[string]any{"project": "demo", "uid": uid})
	if !strings.Contains(after, "新值") || !strings.Contains(after, "第一行保持") || !strings.Contains(after, "第三行保持") {
		t.Errorf("局部替换后其它行应逐字保留：\n%s", after)
	}

	// old 找不到：整单拒绝、内容不变
	bad, isErr := callTool(t, cs, "update_doc", map[string]any{
		"project": "demo", "uid": uid,
		"replaces": []map[string]any{{"old": "不存在的片段", "new": "x"}},
	})
	if !isErr || !strings.Contains(bad, "未匹配") {
		t.Errorf("old 找不到应报错：%s", bad)
	}
	still, _ := callTool(t, cs, "get_doc", map[string]any{"project": "demo", "uid": uid})
	if !strings.Contains(still, "新值") || strings.Contains(still, "不存在的片段") {
		t.Errorf("失败不应改动正文：\n%s", still)
	}

	// ifMatch 不匹配：明确报错 + 当前 hash，且不写入
	cur := fieldAfter(still, "hash ")
	conflict, isErr := callTool(t, cs, "update_doc", map[string]any{
		"project": "demo", "uid": uid, "content": "覆盖尝试", "ifMatch": strings.Repeat("0", 64),
	})
	if !isErr || !strings.Contains(conflict, "ifMatch 不匹配") || !strings.Contains(conflict, "当前 hash="+cur) {
		t.Errorf("冲突应给出明确错误与当前 hash：\n%s", conflict)
	}
	if !strings.Contains(conflict, "未写入任何内容") {
		t.Errorf("冲突提示应说明未写入：%s", conflict)
	}
}

// T9：onlyMatched 收敛模块列表。
func TestProjectModulesOnlyMatched(t *testing.T) {
	cs := connectSelf(t)
	callTool(t, cs, "create_module", map[string]any{"project": "demo", "name": "用户"})
	callTool(t, cs, "create_module", map[string]any{"project": "demo", "name": "订单"})
	mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "folder": "用户", "name": "用户列表", "method": "GET", "url": "/users",
	})
	mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "folder": "订单", "name": "订单详情", "method": "GET", "url": "/orders",
	})

	// 默认（带 query）= 只返回有命中的模块；汇总行仍给出模块总数
	def, _ := callTool(t, cs, "get_project_modules", map[string]any{"project": "demo", "query": "用户"})
	if !strings.Contains(def, "用户") {
		t.Errorf("命中的模块应保留：\n%s", def)
	}
	if strings.Contains(def, "- 订单：") {
		t.Errorf("默认应收敛掉 0 命中的模块：\n%s", def)
	}
	if !strings.Contains(def, "共 2 个模块（有命中 1 个）") {
		t.Errorf("汇总行应说明过滤前后数量：\n%s", def)
	}
	// 显式 onlyMatched=false 才看全部模块（旧行为仍可请求）
	all, _ := callTool(t, cs, "get_project_modules", map[string]any{
		"project": "demo", "query": "用户", "onlyMatched": false,
	})
	if !strings.Contains(all, "- 订单：") {
		t.Errorf("onlyMatched=false 应列出全部模块：\n%s", all)
	}
	// 没有 query 时不存在「命中」，不应做过滤
	noQuery, _ := callTool(t, cs, "get_project_modules", map[string]any{"project": "demo"})
	if !strings.Contains(noQuery, "- 订单：") {
		t.Errorf("无 query 时应保留全部模块：\n%s", noQuery)
	}
}

// mustCall 调工具并要求成功，返回文本。
func mustCall(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	text, isErr := callTool(t, cs, name, args)
	if isErr {
		t.Fatalf("%s 失败：%s", name, text)
	}
	return text
}

// fieldAfter 取 `key` 之后到下一个空白/行尾的值（断言 hash 这类单行字段）。
func fieldAfter(text, key string) string {
	i := strings.Index(text, key)
	if i < 0 {
		return ""
	}
	rest := text[i+len(key):]
	end := strings.IndexAny(rest, " \n\t，,）)")
	if end < 0 {
		end = len(rest)
	}
	return strings.TrimSpace(rest[:end])
}

// uidAfter 从文本里取第一个 uid= 后面的值。
func uidAfter(t *testing.T, text string) string {
	t.Helper()
	i := strings.Index(text, "uid")
	if i < 0 {
		t.Fatalf("文本里没有 uid：\n%s", text)
	}
	rest := strings.TrimPrefix(text[i+3:], "=")
	rest = strings.TrimLeft(rest, " ")
	end := strings.IndexAny(rest, " \n\t，,）)")
	if end >= 0 {
		rest = rest[:end]
	}
	v := strings.TrimSpace(rest)
	if v == "" {
		t.Fatalf("文本里没有 uid：\n%s", text)
	}
	return v
}

// T6 + T7（请求侧）：update_request.replaces 只改 docs 的局部，成功响应带落盘确认；
// 与 docs 互斥、old 找不到时整单拒绝。
func TestUpdateRequestReplacesDocs(t *testing.T) {
	cs := connectSelf(t)
	created := mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "说明局部改", "method": "GET", "url": "/r",
		"docs": "第一节：旧文案\n第二节：保持不变\n",
	})
	uid := uidOfCreate(t, created)

	out, isErr := callTool(t, cs, "update_request", map[string]any{
		"project": "demo", "uid": uid,
		"replaces": []map[string]any{{"old": "旧文案", "new": "新文案"}},
	})
	if isErr {
		t.Fatalf("update_request.replaces 失败：%s", out)
	}
	if !strings.Contains(out, "落盘确认") || !strings.Contains(out, "新文案") {
		t.Errorf("成功响应应带说明文档前几行：\n%s", out)
	}
	if !strings.Contains(out, "第 1–1 行") {
		t.Errorf("替换第 1 行时应标注改动行：\n%s", out)
	}
	detail, _ := callTool(t, cs, "get_request_detail", map[string]any{
		"project": "demo", "uid": uid, "sections": []string{"docs"},
	})
	if !strings.Contains(detail, "新文案") || !strings.Contains(detail, "第二节：保持不变") {
		t.Errorf("局部替换后其它段落应保留：\n%s", detail)
	}

	both, isErr := callTool(t, cs, "update_request", map[string]any{
		"project": "demo", "uid": uid, "docs": "整篇", "replaces": []map[string]any{{"old": "x", "new": "y"}},
	})
	if !isErr || !strings.Contains(both, "互斥") {
		t.Errorf("docs 与 replaces 应互斥：%s", both)
	}
	miss, isErr := callTool(t, cs, "update_request", map[string]any{
		"project": "demo", "uid": uid, "section": "docs",
		"replaces": []map[string]any{{"old": "找不到", "new": "y"}},
	})
	if !isErr || !strings.Contains(miss, "未匹配") {
		t.Errorf("old 找不到应报错：%s", miss)
	}
}

// T5/决策 3：搜索结果里拿到的 hash 能直接回传 update_request.ifMatch（否则「带 hash」等于没带）。
func TestSearchIncludeHashUsableAsIfMatch(t *testing.T) {
	cs := connectSelf(t)
	uid := uidOfCreate(t, mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "哈希直传", "method": "GET", "url": "/hash-demo",
	}))
	out := mustCall(t, cs, "search_requests", map[string]any{
		"project": "demo", "query": "哈希直传", "includeHash": true,
	})
	hash := fieldAfter(out, "· hash ")
	if len(hash) != 64 {
		t.Fatalf("includeHash 应给完整 64 位 hash，实际 %q（输出：\n%s）", hash, out)
	}
	// 用搜索结果里的 hash 直接更新，应成功
	mustCall(t, cs, "update_request", map[string]any{
		"project": "demo", "uid": uid, "url": "/hash-demo-changed", "ifMatch": hash,
	})
	detail, _ := callTool(t, cs, "get_request_detail", map[string]any{
		"project": "demo", "uid": uid, "sections": []string{"overview"},
	})
	if !strings.Contains(detail, "/hash-demo-changed") {
		t.Errorf("带搜索结果的 hash 更新应成功：\n%s", detail)
	}
}

// T8（请求侧）：ifMatch 不匹配 → 明确报错 + 当前 hash + 未写入。
func TestUpdateRequestConflictShowsCurrentHash(t *testing.T) {
	cs := connectSelf(t)
	uid := uidOfCreate(t, mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "请求冲突", "method": "GET", "url": "/keep",
	}))
	before := mustCall(t, cs, "get_request_detail", map[string]any{
		"project": "demo", "uid": uid, "sections": []string{"overview"},
	})
	cur := fieldAfter(before, "hash ")
	bad, isErr := callTool(t, cs, "update_request", map[string]any{
		"project": "demo", "uid": uid, "url": "/should-not-write", "ifMatch": strings.Repeat("0", 64),
	})
	if !isErr || !strings.Contains(bad, "ifMatch 不匹配") ||
		!strings.Contains(bad, "当前 hash="+cur) || !strings.Contains(bad, "未写入任何内容") {
		t.Errorf("请求侧冲突应给出明确错误与当前 hash：\n%s", bad)
	}
	after, _ := callTool(t, cs, "get_request_detail", map[string]any{
		"project": "demo", "uid": uid, "sections": []string{"overview"},
	})
	if strings.Contains(after, "/should-not-write") {
		t.Errorf("冲突时不应写入：\n%s", after)
	}
}
