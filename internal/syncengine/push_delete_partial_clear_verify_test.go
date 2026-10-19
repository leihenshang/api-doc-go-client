package syncengine

// QA 独立验证（P2-1）：墓碑按回执逐条清除。
//
// 与工程师 push_delete_partial_clear_test.go 互补，这里只覆盖他没测到的四个面：
//   ① 删除与非删除 op 混排：upsert 的 MarkSynced 不受删除回执影响，两类互不干扰；
//   ② 全部 rejected：列表原样保留（顺序/类型都不许变），下一轮继续重试，不得误删；
//   ③ 旧格式（无类型）文档墓碑：按 delete/doc 推出，成功后 .sync-deletes.json 必须消失，
//      且下一轮零推送 —— 这条是「按 pendingTypes 而不是 typ 去删」的唯一可观测差异；
//   ④ conflict（非 ok 亦非 rejected）同样不得被清掉。

import (
	"os"
	"path/filepath"
	"testing"

	"api-doc-go-client/internal/collection"
)

// qa21PushCount 已收到的 push 请求数。
func qa21PushCount(f *fakeServer) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.pushes)
}

// qa21LastOps 最近一次 push 的 op 列表（没有则 nil）。
func qa21LastOps(f *fakeServer) []Op {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pushes) == 0 {
		return nil
	}
	return f.pushes[len(f.pushes)-1].Ops
}

// qa21Deletes 最近一次 push 里的 delete op（uid:type 形式，便于断言类型也对）。
func qa21Deletes(f *fakeServer) []string {
	var out []string
	for _, op := range qa21LastOps(f) {
		if op.Op == "delete" {
			out = append(out, op.UID+":"+op.Type)
		}
	}
	return out
}

// qa21Pending 当前待删列表（uid:type，按文件顺序）。
func qa21Pending(c *collection.Collection) []string {
	var out []string
	for _, d := range c.LoadPendingDeletes() {
		out = append(out, d.UID+":"+d.Type)
	}
	return out
}

// qa21Baseline 跑一轮把默认环境等推掉，返回此后基线计数。
func qa21Baseline(t *testing.T, f *fakeServer, c *collection.Collection, e *Engine) int {
	t.Helper()
	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("基线 pushDirty: %v", err)
	}
	if dirty, err := c.DirtyNodes(); err != nil {
		t.Fatalf("DirtyNodes: %v", err)
	} else if len(dirty) != 0 {
		t.Fatalf("基线后不应有脏节点，got %d", len(dirty))
	}
	if del := c.LoadPendingDeletes(); len(del) != 0 {
		t.Fatalf("基线后不应有墓碑，got %+v", del)
	}
	return qa21PushCount(f)
}

// qa21StatusByUID 按 uid 定制 delete op 的回执状态（未列出的 delete 一律 ok，upsert 一律 ok）。
func qa21StatusByUID(f *fakeServer, status map[string]string) {
	f.pushFn = func(r *PushReq) *PushResp {
		res := make([]OpResult, 0, len(r.Ops))
		for _, op := range r.Ops {
			st, ok := status[op.UID]
			if op.Op != "delete" || !ok {
				res = append(res, OpResult{OpID: op.OpID, Status: "ok", ItemRev: op.BaseRev + 1})
				continue
			}
			res = append(res, OpResult{OpID: op.OpID, Status: st, Reason: "测试指定状态"})
		}
		return &PushResp{Results: res, Cursor: f.state.Cursor}
	}
}

// TestQA21MixedUpsertAndDelete 同一批里既有 upsert 又有 delete：删除的回执只影响墓碑，
// upsert 照常 MarkSynced（不再 dirty），成功的那条删除被摘掉、被拒的留下。
func TestQA21MixedUpsertAndDelete(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)

	delOK, err := c.CreateRequest("", "要删掉的", "GET")
	if err != nil {
		t.Fatalf("CreateRequest(delOK): %v", err)
	}
	delBad, err := c.CreateRequest("", "会被拒的", "GET")
	if err != nil {
		t.Fatalf("CreateRequest(delBad): %v", err)
	}
	keep, err := c.CreateRequest("", "要改的", "GET")
	if err != nil {
		t.Fatalf("CreateRequest(keep): %v", err)
	}
	qa21Baseline(t, f, c, e)

	if err := c.DeleteRequest(delOK.UID); err != nil {
		t.Fatalf("DeleteRequest(delOK): %v", err)
	}
	if err := c.DeleteRequest(delBad.UID); err != nil {
		t.Fatalf("DeleteRequest(delBad): %v", err)
	}
	// 再造一个非删除脏节点：改 keep 的 URL（走 upsert 路径）。
	req, err := c.ReadRequest(keep.UID)
	if err != nil {
		t.Fatalf("ReadRequest(keep): %v", err)
	}
	req.URL = "/v1/changed"
	if err := c.SaveRequest(req); err != nil {
		t.Fatalf("SaveRequest(keep): %v", err)
	}
	if dirty, err := c.DirtyNodes(); err != nil {
		t.Fatalf("DirtyNodes: %v", err)
	} else if len(dirty) != 1 {
		t.Fatalf("前置条件：应恰好 1 个脏节点，got %d", len(dirty))
	}

	qa21StatusByUID(f, map[string]string{delBad.UID: "rejected"})
	base := qa21PushCount(f)
	rep := &Report{}
	if err := e.pushDirty(rep); err != nil {
		t.Fatalf("pushDirty: %v", err)
	}
	if qa21PushCount(f) != base+1 {
		t.Fatalf("应恰好 1 次 Push，got %d", qa21PushCount(f)-base)
	}

	ops := qa21LastOps(f)
	if len(ops) != 3 {
		t.Fatalf("本轮应 3 条 op（1 upsert + 2 delete），got %d: %+v", len(ops), ops)
	}
	// 回执计数：upsert ok + delete ok = 2 passed，1 rejected。
	if rep.Pushed != 2 || rep.Rejected != 1 {
		t.Fatalf("应 2 成功 1 被拒，got %+v", rep)
	}

	// ① upsert 正常 MarkSynced：keep 不再是脏节点（删除回执没有把它带偏）。
	if dirty, err := c.DirtyNodes(); err != nil {
		t.Fatalf("DirtyNodes(推送后): %v", err)
	} else if len(dirty) != 0 {
		t.Fatalf("upsert 成功后不应再有脏节点，got %+v", dirty)
	}

	// ② 墓碑：只剩被拒那条，类型仍是 api。
	if got := qa21Pending(c); len(got) != 1 || got[0] != delBad.UID+":"+collection.PendingDeleteAPI {
		t.Fatalf("待删列表应只剩 %s:api，got %+v", delBad.UID, got)
	}

	// ③ 下一轮：只有被拒的 delete，已成功的不得重推，upsert 也不得重推。
	before := qa21PushCount(f)
	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("pushDirty(第二轮): %v", err)
	}
	if qa21PushCount(f) != before+1 {
		t.Fatalf("第二轮应仍发起 1 次 Push（重试被拒项），got %d", qa21PushCount(f)-before)
	}
	second := qa21LastOps(f)
	if len(second) != 1 || second[0].Op != "delete" || second[0].UID != delBad.UID {
		t.Fatalf("第二轮只应重推 delete/%s，got %+v", delBad.UID, second)
	}
}

// TestQA21AllRejectedKeepsList 全部 rejected：列表原样保留（顺序与类型都不变），下一轮继续重试。
func TestQA21AllRejectedKeepsList(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)

	r1, err := c.CreateRequest("", "拒一", "GET")
	if err != nil {
		t.Fatalf("CreateRequest(r1): %v", err)
	}
	r2, err := c.CreateRequest("", "拒二", "GET")
	if err != nil {
		t.Fatalf("CreateRequest(r2): %v", err)
	}
	qa21Baseline(t, f, c, e)

	if err := c.DeleteRequest(r1.UID); err != nil {
		t.Fatalf("DeleteRequest(r1): %v", err)
	}
	if err := c.DeleteRequest(r2.UID); err != nil {
		t.Fatalf("DeleteRequest(r2): %v", err)
	}
	seed := qa21Pending(c)
	if len(seed) != 2 {
		t.Fatalf("前置条件：应 2 条墓碑，got %+v", seed)
	}

	qa21StatusByUID(f, map[string]string{r1.UID: "rejected", r2.UID: "rejected"})
	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("pushDirty(第一轮): %v", err)
	}

	// 一条都不许清：内容与顺序都原样（顺序错乱会让重试语义漂移）。
	after := qa21Pending(c)
	if len(after) != len(seed) {
		t.Fatalf("全部被拒后待删列表不得变短，want %+v got %+v", seed, after)
	}
	for i := range seed {
		if after[i] != seed[i] {
			t.Fatalf("全部被拒后待删列表必须原样保留（含顺序），want %+v got %+v", seed, after)
		}
	}

	// 下一轮继续重试两条（不得因为「什么也没成功」就丢弃）。
	before := qa21PushCount(f)
	rep := &Report{}
	if err := e.pushDirty(rep); err != nil {
		t.Fatalf("pushDirty(第二轮): %v", err)
	}
	if qa21PushCount(f) != before+1 {
		t.Fatalf("第二轮应继续重试（1 次 Push），got %d", qa21PushCount(f)-before)
	}
	if got := qa21Deletes(f); len(got) != 2 {
		t.Fatalf("第二轮应重推 2 条 delete，got %+v", got)
	}
	if rep.Rejected != 2 {
		t.Fatalf("第二轮应 2 条被拒，got %+v", rep)
	}
	if after2 := qa21Pending(c); len(after2) != 2 {
		t.Fatalf("重试后列表仍应保留 2 条，got %+v", after2)
	}
}

// TestQA21LegacyDocTombstoneClearedByPendingType 旧格式（["<docUid>"]）文档墓碑：
// 出网类型被基线纠正为 doc，但文件里存的类型是 api（缺省口径），逐条清除必须按后者删，
// 否则那条旧条目永远摘不掉、每轮重推。
func TestQA21LegacyDocTombstoneClearedByPendingType(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)

	d, err := c.CreateDoc("升级前删的文档", "", "")
	if err != nil {
		t.Fatalf("CreateDoc: %v", err)
	}
	qa21Baseline(t, f, c, e)
	if e.Bind.DocHashes[d.UID] == "" {
		t.Fatal("前置条件：该文档应已建立推送 hash 基线")
	}

	// 模拟升级前遗留状态：文件已被删，.sync-deletes.json 只留 uid 字符串（无类型）。
	full := filepath.Join(c.Dir, filepath.FromSlash(d.Path))
	if err := os.Remove(full); err != nil {
		t.Fatalf("移除文档文件: %v", err)
	}
	legacyPath := filepath.Join(c.Dir, ".sync-deletes.json")
	if err := os.WriteFile(legacyPath, []byte("[\n  \""+d.UID+"\"\n]\n"), 0o644); err != nil {
		t.Fatalf("写旧格式墓碑: %v", err)
	}
	if got := qa21Pending(c); len(got) != 1 || got[0] != d.UID+":"+collection.PendingDeleteAPI {
		t.Fatalf("前置条件：旧格式应读成 %s:api，got %+v", d.UID, got)
	}

	base := qa21PushCount(f)
	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("pushDirty: %v", err)
	}
	if qa21PushCount(f) != base+1 {
		t.Fatalf("应发起 1 次 Push，got %d", qa21PushCount(f)-base)
	}
	// 出网类型是纠正后的 doc（P1-10 迁移路径）。
	if got := qa21Deletes(f); len(got) != 1 || got[0] != d.UID+":doc" {
		t.Fatalf("旧格式文档墓碑应推出 delete/doc/%s，got %+v", d.UID, got)
	}
	// 核心：文件必须消失（按 typ 去删的话这里会残留 api 条目）。
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("推送成功后 .sync-deletes.json 必须被删除，stat err=%v，当前内容=%v", err, qa21Pending(c))
	}
	if got := qa21Pending(c); len(got) != 0 {
		t.Fatalf("推送成功后待删列表必须为空，got %+v", got)
	}
	// 幂等：下一轮零 Push（残留条目会以新 op_id 重推，服务端幂等失效）。
	before := qa21PushCount(f)
	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("pushDirty(第二轮): %v", err)
	}
	if got := qa21PushCount(f); got != before {
		t.Fatalf("墓碑已清，第二轮不应再 Push，多发 %d 次 ops=%+v", got-before, qa21LastOps(f))
	}
}

// TestQA21ConflictDeleteKeptAndRetried conflict 状态（非 ok）同样不得清墓碑。
func TestQA21ConflictDeleteKeptAndRetried(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)

	r, err := c.CreateRequest("", "冲突删除", "GET")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	qa21Baseline(t, f, c, e)
	if err := c.DeleteRequest(r.UID); err != nil {
		t.Fatalf("DeleteRequest: %v", err)
	}

	qa21StatusByUID(f, map[string]string{r.UID: "conflict"})
	rep := &Report{}
	if err := e.pushDirty(rep); err != nil {
		t.Fatalf("pushDirty: %v", err)
	}
	if rep.Conflicts != 1 || rep.Pushed != 0 {
		t.Fatalf("应记 1 条冲突、0 条成功，got %+v", rep)
	}
	if got := qa21Pending(c); len(got) != 1 || got[0] != r.UID+":"+collection.PendingDeleteAPI {
		t.Fatalf("冲突的删除必须留在待删列表里重试，got %+v", got)
	}

	// 下一轮（这次 ok）：重试成功后清掉。
	f.pushFn = nil
	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("pushDirty(重试): %v", err)
	}
	if got := qa21Pending(c); len(got) != 0 {
		t.Fatalf("重试成功后待删列表应为空，got %+v", got)
	}
}
