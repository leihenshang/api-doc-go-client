package mcp_test

// 请求体 / 响应字段文档的**指针合并**语义（本地更新能力改造的核心回归面）。
//
// 契约：入参里为 nil 的子项 = 保留原值；显式零值（"" / 0 / [] / {}）= 清空。
// 与旧的「整份覆盖 + 快照还原」实现相比，这里锁住三件旧实现做不到的事：
// ① 只传 body.raw 不动 form；② 只传 body.form 不动 raw；
// ③ respDoc.sample 传空串能清空、status 传 0 能清零。

import (
	. "api-doc-go-client/internal/mcp" //nolint:revive // 与其它测试一致：白盒式调用
	"testing"

	"api-doc-go-client/internal/collection"
)

// fullBody 一份各子项都非空的请求体（form/fields/descs 齐全，便于验证「不被动到」）。
func fullBody() *BodyInput {
	return &BodyInput{
		Type: strPtr("json"),
		Raw:  strPtr(`{"uid":"u-1"}`),
		Form: &[]KVInput{{Name: "f1", Value: strPtr("v1"), Enabled: boolPtr(true)}},
		Descs: &map[string]string{
			"uid": "用户ID",
		},
		Fields: &[]BodyFieldInput{{Name: "uid", Type: strPtr("string"), Required: boolPtr(true)}},
	}
}

// TestUpdateBodyOnlyRawKeepsForm 只传 body.raw：form / fields / descs 全部保留。
func TestUpdateBodyOnlyRawKeepsForm(t *testing.T) {
	_, svc := setup(t)
	r, err := svc.CreateRequest(CreateRequestInput{
		Project: "demo", Name: "only-raw", Method: "POST", URL: "http://127.0.0.1/a",
		Body: fullBody(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Body.Form) != 1 || len(r.Body.Fields) != 1 || r.Body.Descs["uid"] != "用户ID" {
		t.Fatalf("前置条件：建请求时 body 子项应齐全: %+v", r.Body)
	}

	up, err := svc.UpdateRequest(UpdateRequestInput{
		Project: "demo", UID: r.UID,
		Body: &BodyInput{Raw: strPtr(`{"uid":"u-2"}`)}, // 只动 raw
	})
	if err != nil {
		t.Fatal(err)
	}
	if up.Body.Raw != `{"uid":"u-2"}` {
		t.Fatalf("raw 未生效: %q", up.Body.Raw)
	}
	if len(up.Body.Form) != 1 || up.Body.Form[0].Name != "f1" {
		t.Fatalf("只传 raw 把 form 清掉了: %+v", up.Body.Form)
	}
	if len(up.Body.Fields) != 1 || up.Body.Descs["uid"] != "用户ID" {
		t.Fatalf("只传 raw 把 fields/descs 清掉了: %+v / %+v", up.Body.Fields, up.Body.Descs)
	}
	if up.Body.Type != "json" {
		t.Fatalf("只传 raw 把 type 清掉了: %q", up.Body.Type)
	}
}

// TestUpdateBodyOnlyFormKeepsRaw 只传 body.form：raw / fields / descs 全部保留。
func TestUpdateBodyOnlyFormKeepsRaw(t *testing.T) {
	_, svc := setup(t)
	r, err := svc.CreateRequest(CreateRequestInput{
		Project: "demo", Name: "only-form", Method: "POST", URL: "http://127.0.0.1/b",
		Body: fullBody(),
	})
	if err != nil {
		t.Fatal(err)
	}

	nextForm := []KVInput{
		{Name: "a", Value: strPtr("1"), Enabled: boolPtr(true)},
		{Name: "b", Value: strPtr("2"), Enabled: boolPtr(true), Type: strPtr("file")},
	}
	up, err := svc.UpdateRequest(UpdateRequestInput{
		Project: "demo", UID: r.UID,
		Body: &BodyInput{Form: &nextForm}, // 只动 form
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(up.Body.Form) != 2 || up.Body.Form[1].Type != "file" {
		t.Fatalf("form 未生效: %+v", up.Body.Form)
	}
	if up.Body.Raw != `{"uid":"u-1"}` {
		t.Fatalf("只传 form 把 raw 清掉了: %q", up.Body.Raw)
	}
	if len(up.Body.Fields) != 1 || up.Body.Descs["uid"] != "用户ID" {
		t.Fatalf("只传 form 把 fields/descs 清掉了: %+v / %+v", up.Body.Fields, up.Body.Descs)
	}
}

// TestUpdateBodyExplicitEmptyClears 显式空数组 / 空串才清空（nil 与空值必须区分开）。
func TestUpdateBodyExplicitEmptyClears(t *testing.T) {
	_, svc := setup(t)
	r, err := svc.CreateRequest(CreateRequestInput{
		Project: "demo", Name: "clear-body", Method: "POST", URL: "http://127.0.0.1/c",
		Body: fullBody(),
	})
	if err != nil {
		t.Fatal(err)
	}
	emptyForm := []KVInput{}
	emptyRaw := ""
	up, err := svc.UpdateRequest(UpdateRequestInput{
		Project: "demo", UID: r.UID,
		Body: &BodyInput{Form: &emptyForm, Raw: &emptyRaw},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(up.Body.Form) != 0 || up.Body.Raw != "" {
		t.Fatalf("显式空值应清空 form/raw: form=%+v raw=%q", up.Body.Form, up.Body.Raw)
	}
	if len(up.Body.Fields) != 1 {
		t.Fatalf("清 form/raw 不应连带清掉 fields: %+v", up.Body.Fields)
	}
}

// TestUpdateRespDocOnlyParamsKeepsSample 只传 respDoc.params：sample/type/status/headers 不动。
func TestUpdateRespDocOnlyParamsKeepsSample(t *testing.T) {
	_, svc := setup(t)
	status, headers := 201, []KVInput{{Name: "X-Trace", Value: strPtr("t-1"), Enabled: boolPtr(true)}}
	r, err := svc.CreateRequest(CreateRequestInput{
		Project: "demo", Name: "respdoc-params", Method: "GET", URL: "http://127.0.0.1/d",
		RespDoc: &RespDocInput{
			Type:        strPtr("object"),
			Sample:      strPtr(`{"code":0}`),
			Status:      &status,
			Headers:     &headers,
			ContentType: strPtr("json"),
			Params:      &[]collection.ResponseParam{{FieldName: "code", Type: "number"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.RespDoc == nil || r.RespDoc.Sample != `{"code":0}` || r.RespDoc.Status != 201 {
		t.Fatalf("前置条件：respDoc 应完整落盘: %+v", r.RespDoc)
	}

	up, err := svc.UpdateRequest(UpdateRequestInput{
		Project: "demo", UID: r.UID,
		RespDoc: &RespDocInput{Params: &[]collection.ResponseParam{
			{FieldName: "code", Type: "number", Description: "业务码（改）"},
			{FieldName: "msg", Type: "string"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(up.RespDoc.Params) != 2 || up.RespDoc.Params[0].Description != "业务码（改）" {
		t.Fatalf("params 未覆盖: %+v", up.RespDoc.Params)
	}
	if up.RespDoc.Sample != `{"code":0}` || up.RespDoc.Type != "object" {
		t.Fatalf("只传 params 把 sample/type 清掉了: %+v", up.RespDoc)
	}
	if up.RespDoc.Status != 201 || len(up.RespDoc.Headers) != 1 || up.RespDoc.ContentType != "json" {
		t.Fatalf("只传 params 把 status/headers/contentType 清掉了: %+v", up.RespDoc)
	}
}

// TestUpdateRespDocCanClearSampleAndStatus 旧实现清不掉的两种值：sample="" 与 status=0。
func TestUpdateRespDocCanClearSampleAndStatus(t *testing.T) {
	_, svc := setup(t)
	status := 200
	r, err := svc.CreateRequest(CreateRequestInput{
		Project: "demo", Name: "respdoc-clear", Method: "GET", URL: "http://127.0.0.1/e",
		RespDoc: &RespDocInput{
			Type:   strPtr("object"),
			Sample: strPtr(`{"code":0}`),
			Status: &status,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	emptySample, zeroStatus := "", 0
	up, err := svc.UpdateRequest(UpdateRequestInput{
		Project: "demo", UID: r.UID,
		RespDoc: &RespDocInput{Sample: &emptySample, Status: &zeroStatus},
	})
	if err != nil {
		t.Fatal(err)
	}
	if up.RespDoc.Sample != "" {
		t.Fatalf("respDoc.sample 传空串应能清空，实际 %q", up.RespDoc.Sample)
	}
	if up.RespDoc.Status != 0 {
		t.Fatalf("respDoc.status 传 0 应能清零，实际 %d", up.RespDoc.Status)
	}
	if up.RespDoc.Type != "object" {
		t.Fatalf("清 sample/status 不应连带清掉 type: %+v", up.RespDoc)
	}
	// 落盘也要是空的（不是只改了内存）
	d, err := svc.GetRequestDetail("demo", r.UID, DetailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if d.RespDoc == nil || d.RespDoc.Sample != "" || d.RespDoc.Status != 0 {
		t.Fatalf("清空结果未落盘: %+v", d.RespDoc)
	}
}

// TestCreateRequestBodyPointerMerge create 走同一套合并：bodyType/bodyRaw 只覆盖对应子项，
// body.form 也不会连带把 bodyFields 写进去的表格清掉。
func TestCreateRequestBodyPointerMerge(t *testing.T) {
	_, svc := setup(t)
	r, err := svc.CreateRequest(CreateRequestInput{
		Project: "demo", Name: "create-merge", Method: "POST", URL: "http://127.0.0.1/f",
		BodyType:   "multipart",
		Body:       &BodyInput{Form: &[]KVInput{{Name: "file", Value: strPtr("/tmp/a.png"), Enabled: boolPtr(true), Type: strPtr("file")}}},
		BodyFields: []BodyFieldInput{{Name: "file", Type: strPtr("file"), Required: boolPtr(true)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Body.Type != "multipart" || len(r.Body.Form) != 1 {
		t.Fatalf("create 的 body 合并异常: %+v", r.Body)
	}
	if len(r.Body.Fields) != 1 || r.Body.Fields[0].Name != "file" {
		t.Fatalf("body 合并把 bodyFields 清掉了: %+v", r.Body.Fields)
	}

	// 只给 bodyType/bodyRaw 时，不应把 form 整份清空（旧实现是 Body{Type,Raw} 整份替换）
	r2, err := svc.CreateRequest(CreateRequestInput{
		Project: "demo", Name: "create-raw", Method: "POST", URL: "http://127.0.0.1/g",
		Body:    &BodyInput{Form: &[]KVInput{{Name: "k", Value: strPtr("v"), Enabled: boolPtr(true)}}},
		BodyRaw: `{"k":"v"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Body.Raw != `{"k":"v"}` || len(r2.Body.Form) != 1 {
		t.Fatalf("bodyRaw 快捷字段不应清掉 form: %+v", r2.Body)
	}
}
