package mcp

// 批量写的审计摘要：要能看出「改了哪几条」，但不展开每条改了哪些字段。

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSummarizeArgsBatchUIDs(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"project": "demo",
		"items": []any{
			map[string]any{"uid": "u-1", "summary": "a", "headers": []any{map[string]any{"name": "X-Token", "value": "secret"}}},
			map[string]any{"uid": "u-2", "summary": "b"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(raw)}}
	got := summarizeArgs("update_requests", req)

	if !strings.Contains(got, `"count":2`) || !strings.Contains(got, `"uids":["u-1","u-2"]`) {
		t.Errorf("摘要应含条目数与 uid 列表：%s", got)
	}
	if strings.Contains(got, "X-Token") || strings.Contains(got, "secret") {
		t.Errorf("摘要不应展开条目内的字段与值：%s", got)
	}
	if !strings.Contains(got, `"project":"demo"`) {
		t.Errorf("摘要应保留 project：%s", got)
	}
}
