package proto

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// fixture 的 HelloRequest：name(string) / age(int32) / vip(bool) / tags(repeated string) /
// attrs(map<string,string>) / at(Timestamp) / oneof{user,anon} / nested(Nested) / color(Color) / child(自引用)
func mustSayHello(t *testing.T) (protoreflect.MethodDescriptor, *Result) {
	t.Helper()
	res := mustCompileGreeter(t)
	md, err := res.MethodDescriptor("demo.Greeter", "SayHello")
	if err != nil {
		t.Fatalf("取方法描述符失败: %v", err)
	}
	if got := string(md.Input().FullName()); got != "demo.HelloRequest" {
		t.Fatalf("入参 = %q，期望 demo.HelloRequest", got)
	}
	return md, res
}

func TestFieldsListsTopLevelFields(t *testing.T) {
	md, _ := mustSayHello(t)
	fields := Fields(md.Input())
	if len(fields) != 11 {
		t.Fatalf("字段数 = %d，期望 11：%+v", len(fields), fields)
	}
	by := byName(fields)

	// 标量：JSON 名按 protojson 规则（写请求消息用 JSONName）
	if f := by["name"]; f.Type != "string" || f.Kind != "scalar" || f.JSONName != "name" {
		t.Errorf("name 字段不符: %+v", f)
	}
	if f := by["age"]; f.Type != "int32" {
		t.Errorf("age 类型 = %q，期望 int32", f.Type)
	}
	// repeated 标量
	if f := by["tags"]; f.Type != "repeated string" || !f.Repeated || f.Kind != "scalar" {
		t.Errorf("tags 字段不符: %+v", f)
	}
	// map：展开键值类型
	if f := by["attrs"]; f.Kind != "map" || f.Type != "map<string, string>" {
		t.Errorf("attrs 字段不符: %+v", f)
	}
	// 消息（well-known type 与自引用）走全名
	if f := by["at"]; f.Kind != "message" || f.Type != "google.protobuf.Timestamp" {
		t.Errorf("at 字段不符: %+v", f)
	}
	if f := by["nested"]; f.Type != "demo.HelloRequest.Nested" {
		t.Errorf("nested 类型 = %q", f.Type)
	}
	if f := by["child"]; f.Type != "demo.HelloRequest" {
		t.Errorf("child 类型 = %q", f.Type)
	}
	// 枚举走全名
	if f := by["color"]; f.Kind != "enum" || f.Type != "demo.Color" {
		t.Errorf("color 字段不符: %+v", f)
	}
	// oneof 成员照常列出（protojson 里按成员名写）
	for _, n := range []string{"user", "anon"} {
		if _, ok := by[n]; !ok {
			t.Errorf("oneof 成员 %s 未列出", n)
		}
	}
}

func TestFieldsKeepsDeclarationOrderAndComments(t *testing.T) {
	md, _ := mustSayHello(t)
	fields := Fields(md.Input())
	if fields[0].Name != "name" {
		t.Errorf("首个字段 = %q，期望 name（按声明顺序）", fields[0].Name)
	}
	if c := byName(fields)["child"].Comment; !strings.Contains(c, "自引用") {
		t.Errorf("child 的字段注释未取到: %q", c)
	}
}

func TestValidateMessage(t *testing.T) {
	md, res := mustSayHello(t)
	cases := []struct {
		name    string
		text    string
		wantErr bool
	}{
		{name: "空文本视为通过（发全默认值消息）", text: "   ", wantErr: false},
		{name: "合法 JSON 名", text: `{"name":"alice","age":3,"tags":["a"]}`, wantErr: false},
		{name: "嵌套与枚举", text: `{"nested":{"note":"x"},"color":"RED"}`, wantErr: false},
		{name: "未知字段报错", text: `{"nope":1}`, wantErr: true},
		{name: "类型不匹配报错", text: `{"age":"abc"}`, wantErr: true},
		{name: "语法错误报错", text: `{"name":`, wantErr: true},
	}
	for _, c := range cases {
		err := ValidateMessage(md, c.text, res.Types)
		if c.wantErr && err == nil {
			t.Errorf("%s：期望报错，实际通过", c.name)
		}
		if !c.wantErr && err != nil {
			t.Errorf("%s：期望通过，实际 %v", c.name, err)
		}
	}
	// 错误要能定位到具体字段（G5.3：带字段路径）
	if err := ValidateMessage(md, `{"age":"abc"}`, res.Types); err == nil || !strings.Contains(err.Error(), "age") {
		t.Errorf("类型错误未带字段路径: %v", err)
	}
}

func byName(fields []FieldInfo) map[string]FieldInfo {
	out := make(map[string]FieldInfo, len(fields))
	for _, f := range fields {
		out[f.Name] = f
	}
	return out
}
