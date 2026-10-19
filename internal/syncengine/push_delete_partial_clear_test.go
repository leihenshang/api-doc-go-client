package syncengine

// P2-1 回归：墓碑（待推送删除）必须「按回执逐条清」，而不是「删除类操作全部成功才整文件清空」。
//
// 缺陷原状：pushDirty 末尾是「allOk 才 ClearPendingDeletes()」，而 ClearPendingDeletes 是整文件删除。
// 后果有两条，且第二条是真正的灾难：
//   ① 同批里只要有一条墓碑被服务端永久拒绝（rejected），整个待删列表永远清不掉；
//   ② 清不掉 → 下一轮把**同批已成功**的删除也用**新的 op_id** 再发一遍，
//      服务端按 op_id 做的幂等去重完全失效 → 已成功的删除被无限重推。
//
// 本文件锁两面：
//   ① 两条墓碑一 ok 一 rejected：本轮后列表只剩被拒那条；第二轮只重推被拒那条
//      （已成功的那条不得再出现，一次都不能再发）；
//   ② 全部 ok：列表文件直接消失（不留 "[]" 空壳），且下一轮不再推任何删除。

import (
	"os"
	"path/filepath"
	"testing"

	"api-doc-go-client/internal/collection"
)

// p21RejectDeleteOp 让服务端对指定 uid 的 delete op 回 rejected，其余一律 ok。
// 模拟「该条目在服务端被永久拒绝（需要人工介入）」的真实场景。
func p21RejectDeleteOp(f *fakeServer, rejectUID string) {
	f.pushFn = func(r *PushReq) *PushResp {
		res := make([]OpResult, 0, len(r.Ops))
		for _, op := range r.Ops {
			if op.Op == "delete" && op.UID == rejectUID {
				res = append(res, OpResult{OpID: op.OpID, Status: "rejected", Reason: "条目已锁定"})
				continue
			}
			res = append(res, OpResult{OpID: op.OpID, Status: "ok", ItemRev: op.BaseRev + 1})
		}
		return &PushResp{Results: res, Cursor: f.state.Cursor}
	}
}

// p21PendingUIDs 待删列表里的 uid 集合（顺序无关）。
func p21PendingUIDs(c *collection.Collection) map[string]string {
	out := map[string]string{}
	for _, d := range c.LoadPendingDeletes() {
		out[d.UID] = d.Type
	}
	return out
}

// p21DeleteUIDs 一次 push 里出现的 delete op 的 uid（只看删除）。
func p21DeleteUIDs(ops []Op) []string {
	var out []string
	for _, op := range ops {
		if op.Op == "delete" {
			out = append(out, op.UID)
		}
	}
	return out
}

// TestP21RejectedTombstoneDoesNotBlockOthers 一条被拒不得拖死同批已成功的删除。
func TestP21RejectedTombstoneDoesNotBlockOthers(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)

	// 建两个请求并推一轮基线，让它们有服务端身份（本地删除才能产生墓碑）。
	okReq, err := c.CreateRequest("", "能删掉的", "GET")
	if err != nil {
		t.Fatalf("CreateRequest(ok): %v", err)
	}
	badReq, err := c.CreateRequest("", "会被拒的", "GET")
	if err != nil {
		t.Fatalf("CreateRequest(rejected): %v", err)
	}
	p110Baseline(t, f, c, e)

	// 本地删掉两者 → 两条 api 墓碑。
	if err := c.DeleteRequest(okReq.UID); err != nil {
		t.Fatalf("DeleteRequest(ok): %v", err)
	}
	if err := c.DeleteRequest(badReq.UID); err != nil {
		t.Fatalf("DeleteRequest(rejected): %v", err)
	}
	if pend := c.LoadPendingDeletes(); len(pend) != 2 {
		t.Fatalf("前置条件：应有 2 条墓碑，got %d: %+v", len(pend), pend)
	}

	p21RejectDeleteOp(f, badReq.UID)
	base := p110PushCount(f)
	rep := &Report{}
	if err := e.pushDirty(rep); err != nil {
		t.Fatalf("pushDirty(第一轮): %v", err)
	}
	if p110PushCount(f) != base+1 {
		t.Fatalf("应恰好调用 1 次 Push，got %d", p110PushCount(f)-base)
	}
	ops := p110LastOps(t, f)
	if len(ops) != 2 {
		t.Fatalf("第一轮应推送 2 条 delete，got %d: %+v", len(ops), ops)
	}
	// 回执统计口径不变：一 ok 一 rejected。
	if rep.Pushed != 1 || rep.Rejected != 1 {
		t.Fatalf("应 1 条成功 1 条被拒，got %+v", rep)
	}

	// 核心断言①：只剩被拒那条留在待删列表里。
	got := p21PendingUIDs(c)
	if len(got) != 1 {
		t.Fatalf("待删列表应只剩被拒的那条，got %+v", c.LoadPendingDeletes())
	}
	if typ, ok := got[badReq.UID]; !ok {
		t.Fatalf("被拒的 %s 必须留在待删列表里继续重试，got %+v", badReq.UID, c.LoadPendingDeletes())
	} else if typ != collection.PendingDeleteAPI {
		t.Fatalf("被拒条目类型应为 api，got %q", typ)
	}

	// 核心断言②：第二轮只重推被拒那条，已成功的那条不得再出现。
	before := p110PushCount(f)
	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("pushDirty(第二轮): %v", err)
	}
	if p110PushCount(f) != before+1 {
		t.Fatalf("第二轮应仍为被拒条目发起 1 次 Push，got %d", p110PushCount(f)-before)
	}
	second := p21DeleteUIDs(p110LastOps(t, f))
	if len(second) != 1 || second[0] != badReq.UID {
		t.Fatalf("第二轮只应重推被拒的 %s，got %+v（已成功的 %s 不得再推）", badReq.UID, second, okReq.UID)
	}

	// 第三轮同样：被拒条目继续重试是正确的（需要人工介入），但不能带上别的一起推。
	before = p110PushCount(f)
	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("pushDirty(第三轮): %v", err)
	}
	if third := p21DeleteUIDs(p110LastOps(t, f)); len(third) != 1 || third[0] != badReq.UID {
		t.Fatalf("第三轮仍只应重推被拒的 %s，got %+v", badReq.UID, third)
	}
	if p110PushCount(f) != before+1 {
		t.Fatalf("第三轮应 1 次 Push，got %d", p110PushCount(f)-before)
	}
}

// TestP21AllTombstonesClearedDeletesFile 全部 ok：列表文件消失，且下一轮不再推任何删除。
func TestP21AllTombstonesClearedDeletesFile(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)

	r1, err := c.CreateRequest("", "删一", "GET")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	r2, err := c.CreateRequest("", "删二", "POST")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	p110Baseline(t, f, c, e)

	if err := c.DeleteRequest(r1.UID); err != nil {
		t.Fatalf("DeleteRequest: %v", err)
	}
	if err := c.DeleteRequest(r2.UID); err != nil {
		t.Fatalf("DeleteRequest: %v", err)
	}
	if pend := c.LoadPendingDeletes(); len(pend) != 2 {
		t.Fatalf("前置条件：应有 2 条墓碑，got %d: %+v", len(pend), pend)
	}

	base := p110PushCount(f)
	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("pushDirty: %v", err)
	}
	if p110PushCount(f) != base+1 {
		t.Fatalf("应调用 1 次 Push，got %d", p110PushCount(f)-base)
	}
	if ops := p21DeleteUIDs(p110LastOps(t, f)); len(ops) != 2 {
		t.Fatalf("应推送 2 条 delete，got %+v", ops)
	}

	// 全部成功后文件必须消失：留下 "[]" 或残留条目都会让下一轮白推。
	if list := c.LoadPendingDeletes(); len(list) != 0 {
		t.Fatalf("全部推送成功后待删列表应为空，got %+v", list)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, ".sync-deletes.json")); !os.IsNotExist(err) {
		t.Fatalf(".sync-deletes.json 应被删除（与 ClearPendingDeletes 口径一致），stat err=%v", err)
	}

	// 幂等：下一轮不得再推任何 delete。
	before := p110PushCount(f)
	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("第二轮 pushDirty: %v", err)
	}
	if p110PushCount(f) != before {
		if ops := p21DeleteUIDs(p110LastOps(t, f)); len(ops) != 0 {
			t.Fatalf("第二轮不应再推任何 delete，got %+v", ops)
		}
	}
}

// TestP21FoldedDuplicateTombstoneEmitsSingleDocDelete 异常数据：同一 uid 在待删列表里
// 既有 api 又有 doc 两条，且该 uid 命中 DocHashes。类型纠正会把两条都纠正成 doc，
// 若不去重就会在一次 push 里发出两条一模一样的 delete/doc/<uid>。
// 要求：只发 1 条；且该条成功后两条墓碑一起被摘掉（否则留下的那条永远推不成功 → 又变无限重推）。
func TestP21FoldedDuplicateTombstoneEmitsSingleDocDelete(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)

	d, err := c.CreateDoc("又是接口又是文档的异常 uid", "", "")
	if err != nil {
		t.Fatalf("CreateDoc: %v", err)
	}
	p110Baseline(t, f, c, e) // 建立 DocHashes 基线
	if e.Bind.DocHashes[d.UID] == "" {
		t.Fatal("前置条件：该文档应已有推送 hash 基线（类型纠正的前提）")
	}

	// 模拟被外部工具写坏的待删列表：同一 uid 的 api 与 doc 各一条。
	if err := c.SavePendingDeletes([]collection.PendingDelete{
		{UID: d.UID, Type: collection.PendingDeleteAPI},
		{UID: d.UID, Type: collection.PendingDeleteDoc},
	}); err != nil {
		t.Fatalf("SavePendingDeletes: %v", err)
	}
	// 文件也删掉，贴近「文档确实在本地被删除」的真实状态（走的是墓碑而不是差异检测）。
	if err := os.Remove(filepath.Join(c.Dir, filepath.FromSlash(d.Path))); err != nil {
		t.Fatalf("移除文档文件: %v", err)
	}

	base := p110PushCount(f)
	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("pushDirty: %v", err)
	}
	if p110PushCount(f) != base+1 {
		t.Fatalf("应调用 1 次 Push，got %d", p110PushCount(f)-base)
	}
	ops := p110LastOps(t, f)
	if len(ops) != 1 {
		t.Fatalf("纠正后撞成同一条的墓碑只能发 1 条 op，got %d: %+v", len(ops), ops)
	}
	if ops[0].Op != "delete" || ops[0].Type != collection.PendingDeleteDoc || ops[0].UID != d.UID {
		t.Fatalf("应推送 1 条 delete/doc/%s，got %+v", d.UID, ops[0])
	}
	// 折叠的两条墓碑必须一起被摘掉：留一条下来会永远推不成功。
	if list := c.LoadPendingDeletes(); len(list) != 0 {
		t.Fatalf("折叠的两条墓碑应随该 op 成功后一起被摘掉，got %+v", list)
	}

	// 幂等：下一轮不得再推。
	before := p110PushCount(f)
	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("第二轮 pushDirty: %v", err)
	}
	if got := p110PushCount(f); got != before {
		t.Fatalf("已被折叠推送成功后不应再 Push（多发了 %d 次，ops=%+v）", got-before, p110LastOps(t, f))
	}
}
