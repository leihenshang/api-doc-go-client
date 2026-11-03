package syncengine

// R3：接口生命周期元数据（草稿/已发布/已废弃 + 版本 + 废弃时间）在客户端的落地。
//   - 服务端下发 → 落到本地请求的 meta 段（重开集合仍在）；
//   - 缺键不改：服务端不带这三个键时，本地已有状态不被清空。

import "testing"

func TestLifecycleAppliedFromServer(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 10}
	f.snapshot = Snapshot{Cursor: 10, Items: []Item{
		{Type: "api", UID: "a1", Op: "upsert", ItemRev: 1, Payload: payloadOf(t, Payload{
			Name: "接口一", Method: "GET", URL: "/x", Extra: "{}",
			ApiStatus: 3, Version: "v2", DeprecatedAt: "2026-01-02T03:04:05Z",
		})},
	}}
	c, e := newBoundEngine(t, cli)

	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	if rep.Pulled != 1 || len(rep.Errors) != 0 {
		t.Fatalf("应成功应用 1 条: %+v", rep)
	}
	r, err := c.ReadRequest("a1")
	if err != nil {
		t.Fatalf("ReadRequest: %v", err)
	}
	if r.Status != 3 || r.Version != "v2" || r.DeprecatedAt != "2026-01-02T03:04:05Z" {
		t.Fatalf("生命周期元数据未落地: status=%d version=%q deprecatedAt=%q",
			r.Status, r.Version, r.DeprecatedAt)
	}

	// 第二轮：服务端下发不带状态键 → 本地已有的「已废弃」状态必须保持（缺键不改）。
	f.state = State{Cursor: 11}
	e.Bind.Cursor = 10
	f.changes = Changes{Cursor: 11, Items: []Item{
		{Type: "api", UID: "a1", Op: "upsert", ItemRev: 2, Payload: payloadOf(t, Payload{
			Name: "接口一", Method: "GET", URL: "/x", Extra: "{}",
		})},
	}}
	if _, err := e.RunOne(); err != nil {
		t.Fatalf("RunOne 第二轮: %v", err)
	}
	r, err = c.ReadRequest("a1")
	if err != nil {
		t.Fatalf("ReadRequest 第二轮: %v", err)
	}
	if r.Status != 3 || r.Version != "v2" {
		t.Fatalf("缺键不应清空本地状态: status=%d version=%q", r.Status, r.Version)
	}
}
