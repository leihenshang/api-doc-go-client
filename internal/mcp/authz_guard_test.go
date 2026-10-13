package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"api-doc-go-client/internal/mcpauth"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// recAuthz 记录授权调用的假授权器。
type recAuthz struct {
	calls int
	res   mcpauth.Result
}

func (r *recAuthz) Authorize(_ context.Context, _ mcpauth.Request) (mcpauth.Result, error) {
	r.calls++
	return r.res, nil
}

func rawReq(t *testing.T, raw string) *mcp.CallToolRequest {
	t.Helper()
	return &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(raw)}}
}

func TestSummarizeArgsRedaction(t *testing.T) {
	req := rawReq(t, `{
		"project": "demo",
		"env": "dev",
		"name": "API_KEY",
		"value": "super-secret-value",
		"secret": true,
		"headers": [{"name":"Authorization","value":"Bearer abc","enabled":true},{"name":"X-Ok","value":"1","enabled":true}],
		"params": [{"name":"access_token","value":"zzz","enabled":true}],
		"body": {"type":"json","raw":"{\"password\":\"hunter2\"}","data":"abc"},
		"curl": "curl -H 'Authorization: Bearer x' http://x",
		"httpToken": "should-be-masked",
		"limit": 10,
		"unknownField": "dropped"
	}`)
	got := summarizeArgs("set_env_var", req)
	for _, leak := range []string{"super-secret-value", "Bearer abc", "hunter2", "zzz", "should-be-masked", "curl http", "dropped"} {
		if strings.Contains(got, leak) {
			t.Errorf("摘要泄露 %q：%s", leak, got)
		}
	}
	for _, must := range []string{"••••••", `"project":"demo"`, "Authorization", "access_token", `"keys"`} {
		if !strings.Contains(got, must) {
			t.Errorf("摘要应包含 %q：%s", must, got)
		}
	}
	// body 只保留类型，raw/data 不进摘要
	if strings.Contains(got, "raw") || strings.Contains(got, `"data"`) {
		t.Errorf("body 的 raw/data 不应出现在摘要：%s", got)
	}
}

func TestSummarizeArgsNonSecretValue(t *testing.T) {
	req := rawReq(t, `{"env":"dev","name":"USERNAME","value":"alice","secret":false}`)
	got := summarizeArgs("set_env_var", req)
	if strings.Contains(got, "alice") {
		t.Errorf("普通变量值也不应展示全文：%s", got)
	}
	if !strings.Contains(got, "<value>") {
		t.Errorf("普通变量应占位 <value>：%s", got)
	}
}

func TestRunGuardedLowSkipsAuthorizer(t *testing.T) {
	rec := &recAuthz{}
	ts := &toolSet{authz: rec}
	meta, _ := MetaOf("list_projects")
	ran := false
	res, _, err := runGuarded(ts, meta, context.Background(), nil, func() (*mcp.CallToolResult, any, error) {
		ran = true
		return &mcp.CallToolResult{}, nil, nil
	})
	_ = res
	if err != nil || !ran || rec.calls != 0 {
		t.Fatalf("low 风险应直接执行、不询问：ran=%v calls=%d err=%v", ran, rec.calls, err)
	}
}

func TestRunGuardedDeny(t *testing.T) {
	ts := &toolSet{authz: &mcpauth.DenyAuthorizer{Reason: "headless-strict"}}
	meta, _ := MetaOf("delete_request")
	ran := false
	res, _, _ := runGuarded(ts, meta, context.Background(),
		rawReq(t, `{"project":"demo","uid":"u1"}`), func() (*mcp.CallToolResult, any, error) {
			ran = true
			return &mcp.CallToolResult{}, nil, nil
		})
	if ran {
		t.Fatal("拒绝后不得执行业务")
	}
	if !res.IsError || !strings.Contains(textOf(res), "拒绝") {
		t.Fatalf("应为含「拒绝」的工具错误：%+v", res)
	}
}

func TestRunGuardedRecover(t *testing.T) {
	ts := &toolSet{}
	meta, _ := MetaOf("create_request")
	res, _, err := runGuarded(ts, meta, context.Background(), nil, func() (*mcp.CallToolResult, any, error) {
		panic("boom")
	})
	if err != nil {
		t.Fatalf("panic 必须被 recover，不返回 Go error：%v", err)
	}
	if !res.IsError || !strings.Contains(textOf(res), "内部错误") {
		t.Fatalf("panic 应转内部错误工具结果：%+v", res)
	}
}

// textOf 取工具结果文本（测试辅助）。
func textOf(res *mcp.CallToolResult) string {
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}
