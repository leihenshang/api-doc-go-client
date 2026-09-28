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
