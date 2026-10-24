package mcp_test

// get_request_detail 是 AI 读接口详情的唯一出口：wrap() 只回 TextContent，没有结构化通道。
// 因此「概述 / 请求体字段表格」必须在**文本渲染**里出现 —— 只断言 RequestDetail 结构体字段会绕过这一层，
// 曾经就是这样漏掉了（QA 用真实 mcpserver 走 stdio 才发现：写得到、读不回）。

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectSelf 复用 startSelfServer 装配的内存 server，返回已连接的客户端。
func connectSelf(t *testing.T) *mcp.ClientSession {
	t.Helper()
	server := startSelfServer(t, false)
	t1, t2 := mcp.NewInMemoryTransports()
	ss, err := server.Connect(context.Background(), t1, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "detail-render-check", Version: "0"}, nil).
		Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// uidOfCreate 从 create_request 的结果文本里取 uid（形如「…（uid=xxx，路径 …）」）。
func uidOfCreate(t *testing.T, text string) string {
	t.Helper()
	const marker = "uid="
	i := strings.Index(text, marker)
	if i < 0 {
		t.Fatalf("create_request 输出里没有 uid：\n%s", text)
	}
	rest := text[i+len(marker):]
	end := strings.IndexAny(rest, "，,）)\n")
	if end < 0 {
		end = len(rest)
	}
	uid := strings.TrimSpace(rest[:end])
	if uid == "" {
		t.Fatalf("解析出的 uid 为空：\n%s", text)
	}
	return uid
}

func TestGetRequestDetailTextShowsSummaryAndBodyFields(t *testing.T) {
	cs := connectSelf(t)
	ctx := context.Background()

	created, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "create_request",
		Arguments: map[string]any{
			"project":  "demo",
			"name":     "详览端到端",
			"method":   "POST",
			"url":      "/api/v1/orders",
			"bodyType": "json",
			"bodyRaw":  `{"uid":"u-42"}`,
			"summary":  "下单接口，创建一笔待支付订单",
			"bodyFields": []map[string]any{
				{"name": "uid", "type": "string", "required": true, "example": "u-42", "description": "用户ID"},
				// schema 要求 name/type/required 都在（与 KV 的 enabled 同口径）：type 传空串 → 输出回落 string
				{"name": "page", "type": "", "required": false},
			},
		},
	})
	if err != nil {
		t.Fatalf("create_request: %v", err)
	}
	if created.IsError {
		t.Fatalf("create_request 报错：\n%s", toolText(t, created))
	}
	uid := uidOfCreate(t, toolText(t, created))

	detail, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_request_detail",
		Arguments: map[string]any{"project": "demo", "uid": uid},
	})
	if err != nil {
		t.Fatalf("get_request_detail: %v", err)
	}
	if detail.IsError {
		t.Fatalf("get_request_detail 报错：\n%s", toolText(t, detail))
	}
	out := toolText(t, detail)
	for _, want := range []string{
		"概述：下单接口，创建一笔待支付订单",
		"请求体字段（2 行，body.fields）",
		"| 字段名 | 必选 | 类型 | 示例值 | 说明 |",
		"| uid | 是 | string | u-42 | 用户ID |",
		"| page | 否 | string |",
		`{"uid":"u-42"}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("get_request_detail 文本缺少 %q（AI 读不到写进去的内容）：\n%s", want, out)
		}
	}

	// update_request 只改 body 原文：概述与表格必须仍在文本输出里（C3 的可读版本）。
	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "update_request",
		Arguments: map[string]any{
			"project": "demo",
			"uid":     uid,
			"body":    map[string]any{"type": "json", "raw": `{"uid":"u-99"}`, "form": []any{}},
		},
	}); err != nil {
		t.Fatalf("update_request: %v", err)
	}
	again, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_request_detail",
		Arguments: map[string]any{"project": "demo", "uid": uid},
	})
	if err != nil {
		t.Fatalf("get_request_detail(第二次): %v", err)
	}
	out2 := toolText(t, again)
	for _, want := range []string{"概述：下单接口", "| uid | 是 | string | u-42 | 用户ID |", `{"uid":"u-99"}`} {
		if !strings.Contains(out2, want) {
			t.Errorf("body 覆盖后详情缺少 %q：\n%s", want, out2)
		}
	}
}
