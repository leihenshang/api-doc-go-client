package script

import (
	"strings"
	"testing"
)

func TestPreRequestVarsAndScript(t *testing.T) {
	r := New(map[string]string{"host": "http://x"}, Scripts{
		PreRequest: `bru.setVar('ts', '42'); bru.setVar('fromEnv', bru.getVar('host'))`,
	})
	out := r.RunPreRequest([]Var{
		{Name: "traceId", Value: "t-1", Enabled: true},
		{Name: "off", Value: "no", Enabled: false},
	})
	if out.ScriptError != "" {
		t.Fatalf("脚本错误: %s", out.ScriptError)
	}
	if out.Vars["traceId"] != "t-1" {
		t.Fatalf("vars.pre-request 未生效: %+v", out.Vars)
	}
	if _, ok := out.Vars["off"]; ok {
		t.Fatalf("禁用变量不应写入")
	}
	if out.Vars["ts"] != "42" {
		t.Fatalf("脚本 setVar 未生效: %+v", out.Vars)
	}
	if out.Vars["fromEnv"] != "http://x" {
		t.Fatalf("getVar 环境变量: %+v", out.Vars)
	}
	// 发送用变量应包含脚本写入项
	all := r.Vars()
	if all["ts"] != "42" || all["host"] != "http://x" {
		t.Fatalf("Vars 合并不全: %+v", all)
	}
}

func TestPostResponseAsserts(t *testing.T) {
	r := New(map[string]string{}, Scripts{
		PostResponse: `bru.setVar('id', res.body.id)`,
	})
	out := r.RunPostResponse(&Response{
		Status:       200,
		Headers:      map[string]string{"Content-Type": "application/json"},
		Body:         map[string]any{"id": "u-1", "n": float64(3)},
		BodyText:     `{"id":"u-1","n":3}`,
		ResponseTime: 12,
	}, []Assert{
		{Name: "状态 200", Expr: "res.status === 200"},
		{Name: "有 id", Expr: `res.body.id === "u-1"`},
		{Name: "失败样例", Expr: "res.status === 500"},
		{Name: "运行时", Expr: "JSON.parse('{')"},
	})
	if out.ScriptError != "" {
		t.Fatalf("脚本错误: %s", out.ScriptError)
	}
	if out.Vars["id"] != "u-1" {
		t.Fatalf("post-response setVar: %+v", out.Vars)
	}
	if len(out.Asserts) != 4 {
		t.Fatalf("断言条数: %d", len(out.Asserts))
	}
	if !out.Asserts[0].Passed || !out.Asserts[1].Passed {
		t.Fatalf("应通过的断言失败: %+v", out.Asserts)
	}
	if out.Asserts[2].Passed {
		t.Fatalf("应失败的断言通过了: %+v", out.Asserts[2])
	}
	if out.Asserts[3].Passed || out.Asserts[3].Error == "" {
		t.Fatalf("运行时错误应记入 Error: %+v", out.Asserts[3])
	}
}

func TestParseBody(t *testing.T) {
	if ParseBody(`{"a":1}`).(map[string]any)["a"] == nil {
		t.Fatalf("JSON 未解析")
	}
	if ParseBody("plain") != "plain" {
		t.Fatalf("非 JSON 应保留字符串")
	}
	if ParseBody("  ") != nil {
		t.Fatalf("空白应为 nil")
	}
}

func TestScriptErrorNotFatal(t *testing.T) {
	r := New(nil, Scripts{PreRequest: `throw new Error("boom")`})
	out := r.RunPreRequest(nil)
	if !strings.Contains(out.ScriptError, "boom") {
		t.Fatalf("脚本错误应记录: %q", out.ScriptError)
	}
}
