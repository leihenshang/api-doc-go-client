package syncengine

// 「只改文档」也必须推送（P0-4 回归）：文档条目/响应示例/环境变量都不进索引，
// 走 Bind 的 hash 基线做脏检测，所以 dirty 与 pendingDeletes 都为空是常态。
// 这里锁住 pushDirty 不会因此早退，而是照常收集并调用 Push。

import (
	"encoding/json"
	"testing"
)

// pushCount 已收到的 push 请求数。
func (f *fakeServer) pushCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.pushes)
}

// lastPushOps 最近一次 push 请求携带的 op 列表。
func (f *fakeServer) lastPushOps(t *testing.T) []Op {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pushes) == 0 {
		t.Fatal("服务端没收到任何 push 请求")
	}
	return f.pushes[len(f.pushes)-1].Ops
}

// TestPushDirtyOnlyDocChanges 只新建一篇文档（无 request 脏节点、无墓碑）时，
// pushDirty 必须产出 doc op 并真的调用 Push；回执成功后写入 hash 基线，下一轮不再重推。
func TestPushDirtyOnlyDocChanges(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)

	// 先跑一轮，把 Open 时初始化出来的环境/变量基线推掉，后面才能断言「本轮只有 doc」。
	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("首轮 pushDirty: %v", err)
	}
	basePushes := f.pushCount()

	d, err := c.CreateDoc("只改文档", "", "")
	if err != nil {
		t.Fatalf("CreateDoc: %v", err)
	}
	// 前置条件断言：这条路径正是「dirty 为空 + 无墓碑」，否则测的不是要修的缺陷。
	dirty, err := c.DirtyNodes()
	if err != nil {
		t.Fatalf("DirtyNodes: %v", err)
	}
	if len(dirty) != 0 {
		t.Fatalf("前置条件不成立：不应有脏请求节点，got %d", len(dirty))
	}
	if del := c.LoadPendingDeletes(); len(del) != 0 {
		t.Fatalf("前置条件不成立：不应有待推送删除，got %v", del)
	}

	rep := &Report{}
	if err := e.pushDirty(rep); err != nil {
		t.Fatalf("pushDirty: %v", err)
	}
	if f.pushCount() == basePushes {
		t.Fatal("只改文档也必须调用 Push，实际一次请求都没发")
	}
	ops := f.lastPushOps(t)
	if len(ops) != 1 {
		t.Fatalf("本轮应只推送 1 条 doc op，got %d 条: %+v", len(ops), ops)
	}
	op := ops[0]
	if op.Op != "upsert" || op.Type != "doc" || op.UID != d.UID {
		t.Fatalf("应推送 doc upsert（uid=%s），got %+v", d.UID, op)
	}
	if op.OpID == "" {
		t.Fatal("推送 op 必须带 op_id（幂等凭据）")
	}
	var p Payload
	if err := json.Unmarshal(op.Payload, &p); err != nil {
		t.Fatalf("解析推送载荷失败: %v", err)
	}
	if p.Name != "只改文档" {
		t.Fatalf("推送载荷名称应为「只改文档」，got %q", p.Name)
	}
	if rep.Pushed != 1 {
		t.Fatalf("报告应记 1 条推送成功，got %+v", rep)
	}
	if e.Bind.DocHashes[d.UID] == "" {
		t.Fatal("推送成功后应记录 doc hash 基线（否则下一轮会全量重推）")
	}

	// 第二轮：内容没变 → 不该再推
	pushesBefore := f.pushCount()
	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("第二轮 pushDirty: %v", err)
	}
	if f.pushCount() != pushesBefore {
		t.Fatalf("文档未变化时不应重复推送，实际又发了 %d 次请求", f.pushCount()-pushesBefore)
	}
}
