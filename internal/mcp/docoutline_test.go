package mcp_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// rawCall 调工具并拿回结构化结果（本文件的断言主要看 structuredContent）。
func rawCall(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) *sdk.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("调用 %s: %v", name, err)
	}
	return res
}

// 结构概览：文本给 AI 看结构，结构化输出**不带全文**（否则等于又吞一次全文）。
func TestGetDocOutlineSavesTokens(t *testing.T) {
	cs := connectSelf(t)
	content := strings.Join([]string{
		"# 设计",
		"本文讨论同步源的取舍。",
		"## 背景",
		"现有实现只允许一个同步源。",
		"### 现状",
		"现状细节一。",
		"## 方案",
		"方案概述。",
	}, "\n")
	callTool(t, cs, "create_doc", map[string]any{"project": "demo", "name": "结构概览", "content": content})
	uid := uidAfter(t, mustCall(t, cs, "list_docs", map[string]any{"project": "demo", "query": "结构概览"}))

	raw := rawCall(t, cs, "get_doc", map[string]any{"project": "demo", "uid": uid, "outline": true})
	sc, _ := raw.StructuredContent.(map[string]any)
	if sc == nil {
		t.Fatalf("大纲应返回结构化内容：%v", raw.StructuredContent)
	}
	if sc["kind"] != "outline" {
		t.Errorf("kind 应为 outline：%v", sc["kind"])
	}
	if _, has := sc["content"]; has {
		t.Fatal("大纲模式的结构化输出不能带全文（客户端读结构化等于没省 token）")
	}
	heads, _ := sc["headings"].([]any)
	if len(heads) != 3 {
		t.Fatalf("默认 depth=2 应展开 3 个标题（h1/h2），实际 %d：%v", len(heads), heads)
	}
	if got, _ := sc["omitted"].(float64); got != 1 {
		t.Errorf("h3 应计入 omitted=1，实际 %v", sc["omitted"])
	}

	text := mustCall(t, cs, "get_doc", map[string]any{"project": "demo", "uid": uid, "outline": true})
	for _, want := range []string{"L1  # 设计", "L3  ## 背景", "本文讨论同步源的取舍", "更深层级还有 1 个标题"} {
		if !strings.Contains(text, want) {
			t.Errorf("大纲缺少 %q：\n%s", want, text)
		}
	}

	// depth 放大：h3 也展开
	deep := rawCall(t, cs, "get_doc", map[string]any{"project": "demo", "uid": uid, "outline": true, "depth": 3})
	if got := deep.StructuredContent.(map[string]any)["headings"].([]any); len(got) != 4 {
		t.Errorf("depth=3 应展开 4 个标题，实际 %d", len(got))
	}

	// leads=false：只剩标题，体量更小
	noLead := mustCall(t, cs, "get_doc", map[string]any{"project": "demo", "uid": uid, "outline": true, "leads": false})
	if strings.Contains(noLead, "本文讨论同步源的取舍") {
		t.Errorf("leads=false 不应带首句：\n%s", noLead)
	}
}

// 按标题只读一节：范围到下一个同级/更高级标题之前。
func TestGetDocByHeading(t *testing.T) {
	cs := connectSelf(t)
	content := strings.Join([]string{
		"# 手册",
		"## 安装",
		"步骤一。",
		"### 依赖",
		"需要 go 1.26。",
		"## 配置",
		"配置内容。",
	}, "\n")
	callTool(t, cs, "create_doc", map[string]any{"project": "demo", "name": "按标题读", "content": content})
	uid := uidAfter(t, mustCall(t, cs, "list_docs", map[string]any{"project": "demo", "query": "按标题读"}))

	raw := rawCall(t, cs, "get_doc", map[string]any{"project": "demo", "uid": uid, "heading": "安装"})
	sc, _ := raw.StructuredContent.(map[string]any)
	if sc == nil || sc["kind"] != "range" {
		t.Fatalf("按标题读取应返回 range：%v", raw.StructuredContent)
	}
	body, _ := sc["content"].(string)
	if !strings.Contains(body, "步骤一") || !strings.Contains(body, "需要 go 1.26") {
		t.Errorf("该节应包含正文与子标题：%q", body)
	}
	if strings.Contains(body, "配置内容") {
		t.Errorf("不该带下一节：%q", body)
	}
	if body == content {
		t.Error("按标题读取不能返回全文")
	}

	// 不存在的标题：报错并给出可用标题
	if text, isErr := callTool(t, cs, "get_doc", map[string]any{"project": "demo", "uid": uid, "heading": "部署"}); !isErr || !strings.Contains(text, "没有找到标题") {
		t.Errorf("找不到标题应报错并提示：isErr=%v text=%s", isErr, text)
	}

	// outline 与 heading 互斥
	if text, isErr := callTool(t, cs, "get_doc", map[string]any{"project": "demo", "uid": uid, "heading": "安装", "outline": true}); !isErr {
		t.Errorf("heading 与 outline 同时给应报错：%s", text)
	}
}

// search_docs 命中行带所属标题路径；list_docs withOutline 给标题清单。
func TestSearchAndListDocsOutlineExtras(t *testing.T) {
	cs := connectSelf(t)
	content := strings.Join([]string{
		"# 设计",
		"## 背景",
		"### 现状",
		"这里有个 needle 关键词。",
	}, "\n")
	callTool(t, cs, "create_doc", map[string]any{"project": "demo", "name": "搜索路径", "content": content})

	raw := rawCall(t, cs, "search_docs", map[string]any{"project": "demo", "pattern": "needle"})
	sc, _ := raw.StructuredContent.(map[string]any)
	docs, _ := sc["docs"].([]any)
	if len(docs) == 0 {
		t.Fatalf("应命中一篇：%v", raw.StructuredContent)
	}
	first, _ := docs[0].(map[string]any)
	matches, _ := first["matches"].([]any)
	if len(matches) == 0 {
		t.Fatalf("应有命中行：%v", first)
	}
	m0, _ := matches[0].(map[string]any)
	if got, _ := m0["headingPath"].(string); got != "设计 > 背景 > 现状" {
		t.Errorf("命中行应带标题路径，实际 %q", got)
	}

	listRaw := rawCall(t, cs, "list_docs", map[string]any{"project": "demo", "query": "搜索路径", "withOutline": true})
	listSc, _ := listRaw.StructuredContent.(map[string]any)
	listDocs, _ := listSc["docs"].([]any)
	if len(listDocs) != 1 {
		t.Fatalf("应列出 1 篇：%v", listSc)
	}
	d0, _ := listDocs[0].(map[string]any)
	hs, _ := d0["headings"].([]any)
	if len(hs) != 2 || hs[0] != "# 设计" || hs[1] != "## 背景" {
		t.Errorf("withOutline 应给到 h2 的标题清单，实际 %v", hs)
	}
	// 不带 withOutline 时不返回，避免列表体积膨胀
	plain := rawCall(t, cs, "list_docs", map[string]any{"project": "demo", "query": "搜索路径"})
	p0, _ := plain.StructuredContent.(map[string]any)["docs"].([]any)[0].(map[string]any)
	if _, has := p0["headings"]; has {
		t.Errorf("默认不应带 headings：%v", p0)
	}
}

// 超长文档：首次仍给全文，但末尾追加「先看结构」建议（不截断）。
func TestGetDocLongFullHasHint(t *testing.T) {
	cs := connectSelf(t)
	body := strings.Repeat("这是一段用于压长正文的说明文字。", 500) // >8000 字符
	content := "# 长文\n" + body
	callTool(t, cs, "create_doc", map[string]any{"project": "demo", "name": "超长提示", "content": content})
	uid := uidAfter(t, mustCall(t, cs, "list_docs", map[string]any{"project": "demo", "query": "超长提示"}))

	raw := rawCall(t, cs, "get_doc", map[string]any{"project": "demo", "uid": uid})
	sc, _ := raw.StructuredContent.(map[string]any)
	if sc["kind"] != "full" {
		t.Errorf("默认应是全文：%v", sc["kind"])
	}
	if got, _ := sc["content"].(string); !strings.Contains(got, "这是一段用于压长正文") || len([]rune(got)) < 8000 {
		t.Errorf("全文不能被截断，实际 %d 字", len([]rune(got)))
	}
	if hint, _ := sc["hint"].(string); !strings.Contains(hint, "outline=true") {
		t.Errorf("超长文档应给「先看结构」建议：%v", sc["hint"])
	}
}

// 大纲的结构化输出体积必须远小于全文（防止以后又把全文塞回来）。
func TestGetDocOutlineMuchSmallerThanFull(t *testing.T) {
	cs := connectSelf(t)
	var b strings.Builder
	b.WriteString("# 大文档\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&b, "## 第 %d 节\n", i)
		b.WriteString(strings.Repeat("这一节的正文内容比较长。", 30))
		b.WriteString("（尾部标记）\n")
	}
	content := b.String()
	callTool(t, cs, "create_doc", map[string]any{"project": "demo", "name": "大文档", "content": content})
	uid := uidAfter(t, mustCall(t, cs, "list_docs", map[string]any{"project": "demo", "query": "大文档"}))

	full, _ := callTool(t, cs, "get_doc", map[string]any{"project": "demo", "uid": uid})
	outline, _ := callTool(t, cs, "get_doc", map[string]any{"project": "demo", "uid": uid, "outline": true})
	fullLen := len([]rune(full))
	outlineLen := len([]rune(outline))
	// 40 节 × 每节首句 60 字 ≈ 2.4k，仍应比全文小一个量级；这里守 3 倍，
	// 真正的兜底是"大纲里不能出现正文句子"（下面那条）。
	if outlineLen*3 > fullLen {
		t.Fatalf("大纲应至少小 3 倍：outline=%d full=%d", outlineLen, fullLen)
	}
	// 首句（60 字）本来就会出现在大纲里，所以查「每节末尾才有的标记」：
	// 它能出现说明又整篇塞回来了。
	if strings.Contains(outline, "（尾部标记）") {
		t.Fatal("大纲里不应出现小节末尾的正文（说明又把全文塞回来了）")
	}
	t.Logf("全文 %d 字 → 大纲 %d 字（%.1f 倍）", fullLen, outlineLen, float64(fullLen)/float64(outlineLen))
}
