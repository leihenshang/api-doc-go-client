package syncengine

import (
	"encoding/json"
	"testing"

	"api-doc-go-client/internal/collection"
)

// TestApplyUpsertRejectsBrokenPayload 载荷不是合法 JSON 时必须返回错误。
// 忽略 json.Unmarshal 的 err 会让 Payload 保持零值，applyAPI/applyDoc 随后把
// 一个空条目写盘 —— 等于用垃圾覆盖本地真实数据，且用户完全无感。
func TestApplyUpsertRejectsBrokenPayload(t *testing.T) {
	dir := t.TempDir()
	c, err := collection.Open(dir)
	if err != nil {
		t.Fatalf("open collection: %v", err)
	}
	e := &Engine{Coll: c}
	rep := &Report{}

	for _, tc := range []struct {
		name    string
		typ     string
		payload string
	}{
		{"截断的 JSON", "api", `{"name":"x","requests":[`},
		{"空载荷", "doc", ``},
		{"类型不匹配", "api", `"just a string"`},
	} {
		it := Item{Type: tc.typ, UID: "broken-" + tc.name, Op: "upsert",
			ItemRev: 1, Payload: json.RawMessage(tc.payload)}
		if err := e.applyUpsert(it, rep); err == nil {
			t.Errorf("%s：应返回错误，否则会用零值条目覆盖本地数据", tc.name)
		}
	}

	// 三次失败都不应留下任何条目
	info, err := c.Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if len(info.Tree) != 0 {
		t.Errorf("解析失败不应写入集合树，现有 %d 个节点", len(info.Tree))
	}
	reqs, err := c.ListAllRequests()
	if err != nil {
		t.Fatalf("ListAllRequests: %v", err)
	}
	if len(reqs) != 0 {
		t.Errorf("解析失败不应落任何请求文件，现有 %d 个", len(reqs))
	}
}
