package varx

import (
	"regexp"
	"strings"
	"testing"
)

func TestResolveBasic(t *testing.T) {
	vars := map[string]string{"host": "http://127.0.0.1:8000", "name": "alice"}
	got, missing := Resolve("{{host}}/users?name={{name}}&x={{missing}}", vars)
	want := "http://127.0.0.1:8000/users?name=alice&x={{missing}}"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if len(missing) != 1 || missing[0] != "missing" {
		t.Fatalf("missing = %v", missing)
	}
}

func TestResolveSpacesAndDuplicate(t *testing.T) {
	got, missing := Resolve("{{ a }}-{{a}}-{{b}}-{{b}}", map[string]string{"a": "1"})
	if got != "1-1-{{b}}-{{b}}" {
		t.Fatalf("got %q", got)
	}
	if len(missing) != 1 || missing[0] != "b" {
		t.Fatalf("missing = %v", missing)
	}
}

func TestBuiltins(t *testing.T) {
	got, missing := Resolve("{{$uuid}} {{$timestamp}} {{$isoTimestamp}} {{$randomInt}} {{$nope}}", nil)
	if missing != nil {
		t.Fatalf("内置变量不应报缺失: %v", missing)
	}
	uuidRe := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	parts := strings.Fields(got)
	if !uuidRe.MatchString(parts[0]) {
		t.Fatalf("uuid 格式错误: %q", parts[0])
	}
	if !strings.Contains(parts[2], "T") {
		t.Fatalf("isoTimestamp 格式错误: %q", parts[2])
	}
	if !strings.Contains(got, "{{$nope}}") {
		t.Fatalf("未知 $ 变量应保留原样: %q", got)
	}
}

func TestCollectNames(t *testing.T) {
	names := CollectNames("{{a}} {{ b }} {{c}}")
	if len(names) != 3 || names[0] != "a" || names[1] != "b" || names[2] != "c" {
		t.Fatalf("names = %v", names)
	}
}

func TestCollectValues(t *testing.T) {
	vars := map[string]string{"a": "1", "b": ""}
	got := CollectValues("{{a}}/{{b}}/{{c}}/{{$uuid}}", vars)
	if len(got) != 2 || got["a"] != "1" {
		t.Fatalf("values = %v", got)
	}
	if v, ok := got["b"]; !ok || v != "" {
		t.Fatalf("空值变量也算已定义，应出现在 values 里: %v", got)
	}
	if _, ok := got["c"]; ok {
		t.Fatalf("未定义变量不应出现: %v", got)
	}
	if _, ok := got["$uuid"]; ok {
		t.Fatalf("内置变量不应出现: %v", got)
	}
	if CollectValues("{{c}}", vars) != nil || CollectValues("", vars) != nil {
		t.Fatalf("没有命中时应返回 nil")
	}
}

// TestBuiltinsMemoized 同一文本里的同名内置变量必须同值（语义归一 D2）：
// 迁移前每次命中都重新生成，同一个请求里会出现两个不同的 $uuid。
func TestBuiltinsMemoized(t *testing.T) {
	got, _ := Resolve("{{$uuid}}|{{$uuid}}|{{$timestamp}}|{{$timestamp}}", nil)
	parts := strings.Split(got, "|")
	if parts[0] != parts[1] || parts[2] != parts[3] {
		t.Fatalf("同名内置变量应取同一个值，got %q", got)
	}
}

// TestResolveHyphenatedName 复现用户场景：环境变量名带连字符 clxone-ipam。
func TestResolveHyphenatedName(t *testing.T) {
	out, missing := Resolve("{{clxone-ipam}}/apis/x?action=export", map[string]string{
		"clxone-ipam": "http://127.0.0.1:4444",
	})
	if missing != nil {
		t.Fatalf("不应有缺失变量，got %v", missing)
	}
	want := "http://127.0.0.1:4444/apis/x?action=export"
	if out != want {
		t.Fatalf("解析结果应为 %q，实际 %q", want, out)
	}
}

// TestCollectHyphenatedName 悬停取值：含连字符的名字要能被识别。
func TestCollectHyphenatedName(t *testing.T) {
	names := CollectNames("{{clxone-ipam}}/x")
	if len(names) != 1 || names[0] != "clxone-ipam" {
		t.Fatalf("CollectNames 应返回 [clxone-ipam]，实际 %v", names)
	}
	vals := CollectValues("{{clxone-ipam}}", map[string]string{"clxone-ipam": "http://127.0.0.1:4444"})
	if vals["clxone-ipam"] != "http://127.0.0.1:4444" {
		t.Fatalf("CollectValues 应取到 clxone-ipam 的值，实际 %v", vals)
	}
}

// TestResolveNestedHyphenated 变量值里再引用带连字符的变量（嵌套）。
func TestResolveNestedHyphenated(t *testing.T) {
	out, missing := Resolve("{{a}}", map[string]string{"a": "{{b-c}}", "b-c": "v"})
	if missing != nil {
		t.Fatalf("不应有缺失变量，got %v", missing)
	}
	if out != "v" {
		t.Fatalf("嵌套解析结果应为 v，实际 %q", out)
	}
}

// TestMissingHyphenatedReportedByOriginalName 缺失的带连字符变量：缺失名单应回原始名。
func TestMissingHyphenatedReportedByOriginalName(t *testing.T) {
	out, missing := Resolve("{{clxone-ipam}}/x", map[string]string{})
	if !strings.Contains(out, "{{clxone-ipam}}") {
		t.Fatalf("未定义变量应保留原文，实际 %q", out)
	}
	if len(missing) != 1 || missing[0] != "clxone-ipam" {
		t.Fatalf("缺失名单应为 [clxone-ipam]，实际 %v", missing)
	}
}

// TestResolvePlainNameUnchanged 普通（无连字符）变量行为不变。
func TestResolvePlainNameUnchanged(t *testing.T) {
	out, missing := Resolve("{{host}}/x", map[string]string{"host": "http://localhost:2025"})
	if missing != nil || out != "http://localhost:2025/x" {
		t.Fatalf("普通名解析异常：out=%q missing=%v", out, missing)
	}
}
