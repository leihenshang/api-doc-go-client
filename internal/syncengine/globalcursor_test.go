package syncengine

// R2 补强：全局变量不推进项目游标，客户端必须在「项目游标没动、GlobalCursor 前进」时
// 也拉一次增量，否则管理员改了全局变量、项目本身无变更时永远拿不到。

import "testing"

func TestPullAppliesGlobalVarWhenOnlyGlobalCursorMoves(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)

	// 已同步到项目游标 10、全局游标 5
	e.Bind.Cursor = 10
	e.Bind.GlobalCursor = 5
	f.state = State{Cursor: 10, GlobalCursor: 6}
	f.changes = Changes{Cursor: 10, Items: []Item{
		{Type: "variable", UID: "gv9", Op: "upsert", ItemRev: 9, Payload: payloadOf(t, Payload{
			Name: "global_only", Value: "https://only-global.example.com", Scope: 3,
		})},
	}}

	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	if rep.Pulled != 1 || len(rep.Errors) != 0 {
		t.Fatalf("全局变量应被应用: %+v", rep)
	}
	globals, err := c.ListGlobals()
	if err != nil {
		t.Fatalf("ListGlobals: %v", err)
	}
	if len(globals) != 1 || globals[0].Name != "global_only" {
		t.Fatalf("全局变量未落地: %+v", globals)
	}
	if e.Bind.GlobalCursor != 6 {
		t.Fatalf("GlobalCursor 基线应推进到 6，实际 %d", e.Bind.GlobalCursor)
	}

	// 第二轮：两端游标都没动 → 不应再发增量请求（pulled 保持 0）。
	f.changes = Changes{Cursor: 10}
	rep2, err := e.RunOne()
	if err != nil {
		t.Fatalf("RunOne 第二轮: %v", err)
	}
	if rep2.Pulled != 0 {
		t.Fatalf("游标未变不应重复应用: %+v", rep2)
	}
}
