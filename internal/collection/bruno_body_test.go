package collection

import (
	"strings"
	"testing"
)

// T5：Bruno body:json 段必须解析进 Data 与 Raw（之前 body 整段被忽略）。
func TestParseBruJSONBody(t *testing.T) {
	src := []byte("meta:\n  name: 创建订单\nhttp:\n  method: post\n  url: http://x/orders\n" +
		"body:json\n'''\n" + `{"orderNo":"A-1","amount":12}` + "\n'''\n")
	req, name, ok := parseBru(src)
	if !ok || name != "创建订单" {
		t.Fatalf("解析失败：ok=%v name=%q", ok, name)
	}
	if req.Body.Type != "json" {
		t.Fatalf("body type 应为 json，实际 %q", req.Body.Type)
	}
	if req.Body.Data == "" || req.Body.Raw == "" {
		t.Fatalf("data/raw 都应填充：%+v", req.Body)
	}
	if req.Body.Data != req.Body.Raw {
		t.Fatalf("导入时 data 与 raw 应同源：data=%q raw=%q", req.Body.Data, req.Body.Raw)
	}
	if !strings.Contains(req.Body.Data, `"orderNo":"A-1"`) {
		t.Fatalf("正文内容不对：%q", req.Body.Data)
	}
}

func TestParseBruTextBody(t *testing.T) {
	src := []byte("meta:\n  name: 文本\nhttp:\n  method: get\n  url: http://x\nbody:text\n'''\nhello world\n'''\n")
	req, _, ok := parseBru(src)
	if !ok {
		t.Fatal("解析失败")
	}
	if req.Body.Type != "text" || req.Body.Raw != "hello world" || req.Body.Data != "" {
		t.Fatalf("text body 解析不对：%+v", req.Body)
	}
}
