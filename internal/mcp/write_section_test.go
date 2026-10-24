package mcp_test

// update_request 的 section 节点守卫：限定一次调用只改「请求本身 / 响应文档 / 接口说明」之一。
//
// 为什么需要：update_request 一次能改三类数据，AI 常「只想补响应字段」却顺手带上 url，
// 把请求本身改坏。section 非空时越界字段会被拒绝，报错里带越界字段名与该节点的允许清单。
// section 为空 = 不限制（兼容所有旧调用）。

import (
	. "api-doc-go-client/internal/mcp" //nolint:revive // 与其它测试一致：白盒式调用
	"strings"
	"testing"
)

// TestSectionResponseRejectsRequestBody section=response 时传 body 必须报错，且错误里点名 body。
func TestSectionResponseRejectsRequestBody(t *testing.T) {
	_, svc := setup(t)
	r, err := svc.CreateRequest(CreateRequestInput{
		Project: "demo", Name: "sec-response", Method: "POST", URL: "http://127.0.0.1/s",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.UpdateRequest(UpdateRequestInput{
		Project: "demo", UID: r.UID, Section: "response",
		Body:    &BodyInput{Raw: strPtr(`{"a":1}`)},
		RespDoc: &RespDocInput{Sample: strPtr(`{"code":0}`)},
	})
	if err == nil {
		t.Fatal("section=response 时传 body 应被拒绝")
	}
	if !strings.Contains(err.Error(), "body") {
		t.Errorf("报错应点名越界字段 body，实际: %v", err)
	}
	if !strings.Contains(err.Error(), "respDoc") {
		t.Errorf("报错应列出该 section 允许的字段 respDoc，实际: %v", err)
	}
	// 写入被拒：磁盘上的请求体没有被改动
	d, derr := svc.GetRequestDetail("demo", r.UID, DetailOptions{})
	if derr != nil {
		t.Fatal(derr)
	}
	if d.Body.Raw != "" || d.RespDoc != nil {
		t.Errorf("被拒绝的调用不应落盘: body=%q respDoc=%+v", d.Body.Raw, d.RespDoc)
	}
}

// TestSectionRequestRejectsRespDoc section=request 时传 respDoc 必须报错。
func TestSectionRequestRejectsRespDoc(t *testing.T) {
	_, svc := setup(t)
	r, err := svc.CreateRequest(CreateRequestInput{
		Project: "demo", Name: "sec-request", Method: "GET", URL: "http://127.0.0.1/t",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.UpdateRequest(UpdateRequestInput{
		Project: "demo", UID: r.UID, Section: "request",
		Name:    strPtr("改个名"),
		RespDoc: &RespDocInput{Sample: strPtr(`{"code":0}`)},
	})
	if err == nil {
		t.Fatal("section=request 时传 respDoc 应被拒绝")
	}
	if !strings.Contains(err.Error(), "respDoc") {
		t.Errorf("报错应点名越界字段 respDoc，实际: %v", err)
	}
	if !strings.Contains(err.Error(), "section") {
		t.Errorf("报错应带上 section 上下文，实际: %v", err)
	}
}

// TestSectionRequestPassesAndApplies section=request + 请求字段：正常通过并生效。
func TestSectionRequestPassesAndApplies(t *testing.T) {
	_, svc := setup(t)
	r, err := svc.CreateRequest(CreateRequestInput{
		Project: "demo", Name: "sec-ok", Method: "GET", URL: "http://127.0.0.1/u",
	})
	if err != nil {
		t.Fatal(err)
	}
	up, err := svc.UpdateRequest(UpdateRequestInput{
		Project: "demo", UID: r.UID, Section: "request",
		URL:     strPtr("http://127.0.0.1/u2"),
		Summary: strPtr("请求侧概述"),
	})
	if err != nil {
		t.Fatalf("section=request 只改请求字段应通过: %v", err)
	}
	if up.URL != "http://127.0.0.1/u2" || up.Summary != "请求侧概述" {
		t.Errorf("section=request 的更新未生效: %+v", up)
	}
}

// TestSectionDocsOnly section=docs 只允许 docs；空 section 不限制（旧调用照旧）。
func TestSectionDocsOnly(t *testing.T) {
	_, svc := setup(t)
	r, err := svc.CreateRequest(CreateRequestInput{
		Project: "demo", Name: "sec-docs", Method: "GET", URL: "http://127.0.0.1/v",
	})
	if err != nil {
		t.Fatal(err)
	}
	// docs 节点写说明：通过
	if _, err := svc.UpdateRequest(UpdateRequestInput{
		Project: "demo", UID: r.UID, Section: "docs", Docs: strPtr("# 说明"),
	}); err != nil {
		t.Fatalf("section=docs 写 docs 应通过: %v", err)
	}
	// docs 节点带 url：报错
	if _, err := svc.UpdateRequest(UpdateRequestInput{
		Project: "demo", UID: r.UID, Section: "docs",
		Docs: strPtr("# 说明2"), URL: strPtr("http://x/2"),
	}); err == nil || !strings.Contains(err.Error(), "url") {
		t.Fatalf("section=docs 传 url 应报错并点名 url，实际: %v", err)
	}
	// 空 section：不限制，请求字段与 respDoc 可同时写
	status := 200
	if _, err := svc.UpdateRequest(UpdateRequestInput{
		Project: "demo", UID: r.UID,
		URL:     strPtr("http://127.0.0.1/v2"),
		RespDoc: &RespDocInput{Status: &status, Sample: strPtr(`{"ok":true}`)},
	}); err != nil {
		t.Fatalf("空 section 不应限制字段组合: %v", err)
	}
}

// TestSectionUnsupported 非 request/response/docs 的取值直接报「不支持的 section」。
func TestSectionUnsupported(t *testing.T) {
	_, svc := setup(t)
	r, err := svc.CreateRequest(CreateRequestInput{
		Project: "demo", Name: "sec-bad", Method: "GET", URL: "http://127.0.0.1/w",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.UpdateRequest(UpdateRequestInput{Project: "demo", UID: r.UID, Section: "body"})
	if err == nil || !strings.Contains(err.Error(), "不支持的 section") {
		t.Fatalf("非法 section 应报「不支持的 section」，实际: %v", err)
	}
}
