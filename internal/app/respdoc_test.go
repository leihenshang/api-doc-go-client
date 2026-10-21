package app

import (
	"strings"
	"testing"

	"api-doc-go-client/internal/collection"
)

// UpdateResponseDoc 把响应字段表与样例写进请求模型并落盘（文件即真相源，随后可同步）。
func TestUpdateResponseDoc(t *testing.T) {
	core, _ := newTestApp(t)
	r, err := core.CreateRequest("", "登录", "POST")
	if err != nil {
		t.Fatalf("建请求失败: %v", err)
	}

	in := ResponseDocInput{
		UID:    r.UID,
		Type:   "object",
		Sample: `{"code":0}`,
		// 响应内容类型也必须随更新回传落盘，否则编辑字段/数据时会被重置回 json
		ContentType: "xml",
		Params: []collection.ResponseParam{
			{FieldName: "code", Type: "number", Description: "业务码", Example: "0"},
			{FieldName: "data[].id", Type: "string", Description: "ID", Example: "x1"},
		},
		// item 8：更新字段时一并留存响应快照（状态码 + 当时的响应头）
		Status:  201,
		Headers: []collection.KV{{Name: "Content-Type", Value: "application/json", Enabled: true}},
	}
	if err := core.UpdateResponseDoc(in); err != nil {
		t.Fatalf("写响应文档失败: %v", err)
	}

	got, err := core.ReadRequest(r.UID)
	if err != nil {
		t.Fatalf("重读失败: %v", err)
	}
	if got.RespDoc == nil {
		t.Fatal("RespDoc 应已落盘")
	}
	if got.RespDoc.Type != "object" || got.RespDoc.Sample != `{"code":0}` {
		t.Errorf("类型/样例落盘不符: %+v", got.RespDoc)
	}
	if got.RespDoc.ContentType != "xml" {
		t.Errorf("内容类型未随 respdoc 落盘（编辑时会被重置）: %q", got.RespDoc.ContentType)
	}
	if len(got.RespDoc.Params) != 2 || got.RespDoc.Params[1].FieldName != "data[].id" {
		t.Errorf("返回参数行不符: %+v", got.RespDoc.Params)
	}
	if got.RespDoc.Status != 201 || len(got.RespDoc.Headers) != 1 || got.RespDoc.Headers[0].Name != "Content-Type" {
		t.Errorf("响应快照（状态码/响应头）未随 respdoc 落盘: %+v", got.RespDoc)
	}
	// 同步 extra 应带响应侧字段（respdoc → retType/retParams/retSample）
	extra := collection.SyncExtra(got)
	for _, key := range []string{`"retType"`, `"retParams"`, `"retSample"`, `"retContentType"`} {
		if !strings.Contains(extra, key) {
			t.Errorf("SyncExtra 缺少 %s: %s", key, extra)
		}
	}

	// 全空入参清空 RespDoc
	if err := core.UpdateResponseDoc(ResponseDocInput{UID: r.UID}); err != nil {
		t.Fatalf("清空响应文档失败: %v", err)
	}
	got2, err := core.ReadRequest(r.UID)
	if err != nil {
		t.Fatalf("重读失败: %v", err)
	}
	if got2.RespDoc != nil {
		t.Fatalf("全空应清空 RespDoc，实际: %+v", got2.RespDoc)
	}
}

// gRPC 请求不承载响应文档：写入应被拒绝，避免静默丢失。
func TestUpdateResponseDocRejectGRPC(t *testing.T) {
	core, _ := newTestApp(t)
	g := &collection.Request{
		UID: "g-1", Name: "SayHello", Path: "SayHello.yml", Method: "GRPC",
		GRPC: &collection.GrpcBlock{Target: "127.0.0.1:1", Service: "demo.Greeter", Method: "SayHello"},
	}
	if err := core.SaveRequest(g); err != nil {
		t.Fatalf("存 gRPC 请求失败: %v", err)
	}
	err := core.UpdateResponseDoc(ResponseDocInput{UID: g.UID, Type: "object"})
	if err == nil || !strings.Contains(err.Error(), "gRPC") {
		t.Fatalf("gRPC 请求应拒绝响应文档写入，得到: %v", err)
	}
}
