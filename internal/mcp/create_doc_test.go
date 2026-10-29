package mcp_test

// 批次 1（清单 20261030）：create_doc 冲突语义 / 错误码 / list_docs 分页与 hash。

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

var docHashRe = regexp.MustCompile(`hash [0-9a-f]{64}`)

func TestCreateDocConflictModes(t *testing.T) {
	cs := connectSelf(t)

	first := mustCall(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "冲突文档", "folder": "docs", "content": "A",
	})
	uid := uidAfter(t, first)

	// ① 默认 error：失败并给出 [CONFLICT] + 已有 uid/path
	again, isErr := callTool(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "冲突文档", "folder": "docs", "content": "B",
	})
	if !isErr || !strings.Contains(again, "[CONFLICT]") {
		t.Fatalf("默认应返回 [CONFLICT]：%s", again)
	}
	if !strings.Contains(again, uid) || !strings.Contains(again, "冲突文档.md") {
		t.Errorf("冲突错误应带出已有 uid/path：%s", again)
	}

	// ② suffix：显式放宽才生成 -2
	suffixed := mustCall(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "冲突文档", "folder": "docs", "onConflict": "suffix", "content": "C",
	})
	if !strings.Contains(suffixed, "冲突文档-2.md") {
		t.Errorf("suffix 应生成 -2.md：%s", suffixed)
	}

	// ③ overwrite：复用原 uid，覆盖正文
	over := mustCall(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "冲突文档", "folder": "docs", "onConflict": "overwrite", "content": "D",
	})
	if !strings.Contains(over, "onConflict=overwrite") || !strings.Contains(over, uid) {
		t.Errorf("overwrite 应复用原 uid 并提示：%s", over)
	}
	read := mustCall(t, cs, "get_doc", map[string]any{"project": "demo", "uid": uid})
	if !strings.Contains(read, "D") {
		t.Errorf("overwrite 应覆盖正文：%s", read)
	}

	// ④ uid 幂等：固定 uid 建一次 → 再建冲突 → overwrite 可复用
	u := mustCall(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "固定 UID", "folder": "docs", "uid": "fixed-uid-doc-1",
	})
	if !strings.Contains(u, "fixed-uid-doc-1") {
		t.Fatalf("应使用指定 uid：%s", u)
	}
	if out, isErr := callTool(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "固定 UID", "folder": "docs", "uid": "fixed-uid-doc-1",
	}); !isErr || !strings.Contains(out, "[CONFLICT]") {
		t.Errorf("同 uid 应冲突：%s", out)
	}

	// ⑤ 非法 onConflict → [INVALID]
	if out, isErr := callTool(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "x", "onConflict": "merge",
	}); !isErr || !strings.Contains(out, "[INVALID]") {
		t.Errorf("非法 onConflict 应 [INVALID]：%s", out)
	}
}

func TestListDocsPaginationAndHash(t *testing.T) {
	cs := connectSelf(t)
	for _, name := range []string{"甲", "乙", "丙"} {
		mustCall(t, cs, "create_doc", map[string]any{
			"project": "demo", "name": name, "folder": "docs", "content": "# " + name,
		})
	}

	// ① 默认带 hash、默认 200
	all := mustCall(t, cs, "list_docs", map[string]any{"project": "demo"})
	if !strings.Contains(all, "共 3 篇文档（本次返回 3 篇，offset 0）") {
		t.Errorf("首行应给总数与 offset：%s", all)
	}
	if got := len(docHashRe.FindAllString(all, -1)); got != 3 {
		t.Errorf("默认应每篇带 hash，实际 %d：%s", got, all)
	}

	// ② limit=2 → 两条 + 继续提示
	page := mustCall(t, cs, "list_docs", map[string]any{"project": "demo", "limit": 2})
	if !strings.Contains(page, "本次返回 2 篇") || !strings.Contains(page, "offset=2 继续取") {
		t.Errorf("分页首行/续取提示不符：%s", page)
	}

	// ③ offset=2 → 只剩一篇
	tail := mustCall(t, cs, "list_docs", map[string]any{"project": "demo", "limit": 2, "offset": 2})
	if !strings.Contains(tail, "本次返回 1 篇") || strings.Contains(tail, "继续取") {
		t.Errorf("末页不应提示继续：%s", tail)
	}

	// ④ includeHash=false → 不带 hash
	plain := mustCall(t, cs, "list_docs", map[string]any{"project": "demo", "includeHash": false})
	if got := len(docHashRe.FindAllString(plain, -1)); got != 0 {
		t.Errorf("includeHash=false 不应带 hash，实际 %d：%s", got, plain)
	}
	// ⑤ 越界 offset → 空页但总数仍在
	out := mustCall(t, cs, "list_docs", map[string]any{"project": "demo", "offset": 99})
	if !strings.Contains(out, "共 3 篇文档（本次返回 0 篇，offset 99）") {
		t.Errorf("越界应给空页与总数：%s", out)
	}
}

func TestErrorCodes(t *testing.T) {
	cs := connectSelf(t)

	cases := []struct {
		name string
		tool string
		args map[string]any
		want string
	}{
		{"文档不存在", "get_doc", map[string]any{"project": "demo", "uid": "00000000-0000-0000-0000-000000000000"}, "[NOT_FOUND]"},
		{"更新不存在", "update_doc", map[string]any{"project": "demo", "uid": "nope", "name": "x"}, "[NOT_FOUND]"},
		{"项目不存在", "list_docs", map[string]any{"project": "不存在项目"}, "[NOT_FOUND]"},
		{"缺名称", "create_doc", map[string]any{"project": "demo"}, "[INVALID]"},
		{"未知 sections", "get_request_detail", map[string]any{"project": "demo", "uid": "x", "sections": []string{"bogus"}}, "[INVALID]"},
		{"批量超限", "get_request_details", map[string]any{
			"project": "demo",
			"uids":    []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"},
		}, "[LIMIT]"},
	}
	for _, c := range cases {
		out, isErr := callTool(t, cs, c.tool, c.args)
		if !isErr || !strings.Contains(out, c.want) {
			t.Errorf("%s（%s）：期望 %s，实际 isErr=%v out=%s", c.name, c.tool, c.want, isErr, out)
		}
	}

	// ifMatch 冲突 → [CONFLICT]
	uid := uidOfCreate(t, mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "冲突码", "method": "GET", "url": "/c",
	}))
	out, isErr := callTool(t, cs, "update_request", map[string]any{
		"project": "demo", "uid": uid, "url": "/c2", "ifMatch": strings.Repeat("0", 64),
	})
	if !isErr || !strings.Contains(out, "[CONFLICT]") {
		t.Errorf("ifMatch 冲突应 [CONFLICT]：%s", out)
	}
	_ = fmt.Sprintf
}

// T9：文档写工具的 dryRun —— 只算计划，不落盘。
func TestDocDryRun(t *testing.T) {
	cs := connectSelf(t)
	uid := uidAfter(t, mustCall(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "演练", "folder": "docs", "content": "v1",
	}))

	// create：冲突时 dryRun 也报 [CONFLICT]（调用方可先干跑）
	if out, isErr := callTool(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "演练", "folder": "docs", "dryRun": true,
	}); !isErr || !strings.Contains(out, "[CONFLICT]") {
		t.Errorf("dryRun 冲突应 [CONFLICT]：%s", out)
	}
	// create：不冲突 → 给计划，且文档数不变
	before := mustCall(t, cs, "list_docs", map[string]any{"project": "demo"})
	plan := mustCall(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "演练2", "folder": "docs", "content": "x", "dryRun": true,
	})
	if !strings.Contains(plan, "（dryRun：未落盘）将创建") {
		t.Errorf("dryRun 创建应给计划：%s", plan)
	}
	if strings.Contains(plan, "已创建文档") {
		t.Errorf("dryRun 不应同时输出「已创建」（评审反馈的文案自相矛盾）：%s", plan)
	}
	after := mustCall(t, cs, "list_docs", map[string]any{"project": "demo"})
	if before != after {
		t.Errorf("dryRun 不应改变文档列表")
	}

	// update：给改动预览，正文不变
	up := mustCall(t, cs, "update_doc", map[string]any{
		"project": "demo", "uid": uid, "content": "v2", "dryRun": true,
	})
	if !strings.Contains(up, "改动预览") || !strings.Contains(up, "v2") {
		t.Errorf("dryRun 更新应给预览：%s", up)
	}
	if got := mustCall(t, cs, "get_doc", map[string]any{"project": "demo", "uid": uid}); !strings.Contains(got, "v1") {
		t.Errorf("dryRun 不应改正文：%s", got)
	}
	// update：ifMatch 不符 → [CONFLICT]
	if out, isErr := callTool(t, cs, "update_doc", map[string]any{
		"project": "demo", "uid": uid, "content": "v3", "dryRun": true, "ifMatch": strings.Repeat("0", 64),
	}); !isErr || !strings.Contains(out, "[CONFLICT]") {
		t.Errorf("dryRun 也应校验 ifMatch：%s", out)
	}

	// delete：给目标路径，文档仍在
	del := mustCall(t, cs, "delete_doc", map[string]any{"project": "demo", "uid": uid, "dryRun": true})
	if !strings.Contains(del, "将把") || !strings.Contains(del, "演练.md") {
		t.Errorf("dryRun 删除应给目标路径：%s", del)
	}
	if got := mustCall(t, cs, "get_doc", map[string]any{"project": "demo", "uid": uid}); !strings.Contains(got, "v1") {
		t.Errorf("dryRun 删除不应删文档：%s", got)
	}
}
