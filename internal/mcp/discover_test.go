package mcp_test

// 外部实测反馈的六项修复的回归：
//   1. list_tools 工具发现（名字 + 一句话 + 必填参数）
//   2. SDK 入参校验错误的中文转述（带参数名与工具名）
//   3. use_workspace 之外的 project 报错会说明「按会话保存、重连/重启会失效」
//   4. create_doc / create_request 的 fileName（ASCII 文件名）
//   5. update_request.folder 移动请求（能力本来就有，只是参数没有描述导致发现不了）
//   6. import_collection 批量导入（含 dryRun）

import (
	"context"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// callStructured 调工具并返回结构化结果（失败即失败）。
func callStructured(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) map[string]any {
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

// catalogByName 把 list_tools 的结果按工具名索引。
func catalogByName(t *testing.T, sc map[string]any) map[string]map[string]any {
	t.Helper()
	items, _ := sc["tools"].([]any)
	out := make(map[string]map[string]any, len(items))
	for _, it := range items {
		m, _ := it.(map[string]any)
		name, _ := m["name"].(string)
		out[name] = m
	}
	return out
}

func TestListToolsCatalog(t *testing.T) {
	cs := connectSelf(t)
	sc := callStructured(t, cs, "list_tools", map[string]any{})
	if got := sc["total"].(float64); got != 36 {
		t.Errorf("list_tools total = %v，期望 36（全模式工具数）", got)
	}
	byName := catalogByName(t, sc)
	if len(byName) == 0 {
		t.Fatal("list_tools 没返回任何工具")
	}
	// 必填参数要真的推导出来（create_doc 的 name 是必填）
	doc, ok := byName["create_doc"]
	if !ok {
		t.Fatal("list_tools 缺 create_doc")
	}
	req, _ := doc["required"].([]any)
	has := false
	for _, r := range req {
		if r == "name" {
			has = true
		}
	}
	if !has {
		t.Errorf("create_doc 的必填参数应含 name，实际 %v", req)
	}
	// 写工具要标出来
	if imp, ok := byName["import_collection"]; !ok {
		t.Error("list_tools 缺 import_collection")
	} else if imp["write"] != true {
		t.Errorf("import_collection 应标记为写工具：%v", imp)
	}

	// filter 只留命中的
	sc = callStructured(t, cs, "list_tools", map[string]any{"filter": "project"})
	filtered := catalogByName(t, sc)
	if len(filtered) == 0 || len(filtered) >= len(byName) {
		t.Fatalf("filter=project 应缩小结果：%d → %d", len(byName), len(filtered))
	}
	for name := range filtered {
		if !strings.Contains(name, "project") {
			// 描述里命中也算，这里只要结果非空且不超集即可
			continue
		}
	}

	// mode=write 只给落盘工具
	sc = callStructured(t, cs, "list_tools", map[string]any{"mode": "write"})
	writes := catalogByName(t, sc)
	if len(writes) == 0 {
		t.Fatal("mode=write 没返回工具")
	}
	for name, m := range writes {
		if m["write"] != true {
			t.Errorf("mode=write 里出现非写工具 %s", name)
		}
	}
	if _, ok := writes["list_projects"]; ok {
		t.Error("mode=write 不应包含只读工具 list_projects")
	}
}

// 缺必填参数时，SDK 给的是英文 "validating ... missing properties"：
// 必须翻成中文并点名参数与工具，否则调用方只能猜。
func TestSchemaErrorIsChinese(t *testing.T) {
	cs := connectSelf(t)
	text, isErr := callTool(t, cs, "create_doc", map[string]any{"project": "demo"})
	if !isErr {
		t.Fatalf("缺 name 应失败：%s", text)
	}
	if !strings.Contains(text, "[INVALID]") {
		t.Errorf("应带 [INVALID] 码：%s", text)
	}
	if !strings.Contains(text, "缺少必填参数：name") {
		t.Errorf("应点名缺少的参数：%s", text)
	}
	if strings.Contains(text, "validating") || strings.Contains(text, "missing properties") {
		t.Errorf("不应把英文校验原文留给调用方：%s", text)
	}
	if !strings.Contains(text, "create_doc") {
		t.Errorf("应说明是哪个工具：%s", text)
	}
}

// 默认目标是会话级的：报错要讲清「重连/重启后需重新选定」，否则调用方只会觉得状态随机失效。
func TestMissingProjectErrorExplainsSession(t *testing.T) {
	cs := connectSelf(t)
	text, isErr := callTool(t, cs, "list_docs", map[string]any{})
	if !isErr {
		t.Fatalf("未选定工作目录时应失败：%s", text)
	}
	for _, want := range []string{"use_workspace", "会话", "重启"} {
		if !strings.Contains(text, want) {
			t.Errorf("报错应包含 %q（说明默认目标何时失效）：%s", want, text)
		}
	}
}

// fileName 让中文显示名也能落 ASCII 文件名；不传时保持既有行为（中文名落中文文件）。
func TestCreateDocAndRequestFileName(t *testing.T) {
	cs := connectSelf(t)

	sc := callStructured(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "项目概要", "fileName": "project-summary", "content": "# 概要",
	})
	if path, _ := sc["path"].(string); !strings.Contains(path, "project-summary") {
		t.Errorf("fileName 未生效：path=%s", path)
	} else if strings.Contains(path, "项目概要") {
		t.Errorf("fileName 应完全取代 name 推导：path=%s", path)
	}

	// 不传 fileName：仍是既有行为（中文名 → 中文文件名）
	sc = callStructured(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "设计说明", "content": "# 设计",
	})
	if path, _ := sc["path"].(string); !strings.Contains(path, "设计说明") {
		t.Errorf("未传 fileName 时应沿用 name 推导：path=%s", path)
	}

	// 请求侧同样生效
	sc = callStructured(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "创建文档", "method": "POST", "url": "/docs", "fileName": "create-doc",
	})
	if path, _ := sc["path"].(string); !strings.Contains(path, "create-doc") {
		t.Errorf("请求的 fileName 未生效：path=%s", path)
	}
}

// folder 是移动请求的入口（能力一直在，只是参数没描述导致 AI 发现不了）。
func TestUpdateRequestFolderMoves(t *testing.T) {
	cs := connectSelf(t)
	mustCall(t, cs, "create_module", map[string]any{"project": "demo", "name": "api"})
	uid := uidAfter(t, mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "用户列表", "method": "GET", "url": "/users",
	}))
	before := callStructured(t, cs, "get_request_detail", map[string]any{"project": "demo", "uid": uid})
	if path, _ := before["path"].(string); strings.Contains(path, "/") {
		t.Fatalf("新建请求应在项目根：path=%s", path)
	}
	after := callStructured(t, cs, "update_request", map[string]any{
		"project": "demo", "uid": uid, "folder": "api",
	})
	if path, _ := after["path"].(string); !strings.HasPrefix(path, "api/") {
		t.Errorf("folder 未把请求移进模块：path=%s", path)
	}
}

// import_collection：dryRun 只解析不落盘，真导入一次落多条。
func TestImportCollectionPostman(t *testing.T) {
	cs := connectSelf(t)
	const pm = `{
	  "info": {"name": "sample", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
	  "item": [
	    {"name": "登录", "request": {"method": "POST", "url": "https://example.com/api/login"}},
	    {"name": "用户组", "item": [
	      {"name": "列表", "request": {"method": "GET", "url": "https://example.com/api/users"}}
	    ]},
	    {"name": "详情", "request": {"method": "GET", "url": "https://example.com/api/users/1"}}
	  ]
	}`

	// 预演：解析出 3 条叶子请求，且不落盘
	dry := callStructured(t, cs, "import_collection", map[string]any{
		"project": "demo", "format": "postman", "text": pm, "dryRun": true,
	})
	if dry["imported"].(float64) != 3 || dry["dryRun"] != true {
		t.Fatalf("预演结果不符：%v", dry)
	}
	drySearch := callStructured(t, cs, "search_requests", map[string]any{"project": "demo", "query": "登录"})
	if items, _ := drySearch["results"].([]any); len(items) != 0 {
		t.Errorf("dryRun 不应落盘：%v", drySearch)
	}

	// 真导入
	run := callStructured(t, cs, "import_collection", map[string]any{
		"project": "demo", "format": "postman", "text": pm,
	})
	if run["imported"].(float64) != 3 {
		t.Fatalf("导入条数不符：%v", run)
	}
	runSearch := callStructured(t, cs, "search_requests", map[string]any{"project": "demo", "query": "登录"})
	items, _ := runSearch["results"].([]any)
	if len(items) == 0 {
		t.Fatalf("导入后应能搜到请求：%v", runSearch)
	}
	first, _ := items[0].(map[string]any)
	if !strings.Contains(first["name"].(string), "登录") {
		t.Errorf("搜到的不是导入的请求：%v", first)
	}

	// 格式不对要 [INVALID]，并说清可选值
	text, isErr := callTool(t, cs, "import_collection", map[string]any{
		"project": "demo", "format": "swagger", "text": "{}",
	})
	if !isErr || !strings.Contains(text, "[INVALID]") {
		t.Errorf("不支持的格式应报 [INVALID]：%s", text)
	}
}
