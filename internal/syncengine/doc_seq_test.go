package syncengine

// R5：文档同层排序权重（服务端 priority ↔ 客户端 frontmatter 的 seq）落地。

import "testing"

func TestDocSeqAppliedFromServer(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 10}
	f.snapshot = Snapshot{Cursor: 10, Items: []Item{
		{Type: "doc", UID: "d1", Op: "upsert", ItemRev: 1, Payload: payloadOf(t, Payload{
			Name: "排序文档", Title: "排序文档", Content: "正文", DocSeq: 7,
		})},
	}}
	c, e := newBoundEngine(t, cli)

	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	if rep.Pulled != 1 || len(rep.Errors) != 0 {
		t.Fatalf("应成功应用 1 条文档: %+v", rep)
	}
	d, err := c.ReadDoc("d1")
	if err != nil {
		t.Fatalf("ReadDoc: %v", err)
	}
	if d.Seq != 7 {
		t.Fatalf("文档排序权重未落地，seq=%d", d.Seq)
	}

	// 第二轮：服务端不带 doc_seq → 本地顺序不被清空（缺键不改）。
	f.state = State{Cursor: 11}
	e.Bind.Cursor = 10
	f.changes = Changes{Cursor: 11, Items: []Item{
		{Type: "doc", UID: "d1", Op: "upsert", ItemRev: 2, Payload: payloadOf(t, Payload{
			Name: "排序文档", Title: "排序文档", Content: "正文二",
		})},
	}}
	if _, err := e.RunOne(); err != nil {
		t.Fatalf("RunOne 第二轮: %v", err)
	}
	d, err = c.ReadDoc("d1")
	if err != nil {
		t.Fatalf("ReadDoc 第二轮: %v", err)
	}
	if d.Seq != 7 {
		t.Fatalf("缺键不应清空本地顺序，seq=%d", d.Seq)
	}
}
