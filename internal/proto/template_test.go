package proto

import (
	"encoding/json"
	"testing"
)

func TestSampleMessageFillsFields(t *testing.T) {
	res := mustCompileGreeter(t)
	md, err := res.MessageDescriptor("demo.HelloRequest")
	if err != nil {
		t.Fatalf("查消息失败: %v", err)
	}
	out, err := SampleMessage(md, res.Types)
	if err != nil {
		t.Fatalf("生成样例失败: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("样例不是合法 JSON: %v\n%s", err, out)
	}
	for _, k := range []string{"name", "age", "vip", "tags", "attrs", "at", "user", "nested", "color", "child"} {
		if _, ok := got[k]; !ok {
			t.Errorf("样例缺少字段 %s:\n%s", k, out)
		}
	}
	if _, ok := got["anon"]; ok {
		t.Errorf("oneof 只应保留第一个成员 user，不应出现 anon:\n%s", out)
	}
	if list, ok := got["tags"].([]any); !ok || len(list) != 1 {
		t.Errorf("repeated 字段应给一条: %v", got["tags"])
	}
	if m, ok := got["attrs"].(map[string]any); !ok || len(m) != 1 {
		t.Errorf("map 字段应给一条: %v", got["attrs"])
	}
	// Timestamp 按 protojson 的 WKT 规则渲染成字符串
	if at, ok := got["at"].(string); !ok || at == "" {
		t.Errorf("google.protobuf.Timestamp 应渲染成字符串: %v", got["at"])
	}
	// 枚举按名字输出
	if color, ok := got["color"].(string); !ok || color != "COLOR_UNSPECIFIED" {
		t.Errorf("枚举应按名字输出: %v", got["color"])
	}
}

func TestSampleMessageSelfReferenceStops(t *testing.T) {
	res := mustCompileGreeter(t)
	md, err := res.MessageDescriptor("demo.HelloRequest")
	if err != nil {
		t.Fatalf("查消息失败: %v", err)
	}
	out, err := SampleMessage(md, nil) // types 传 nil 也要能工作
	if err != nil {
		t.Fatalf("生成样例失败: %v", err)
	}
	// 自引用必须限深：嵌套 3 层后不再展开（child 为 null 或缺失）
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("样例不是合法 JSON: %v", err)
	}
	level := 0
	for cur := got; cur != nil; {
		next, ok := cur["child"].(map[string]any)
		if !ok {
			break
		}
		level++
		cur = next
		if level > sampleMaxDepth {
			t.Fatalf("自引用未限深（展开超过 %d 层）", sampleMaxDepth)
		}
	}
}

func TestSampleMessageNilDescriptor(t *testing.T) {
	if _, err := SampleMessage(nil, nil); err == nil {
		t.Error("缺少消息描述符时应报错")
	}
}
