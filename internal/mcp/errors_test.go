package mcp

// 入参校验错误的中文转述（errors.go）单测：多参数、两种引号风格、无引号兜底。

import (
	"strings"
	"testing"
)

func TestQuotedList(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"单个", `["uid"]`, "uid"},
		{"JSON 多参数", `["uid","name"]`, "uid、name"},
		{"Go %q 多参数", `["uid" "name"]`, "uid、name"},
		{"无引号兜底", `uid, name`, "uid、name"},
		{"空输入", ``, ""},
	}
	for _, c := range cases {
		if got := quotedList(c.in); got != c.want {
			t.Errorf("%s: quotedList(%q) = %q，期望 %q", c.name, c.in, got, c.want)
		}
	}
}

func TestHumanizeSchemaErrorMultiParam(t *testing.T) {
	raw := `validating "arguments": validating root: required: missing properties: ["uid","name"]`
	got := humanizeSchemaError("create_doc", raw)
	if strings.ContainsAny(got, "[]") {
		t.Errorf("中文报错不应残留方括号：%q", got)
	}
	if !strings.Contains(got, "uid、name") {
		t.Errorf("应列出全部缺失参数：%q", got)
	}
	if !strings.Contains(got, "create_doc") {
		t.Errorf("应点名工具：%q", got)
	}
}

func TestHumanizeSchemaErrorExtraParam(t *testing.T) {
	raw := `validating "arguments": validating root: unexpected additional properties ["uids"]`
	got := humanizeSchemaError("get_doc", raw)
	if !strings.Contains(got, "uids") || strings.ContainsAny(got, "[]") {
		t.Errorf("多余参数提示不符：%q", got)
	}
}
