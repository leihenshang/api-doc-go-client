package mcp_test

// 请求体字段表格（body.fields）与 API 概述（summary）在 MCP create/update/detail 的三态语义。
//
// 核心风险（设计 R1 / C3 / C4）：AI 只是补一次请求体原文（body 整份覆盖），
// 不能顺手把用户手写的字段表格与概述清掉 —— 除非显式传 bodyFields / body.fields 空数组。

import (
	. "api-doc-go-client/internal/mcp" //nolint:revive // 历史测试文件：沿用未加前缀的调用写法
	"testing"
)

// sampleBodyFields 表格样例（含 required=false 与空 example/description 的行）。
func sampleBodyFields() []BodyFieldInput {
	return []BodyFieldInput{
		{Name: "uid", Type: strPtr("string"), Required: boolPtr(true), Example: strPtr("u-42"), Description: strPtr("用户ID")},
		{Name: "page", Type: strPtr("integer")},
	}
}

// TestCreateRequestWithSummaryAndBodyFields create_request 的 summary / bodyFields 必须落盘，
// 并能经 get_request_detail 读回（create 走草稿路径，漏搬 Summary 会静默丢概述）。
func TestCreateRequestWithSummaryAndBodyFields(t *testing.T) {
	_, svc := setup(t)

	r, err := svc.CreateRequest(CreateRequestInput{
		Project:    "demo",
		Name:       "create-with-summary",
		Method:     "POST",
		URL:        "http://127.0.0.1/orders",
		BodyType:   "json",
		BodyRaw:    `{"uid":"u-42"}`,
		BodyFields: sampleBodyFields(),
		Summary:    "下单接口",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Summary != "下单接口" {
		t.Fatalf("create 的 summary 未生效: %q", r.Summary)
	}
	if len(r.Body.Fields) != 2 || r.Body.Fields[0].Name != "uid" {
		t.Fatalf("create 的 bodyFields 未生效: %+v", r.Body.Fields)
	}

	d, err := svc.GetRequestDetail("demo", r.UID, DetailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Summary != "下单接口" {
		t.Errorf("详情未返回 summary: %q", d.Summary)
	}
	if len(d.Body.Fields) != 2 || d.Body.Fields[1].Name != "page" || d.Body.Fields[1].Required {
		t.Errorf("详情未返回 body.fields: %+v", d.Body.Fields)
	}
}

// TestUpdateRequestBodyOverwriteKeepsFieldsAndSummary body 整份覆盖不得清掉表格与概述（C3）：
// 只传 body（fields/descs 为 nil）→ 表格、备注、概述都还在。
func TestUpdateRequestBodyOverwriteKeepsFieldsAndSummary(t *testing.T) {
	_, svc := setup(t)

	r, err := svc.CreateRequest(CreateRequestInput{
		Project:    "demo",
		Name:       "keep-fields",
		Method:     "POST",
		URL:        "http://127.0.0.1/x",
		Body:       &BodyInput{Type: strPtr("json"), Raw: strPtr(`{"uid":"u-42"}`), Descs: &map[string]string{"uid": "用户ID"}},
		BodyFields: sampleBodyFields(),
		Summary:    "原始概述",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Body.Descs["uid"] != "用户ID" {
		t.Fatalf("建请求时备注未生效: %+v", r.Body.Descs)
	}

	// 只覆盖 body 原文：fields / descs 都没传（nil）。
	upd, err := svc.UpdateRequest(UpdateRequestInput{
		Project: "demo",
		UID:     r.UID,
		Body: &BodyInput{
			Type: strPtr("json"),
			Raw:  strPtr(`{"uid":"u-99"}`),
			// Fields / Descs 保持 nil：JSON 里即「缺键」
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(upd.Body.Fields) != 2 {
		t.Fatalf("body 覆盖把表格清掉了: %+v", upd.Body.Fields)
	}
	if upd.Body.Descs["uid"] != "用户ID" {
		t.Fatalf("body 覆盖把备注清掉了: %+v", upd.Body.Descs)
	}
	if upd.Summary != "原始概述" {
		t.Fatalf("body 覆盖把概述清掉了: %q", upd.Summary)
	}
	if upd.Body.Raw != `{"uid":"u-99"}` {
		t.Fatalf("body 原文未生效: %q", upd.Body.Raw)
	}

	// 显式空数组才清空表格。
	empty := []BodyFieldInput{}
	cleared, err := svc.UpdateRequest(UpdateRequestInput{Project: "demo", UID: r.UID, BodyFields: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared.Body.Fields) != 0 {
		t.Fatalf("bodyFields: [] 应清空表格: %+v", cleared.Body.Fields)
	}
	if cleared.Summary != "原始概述" {
		t.Fatalf("清空表格不应影响概述: %q", cleared.Summary)
	}
}

// TestUpdateRequestSetsFieldsAndSummaryIndependently bodyFields / summary 可分别设置：
// 传一个不清另一个，传空串清概述，且不传则不动。
func TestUpdateRequestSetsFieldsAndSummaryIndependently(t *testing.T) {
	_, svc := setup(t)

	r, err := svc.CreateRequest(CreateRequestInput{
		Project: "demo", Name: "independent", Method: "POST", URL: "http://127.0.0.1/y",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 只设表格，不设概述。
	fields := sampleBodyFields()
	r2, err := svc.UpdateRequest(UpdateRequestInput{Project: "demo", UID: r.UID, BodyFields: &fields})
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Body.Fields) != 2 || r2.Summary != "" {
		t.Fatalf("单独设置表格异常: fields=%+v summary=%q", r2.Body.Fields, r2.Summary)
	}

	// 只设概述，不动表格。
	sum := "只设概述"
	r3, err := svc.UpdateRequest(UpdateRequestInput{Project: "demo", UID: r.UID, Summary: &sum})
	if err != nil {
		t.Fatal(err)
	}
	if r3.Summary != sum || len(r3.Body.Fields) != 2 {
		t.Fatalf("单独设置概述异常: summary=%q fields=%+v", r3.Summary, r3.Body.Fields)
	}

	// 传空串清概述，表格仍在。
	emptyStr := ""
	r4, err := svc.UpdateRequest(UpdateRequestInput{Project: "demo", UID: r.UID, Summary: &emptyStr})
	if err != nil {
		t.Fatal(err)
	}
	if r4.Summary != "" || len(r4.Body.Fields) != 2 {
		t.Fatalf("清空概述异常: summary=%q fields=%+v", r4.Summary, r4.Body.Fields)
	}

	// 什么都不传 → 两者都不动。
	r5, err := svc.UpdateRequest(UpdateRequestInput{Project: "demo", UID: r.UID})
	if err != nil {
		t.Fatal(err)
	}
	if len(r5.Body.Fields) != 2 {
		t.Fatalf("不传 bodyFields 时表格被改动: %+v", r5.Body.Fields)
	}
}
