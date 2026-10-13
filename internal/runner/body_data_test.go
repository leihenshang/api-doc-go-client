package runner

import (
	"testing"

	"api-doc-go-client/internal/collection"
)

// T5：raw 为空、data（Bruno 结构化体）有值时，发送必须回落 data，不能发出空体。
func TestBuildBodyFallsBackToData(t *testing.T) {
	b, ct, err := buildBody(collection.Body{Type: "json", Data: `{"a":1}`}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ct != "application/json" {
		t.Fatalf("content-type 不对：%s", ct)
	}
	if string(b) != `{"a":1}` {
		t.Fatalf("应回落 data 作为请求体，实际 %q", string(b))
	}
}

// raw 与 data 同时存在时 raw 优先（界面编辑后以 raw 为准）。
func TestBuildBodyRawWinsOverData(t *testing.T) {
	b, _, err := buildBody(collection.Body{Type: "json", Raw: `{"raw":1}`, Data: `{"data":2}`}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"raw":1}` {
		t.Fatalf("raw 应优先，实际 %q", string(b))
	}
}

// data 中的变量引用同样参与渲染（回落路径也走 varx.Resolve）。
func TestBuildBodyDataResolvesVars(t *testing.T) {
	b, _, err := buildBody(collection.Body{Type: "json", Data: `{"host":"{{h}}"}`},
		map[string]string{"h": "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"host":"example.com"}` {
		t.Fatalf("data 中变量应渲染，实际 %q", string(b))
	}
}
