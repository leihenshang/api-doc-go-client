package mcp

import (
	"strings"
	"testing"

	"api-doc-go-client/internal/collection"
)

// TestRenderEnvsShowsMaskAndSorted 直接测渲染函数：它在内部包（不导出的实现细节）。
// 输出会进 AI 的上下文，排序与掩码标注错了会直接误导调用方。
func TestRenderEnvsShowsMaskAndSorted(t *testing.T) {
	envs := []EnvEntry{
		{Name: "dev", Default: true, Vars: map[string]EnvVarEntry{
			"zeta":  {Name: "zeta", Value: "z"},
			"alpha": {Name: "alpha", Value: maskedValue, Secret: true},
			"off":   {Name: "off", Value: "x", Enabled: false},
		}},
		{Name: "prod", Vars: map[string]EnvVarEntry{}},
	}
	out := renderEnvs("demo", envs)
	for _, want := range []string{"dev", "prod", "默认", maskedValue, "敏感值", "已停用", "空环境"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺少 %q:\n%s", want, out)
		}
	}
	// 变量按名字排序（alpha < off < zeta），否则 AI 每次看到的顺序都不同
	ia, io, iz := strings.Index(out, "alpha"), strings.Index(out, "off"), strings.Index(out, "zeta")
	if !(ia < io && io < iz) {
		t.Errorf("变量未按名字排序:\n%s", out)
	}
	// 空集合也要给得出可读结论
	if s := renderEnvs("demo", nil); !strings.Contains(s, "create_env") {
		t.Errorf("空列表应提示怎么建环境:\n%s", s)
	}
}

// TestRenderDetailShowsSummaryAndBodyFields get_request_detail 的**文本输出**必须带上概述与
// 请求体字段表格：wrap() 只回 TextContent，没有结构化通道，渲染层漏了就等于「写得到、读不回」。
// 这一层曾整体漏掉（单测只断言了结构体字段，正好绕过渲染）。
func TestRenderDetailShowsSummaryAndBodyFields(t *testing.T) {
	d := &RequestDetail{
		Project: "coll", UID: "u-1", Name: "创建订单", Method: "POST", URL: "{{host}}/api/v1/orders",
		Summary: "下单接口，创建一笔待支付订单",
		Body: collection.Body{
			Type: "json",
			Raw:  `{"uid":"u-42"}`,
			Fields: []collection.BodyField{
				{Name: "uid", Type: "string", Required: true, Example: "u-42", Description: "用户ID"},
				{Name: "page", Required: false}, // 类型空 → 回落 string；必填 false → 「否」
			},
		},
	}
	out := renderDetail(d)
	for _, want := range []string{
		"概述：下单接口，创建一笔待支付订单",
		"请求体字段（2 行",
		"| 字段名 | 必选 | 类型 | 示例值 | 说明 |",
		"| uid | 是 | string | u-42 | 用户ID |",
		"| page | 否 | string |", // 类型缺省回落 string
		`{"uid":"u-42"}`,        // 原文照旧输出
	} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺少 %q:\n%s", want, out)
		}
	}

	// 空概述 / 空表格不占位：没有内容就没有这一行（避免 AI 误以为「有个空表格」）。
	bare := renderDetail(&RequestDetail{Project: "coll", UID: "u-2", Name: "无概述", Method: "GET", Body: collection.Body{Type: "none"}})
	for _, banned := range []string{"概述：", "请求体字段（"} {
		if strings.Contains(bare, banned) {
			t.Errorf("空内容不应输出 %q:\n%s", banned, bare)
		}
	}

	// 单元格里的竖线要转义，否则 5 列表格会被撑破。
	broken := renderDetail(&RequestDetail{
		Project: "coll", UID: "u-3", Name: "转义", Method: "POST",
		Body: collection.Body{Fields: []collection.BodyField{{Name: "a|b", Type: "string", Description: "x|y"}}},
	})
	if strings.Contains(broken, "| a|b |") || !strings.Contains(broken, `a\|b`) {
		t.Errorf("竖线未转义:\n%s", broken)
	}
}

// TestRenderDetailExposesExampleUID 示例行必须带 uid：
// update_response_example / delete_response_example 的 exampleUid 只能从 get_request_detail
// 的文本里拿到（工具结果没有结构化字段），漏了就无法定位「已存在」的示例 ——
// send_request / 界面 / 上次会话存下的示例全都改不了、删不掉。
func TestRenderDetailExposesExampleUID(t *testing.T) {
	out := renderDetail(&RequestDetail{
		Project: "coll", UID: "req-1", Name: "带示例", Method: "GET",
		Body: collection.Body{Type: "none"},
		Examples: []ExampleItem{
			{UID: "ex-aaa-111", Name: "线上 504", Path: "examples/带示例/线上 504.yml", Status: 504, BodyBytes: 12},
			{UID: "ex-bbb-222", Name: "正常", Path: "examples/带示例/正常.yml", Status: 200},
		},
	})
	for _, want := range []string{"uid=ex-aaa-111", "uid=ex-bbb-222", "线上 504", "504"} {
		if !strings.Contains(out, want) {
			t.Errorf("示例段缺少 %q（AI 拿不到 exampleUid 就改不了已存在的示例）:\n%s", want, out)
		}
	}
}
