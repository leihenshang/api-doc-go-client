package mcp_test

// 长文档分段读取的端到端回归：走内存 MCP server，断言 AI 真正读到的文本。
// 覆盖：区间读取（行号/下一段/hash 不变）、大纲（跳过围栏代码）、越界、
//      search_docs 行号接续、请求说明文档（docsStartLine/docsOutline）。

import (
	"fmt"
	"strings"
	"testing"
)

const rangeDocContent = `# 需求
背景
## 接口
- a
- b
` + "```" + `
# 注释
` + "```" + `
## 验收
完成`

func TestGetDocLineRangeAndOutline(t *testing.T) {
	cs := connectSelf(t)
	callTool(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "长文分段", "content": rangeDocContent,
	})
	docs := mustCall(t, cs, "list_docs", map[string]any{"project": "demo", "query": "长文分段"})
	uid := uidAfter(t, docs)

	// ① 全文（旧行为）：不带行号、不截断，hash 完整 64 位
	full := mustCall(t, cs, "get_doc", map[string]any{"project": "demo", "uid": uid})
	if strings.Contains(full, "1│ ") || strings.Contains(full, "行 1–") {
		t.Errorf("全文读取不应带行号/区间头：\n%s", full)
	}
	if !strings.Contains(full, "# 需求") || !strings.Contains(full, "## 验收") {
		t.Errorf("全文应包含全部正文：\n%s", full)
	}
	hash := fieldAfter(full, "hash ")
	if len(hash) != 64 {
		t.Fatalf("全文应给完整 64 位 hash（区间读取要靠它安全更新），实际 %q", hash)
	}

	// ② 区间读取：行 3–4，带行号与下一段提示；hash 不变（仍可 ifMatch）
	ranged := mustCall(t, cs, "get_doc", map[string]any{
		"project": "demo", "uid": uid, "startLine": 3, "maxLines": 2,
	})
	if !strings.Contains(ranged, "行 3–4 / 共 10 行（下一段 startLine=5）") {
		t.Errorf("区间头应给范围/总量/下一段：\n%s", ranged)
	}
	if !strings.Contains(ranged, "3│ ## 接口") || !strings.Contains(ranged, "4│ - a") {
		t.Errorf("正文应带行号：\n%s", ranged)
	}
	if strings.Contains(ranged, "1│ # 需求") || strings.Contains(ranged, "5│ - b") {
		t.Errorf("区间外不应出现：\n%s", ranged)
	}
	if h := fieldAfter(ranged, "hash "); h != hash {
		t.Errorf("区间读取的 hash 必须是全文 hash：%q != %q", h, hash)
	}

	// ③ 大纲：围栏代码里的 # 不算标题
	outline := mustCall(t, cs, "get_doc", map[string]any{
		"project": "demo", "uid": uid, "outline": true,
	})
	if !strings.Contains(outline, "共 10 行 · 3 个标题") {
		t.Errorf("大纲应跳过围栏代码里的 #：\n%s", outline)
	}
	for _, want := range []string{"L1  # 需求", "L3  ## 接口", "L9  ## 验收"} {
		if !strings.Contains(outline, want) {
			t.Errorf("大纲缺少 %q：\n%s", want, outline)
		}
	}
	if strings.Contains(outline, "注释") {
		t.Errorf("围栏内的 # 注释不应进大纲：\n%s", outline)
	}

	// ④ 越界：明确说明，不报「读取失败」
	bad := mustCall(t, cs, "get_doc", map[string]any{
		"project": "demo", "uid": uid, "startLine": 99,
	})
	if !strings.Contains(bad, "startLine 超出范围") {
		t.Errorf("越界应明确说明：\n%s", bad)
	}
}

func TestSearchDocsLineNumberFeedsGetDocRange(t *testing.T) {
	cs := connectSelf(t)
	lines := []string{"# 标题"}
	for i := 2; i <= 30; i++ {
		if i == 10 {
			lines = append(lines, "needle 所在的第十行")
		} else {
			lines = append(lines, fmt.Sprintf("第 %d 行", i))
		}
	}
	callTool(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "行号接续", "content": strings.Join(lines, "\n"),
	})
	hits := mustCall(t, cs, "search_docs", map[string]any{"project": "demo", "pattern": "needle"})
	if !strings.Contains(hits, "L10") {
		t.Fatalf("search_docs 应给出行号：\n%s", hits)
	}
	// 用搜索给的行号直接精读：应只回这一行
	read := mustCall(t, cs, "get_doc", map[string]any{
		"project": "demo", "uid": uidAfter(t, mustCall(t, cs, "list_docs", map[string]any{"project": "demo", "query": "行号接续"})),
		"startLine": 10, "maxLines": 1,
	})
	if !strings.Contains(read, "行 10–10 / 共 30 行") || !strings.Contains(read, "10│ needle 所在的第十行") {
		t.Errorf("startLine 应精确落到命中行：\n%s", read)
	}
}

func TestGetRequestDetailDocsRangeAndOutline(t *testing.T) {
	cs := connectSelf(t)
	uid := uidOfCreate(t, mustCall(t, cs, "create_request", map[string]any{
		"project": "demo", "name": "带长说明", "method": "GET", "url": "/docs-range",
	}))
	mustCall(t, cs, "update_request", map[string]any{
		"project": "demo", "uid": uid,
		"docs": "# 概述\n第一段\n## 细节\n第二段",
	})

	// 大纲：只取 docs 段时也应支持
	outline := mustCall(t, cs, "get_request_detail", map[string]any{
		"project": "demo", "uid": uid, "sections": []string{"docs"}, "docsOutline": true,
	})
	if !strings.Contains(outline, "L1  # 概述") || !strings.Contains(outline, "L3  ## 细节") {
		t.Errorf("说明文档大纲缺失：\n%s", outline)
	}
	if strings.Contains(outline, "参数：") {
		t.Errorf("sections=docs 不应带 overview：\n%s", outline)
	}

	// 区间：行 2–3；身份行与 hash 仍在（可直接 ifMatch 更新）
	ranged := mustCall(t, cs, "get_request_detail", map[string]any{
		"project": "demo", "uid": uid, "sections": []string{"docs"},
		"docsStartLine": 2, "docsMaxLines": 2,
	})
	if !strings.Contains(ranged, "行 2–3 / 共 4 行") {
		t.Errorf("说明文档区间头不符：\n%s", ranged)
	}
	if !strings.Contains(ranged, "2│ 第一段") || !strings.Contains(ranged, "3│ ## 细节") {
		t.Errorf("说明文档区间正文应带行号：\n%s", ranged)
	}
	if !strings.Contains(ranged, "uid "+uid) || len(fieldAfter(ranged, "hash ")) != 64 {
		t.Errorf("区间读取仍应带 uid 与完整 hash：\n%s", ranged)
	}
}

func TestGetDocFullReadHintsLongDoc(t *testing.T) {
	cs := connectSelf(t)
	lines := make([]string, 500)
	for i := range lines {
		lines[i] = fmt.Sprintf("第 %d 行", i+1)
	}
	callTool(t, cs, "create_doc", map[string]any{
		"project": "demo", "name": "很长", "content": strings.Join(lines, "\n"),
	})
	uid := uidAfter(t, mustCall(t, cs, "list_docs", map[string]any{"project": "demo", "query": "很长"}))

	full := mustCall(t, cs, "get_doc", map[string]any{"project": "demo", "uid": uid})
	if !strings.Contains(full, "建议用 startLine+maxLines 分段读取") {
		t.Errorf("超长全文应给分段建议：\n%s", full[:200])
	}
	if !strings.Contains(full, "第 500 行") {
		t.Errorf("建议归建议，正文不能被截断")
	}
}
