package mcp_test

import (
	. "api-doc-go-client/internal/mcp" //nolint:revive // 与 write_test.go 一致：白盒式调用
	"os"
	"path/filepath"
	"strings"
	"testing"

	"api-doc-go-client/internal/collection"
)

// 本轮 MCP 扩展的回归面：
// ① create_request 携带完整 body（form 行）/settings/respDoc；
// ② update_request 补 params（整表覆盖/清空）/settings/respDoc；
// ③ get_request_detail 露出 respDoc；
// ④ gRPC 请求拒绝 respDoc（与客户端 UpdateResponseDoc 的限制一致）。

func sampleRespDoc() *RespDocInput {
	params := []collection.ResponseParam{
		{FieldName: "code", Type: "number", Required: true, Description: "业务码", Example: "0"},
		{FieldName: "data[].id", Type: "string", Description: "列表项主键"},
	}
	return &RespDocInput{
		Type:   strPtr("object"),
		Params: &params,
		Sample: strPtr(`{"code":0,"data":[]}`),
	}
}

func TestCreateRequestWithBodyFormSettingsRespDoc(t *testing.T) {
	root, svc := setup(t)
	r, err := svc.CreateRequest(CreateRequestInput{
		Project: "demo", Name: "upload", Method: "POST", URL: "http://127.0.0.1/up",
		Body: &BodyInput{Type: strPtr("form"), Form: &[]KVInput{
			{Name: "caption", Value: strPtr("hi"), Enabled: boolPtr(true)},
			{Name: "file", Value: strPtr("/tmp/a.png"), Enabled: boolPtr(true), Type: strPtr("file")},
		}},
		Settings: &collection.RequestSettings{TimeoutSec: 15},
		RespDoc:  sampleRespDoc(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Body.Form) != 2 || r.Body.Form[1].Type != "file" {
		t.Errorf("form 行未生效: %+v", r.Body)
	}

	// 磁盘真相源：respdoc 段与 settings 都应落进文件
	b, err := os.ReadFile(filepath.Join(root, "demo", r.Path))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "respdoc") {
		t.Errorf("文件应含 respdoc 段:\n%s", b)
	}

	// 详情读回：字段表全量（读了能改）
	d, err := svc.GetRequestDetail("demo", r.UID, DetailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if d.RespDoc == nil || d.RespDoc.Type != "object" || len(d.RespDoc.Params) != 2 {
		t.Fatalf("详情未带 respDoc: %+v", d.RespDoc)
	}
	if d.RespDoc.Params[0].Description != "业务码" || !d.RespDoc.Params[0].Required {
		t.Errorf("字段说明/必选未回读: %+v", d.RespDoc.Params[0])
	}
	if d.Settings == nil || d.Settings.TimeoutSec != 15 {
		t.Errorf("settings 未回读: %+v", d.Settings)
	}
}

func TestUpdateRequestParamsSettingsRespDoc(t *testing.T) {
	_, svc := setup(t)
	r, err := svc.CreateRequest(CreateRequestInput{
		Project: "demo", Name: "list", Method: "GET", URL: "http://127.0.0.1/list",
		Params: []KVInput{{Name: "old", Value: strPtr("1"), Enabled: boolPtr(true)}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// params 整表覆盖 + settings + respDoc 一次写入
	up, err := svc.UpdateRequest(UpdateRequestInput{
		Project: "demo", UID: r.UID,
		Params:   []KVInput{{Name: "page", Value: strPtr("1"), Enabled: boolPtr(true), Description: strPtr("页码")}},
		Settings: &collection.RequestSettings{FollowRedirects: boolPtr(false)},
		RespDoc:  sampleRespDoc(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(up.Params) != 1 || up.Params[0].Name != "page" || up.Params[0].Description != "页码" {
		t.Errorf("params 未整表覆盖: %+v", up.Params)
	}
	if up.Settings == nil || up.Settings.FollowRedirects == nil || *up.Settings.FollowRedirects {
		t.Errorf("settings 未生效: %+v", up.Settings)
	}
	if up.RespDoc == nil || len(up.RespDoc.Params) != 2 {
		t.Errorf("respDoc 未写入: %+v", up.RespDoc)
	}

	// 不传不动：只改名不应清空 params / respDoc
	renamed := "list-v2"
	up2, err := svc.UpdateRequest(UpdateRequestInput{Project: "demo", UID: r.UID, Name: &renamed})
	if err != nil {
		t.Fatal(err)
	}
	if len(up2.Params) != 1 || up2.RespDoc == nil {
		t.Errorf("未传字段应保持不动: params=%+v respDoc=%v", up2.Params, up2.RespDoc != nil)
	}

	// 空数组 = 清空
	up3, err := svc.UpdateRequest(UpdateRequestInput{Project: "demo", UID: r.UID, Params: []KVInput{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(up3.Params) != 0 {
		t.Errorf("空数组应清空参数表: %+v", up3.Params)
	}
}

// item 13：respDoc 按子项合并——只传 params 补字段说明时，已有的样例/状态码/响应头不动。
func TestUpdateRespDocMergesPartial(t *testing.T) {
	_, svc := setup(t)
	r, err := svc.CreateRequest(CreateRequestInput{
		Project: "demo", Name: "merge", Method: "GET", URL: "http://127.0.0.1/m",
		RespDoc: sampleRespDoc(), // 带样例的完整文档
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.RespDoc == nil || r.RespDoc.Sample == "" {
		t.Fatalf("前置条要求样例已写: %+v", r.RespDoc)
	}

	// 只写字段表（无 sample/status/headers）
	params := []collection.ResponseParam{{FieldName: "code", Type: "number", Description: "业务码（新说明）"}}
	up, err := svc.UpdateRequest(UpdateRequestInput{Project: "demo", UID: r.UID, RespDoc: &RespDocInput{
		Params: &params,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if up.RespDoc.Sample != `{"code":0,"data":[]}` {
		t.Errorf("缺省样例应保留原值: %q", up.RespDoc.Sample)
	}
	if len(up.RespDoc.Params) != 1 || up.RespDoc.Params[0].Description != "业务码（新说明）" {
		t.Errorf("params 应被覆盖: %+v", up.RespDoc.Params)
	}
	// 回读磁盘：合并结果已落盘
	d, err := svc.GetRequestDetail("demo", r.UID, DetailOptions{IncludeBody: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.RespDoc == nil || d.RespDoc.Sample == "" {
		t.Fatalf("落盘后样例丢失: %+v", d.RespDoc)
	}
}

func TestGRPCRejectsRespDoc(t *testing.T) {
	root, svc := setup(t)
	// 用集合层直接种一个 gRPC 请求（MCP 暂不创建 gRPC，绕开创建路径只测拒绝逻辑）
	c, err := collection.Open(filepath.Join(root, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	g, err := c.CreateRequestFromDraft("", "grpc-A", &collection.Request{
		GRPC: &collection.GrpcBlock{Target: "127.0.0.1:9000", Service: "user.Service", Method: "Ping"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.UpdateRequest(UpdateRequestInput{Project: "demo", UID: g.UID, RespDoc: sampleRespDoc()}); err == nil ||
		!strings.Contains(err.Error(), "gRPC") {
		t.Errorf("update 对 gRPC 写 respDoc 应被拒绝，实际: %v", err)
	}
	if _, err := svc.CreateRequest(CreateRequestInput{
		Project: "demo", Name: "grpc-copy", CopyFromUID: g.UID, RespDoc: sampleRespDoc(),
	}); err == nil || !strings.Contains(err.Error(), "gRPC") {
		t.Errorf("create 复制 gRPC 源写 respDoc 应被拒绝，实际: %v", err)
	}
}
