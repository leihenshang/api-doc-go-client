package syncengine

// P1-10 回归：一次删除只能发一条 delete op，且实体类型必须对。
//
// 缺陷原状：DeleteDoc 记一条只有 uid 的墓碑，collectTombstoneOps 一律按 Type="api" 出网，
// 同一轮 collectDocOps 的墓碑分支又按 Type="doc" 出一条 → 一次删除发出
// delete/api/<docUid> + delete/doc/<docUid>；服务端对未知 api uid 回 rejected，
// applyPushResults 得到 allOk=false，ClearPendingDeletes 不执行，于是每轮重复推送并报错。
//
// 本文件锁四面：
//   ① DeleteDoc → 恰好 1 条 doc delete（且待删列表被清空、不重复推送）；
//   ② DeleteRequest → 恰好 1 条 api delete（别把接口删除弄丢或改成 doc）；
//   ③ 旧格式 .sync-deletes.json（["uid"]）→ 仍按 api 推出（向后兼容）；
//   ④ 文件被外部删除（不走 DeleteDoc）→ 仍由 collectDocOps 出 1 条 doc delete（防回归）。

import (
	"os"
	"path/filepath"
	"testing"

	"api-doc-go-client/internal/collection"
)

// p110PushCount 已收到的 push 请求数。
func p110PushCount(f *fakeServer) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.pushes)
}

// p110LastOps 最近一次 push 携带的 op 列表。
func p110LastOps(t *testing.T, f *fakeServer) []Op {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pushes) == 0 {
		t.Fatal("服务端没收到任何 push 请求")
	}
	return f.pushes[len(f.pushes)-1].Ops
}

// p110Baseline 先跑一轮把集合自带的默认环境/变量推掉，并断言此后既无脏节点也无待推送删除，
// 这样后面那一轮推送里的 op 就只可能来自被测的删除路径。
func p110Baseline(t *testing.T, f *fakeServer, c *collection.Collection, e *Engine) int {
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
		t.Fatalf("基线后不应有待推送删除，got %+v", del)
	}
	return p110PushCount(f)
}

// TestP110DeleteDocEmitsSingleDocOp 删除一篇已推送过的文档：恰好 1 条 delete，Type=="doc"。
func TestP110DeleteDocEmitsSingleDocOp(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)

	d, err := c.CreateDoc("待删文档", "", "")
	if err != nil {
		t.Fatalf("CreateDoc: %v", err)
	}
	base := p110Baseline(t, f, c, e)
	if e.Bind.DocHashes[d.UID] == "" {
		t.Fatal("前置条件：该文档应已有推送 hash 基线")
	}

	if err := c.DeleteDoc(d.UID); err != nil {
		t.Fatalf("DeleteDoc: %v", err)
	}
	// 前置条件：墓碑带 doc 类型（这正是本次修复的核心——墓碑必须记住实体类型）
	pend := c.LoadPendingDeletes()
	if len(pend) != 1 {
		t.Fatalf("DeleteDoc 应只记 1 条墓碑，got %d: %+v", len(pend), pend)
	}
	if pend[0].UID != d.UID || pend[0].Type != collection.PendingDeleteDoc {
		t.Fatalf("墓碑应为 {uid:%s type:doc}，got %+v", d.UID, pend[0])
	}

	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("pushDirty: %v", err)
	}
	if p110PushCount(f) == base {
		t.Fatal("删除文档必须调用 Push")
	}
	ops := p110LastOps(t, f)
	if len(ops) != 1 {
		t.Fatalf("一次文档删除必须恰好 1 条 op（P1-10：不能同时发 api 与 doc），got %d: %+v", len(ops), ops)
	}
	if ops[0].Op != "delete" || ops[0].Type != "doc" || ops[0].UID != d.UID {
		t.Fatalf("应推送 delete/doc/%s，got %+v", d.UID, ops[0])
	}
	// 待删列表必须被清掉：留着就会每轮重复推送并报错
	if del := c.LoadPendingDeletes(); len(del) != 0 {
		t.Fatalf("删除推送成功后必须清空待删列表，got %+v", del)
	}
	if _, ok := e.Bind.DocHashes[d.UID]; ok {
		t.Fatal("删除推送成功后应清掉该 doc 的 hash 基线")
	}

	// 幂等：下一轮不得再推
	before := p110PushCount(f)
	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("第二轮 pushDirty: %v", err)
	}
	if got := p110PushCount(f); got != before {
		t.Fatalf("删除已推完，第二轮不应再 Push（多发了 %d 次，ops=%+v）", got-before, p110LastOps(t, f))
	}
}

// TestP110DeleteRequestEmitsSingleApiOp 删除一个请求：恰好 1 条 delete，Type=="api"。
// 防「修 doc 删重复时顺手把接口删除改成 doc 类型或者弄丢」。
func TestP110DeleteRequestEmitsSingleApiOp(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)

	r, err := c.CreateRequest("", "待删请求", "GET")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	base := p110Baseline(t, f, c, e)

	if err := c.DeleteRequest(r.UID); err != nil {
		t.Fatalf("DeleteRequest: %v", err)
	}
	pend := c.LoadPendingDeletes()
	if len(pend) != 1 || pend[0].UID != r.UID || pend[0].Type != collection.PendingDeleteAPI {
		t.Fatalf("DeleteRequest 应记 {uid:%s type:api} 一条墓碑，got %+v", r.UID, pend)
	}

	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("pushDirty: %v", err)
	}
	if p110PushCount(f) == base {
		t.Fatal("删除请求必须调用 Push")
	}
	ops := p110LastOps(t, f)
	if len(ops) != 1 {
		t.Fatalf("一次请求删除必须恰好 1 条 op，got %d: %+v", len(ops), ops)
	}
	if ops[0].Op != "delete" || ops[0].Type != "api" || ops[0].UID != r.UID {
		t.Fatalf("应推送 delete/api/%s，got %+v", r.UID, ops[0])
	}
	if del := c.LoadPendingDeletes(); len(del) != 0 {
		t.Fatalf("删除推送成功后必须清空待删列表，got %+v", del)
	}
}

// TestP110LegacyPendingDeletesPushedAsAPI 旧格式兼容：.sync-deletes.json 内容是
// ["uid1","uid2"]（修复前的写法）时，仍然按 api 类型把两条删除推出去。
func TestP110LegacyPendingDeletesPushedAsAPI(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)
	p110Baseline(t, f, c, e)

	// 直接写旧格式文件，模拟「升级前留下的待删列表」
	legacy := []byte("[\n  \"legacy-uid-1\",\n  \"legacy-uid-2\"\n]\n")
	if err := os.WriteFile(filepath.Join(c.Dir, ".sync-deletes.json"), legacy, 0o644); err != nil {
		t.Fatalf("写旧格式待删列表: %v", err)
	}
	pend := c.LoadPendingDeletes()
	if len(pend) != 2 {
		t.Fatalf("旧格式应能读出 2 条，got %d: %+v", len(pend), pend)
	}
	for _, d := range pend {
		if d.Type != collection.PendingDeleteAPI {
			t.Fatalf("旧格式条目必须回落为 api 类型，got %+v", d)
		}
	}

	base := p110PushCount(f)
	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("pushDirty: %v", err)
	}
	if p110PushCount(f) == base {
		t.Fatal("旧格式待删列表也必须被推送出去（不能因为升级就丢掉删除）")
	}
	ops := p110LastOps(t, f)
	if len(ops) != 2 {
		t.Fatalf("应推送 2 条 delete，got %d: %+v", len(ops), ops)
	}
	seen := map[string]bool{}
	for _, o := range ops {
		if o.Op != "delete" || o.Type != "api" {
			t.Fatalf("旧格式条目应推出 delete/api，got %+v", o)
		}
		seen[o.UID] = true
	}
	if !seen["legacy-uid-1"] || !seen["legacy-uid-2"] {
		t.Fatalf("两条旧 uid 都应被推送，got %+v", ops)
	}
	if del := c.LoadPendingDeletes(); len(del) != 0 {
		t.Fatalf("推送成功后应清空待删列表，got %+v", del)
	}
}

// TestP110LegacyPendingDeleteForDocTreatedAsDoc 迁移路径：升级前用户刚好删了一篇文档，
// 旧格式墓碑里只有 uid（["<docUid>"]，无类型）。反序列化按旧口径得到 api，但真按 api 出网
// 服务端会以「未知 api uid」拒绝 → allOk=false → pendingDeletes 依旧清不掉（P1-10 换条路回来）。
// 引擎必须用文档基线兜底判成 doc：恰好 1 条 delete/doc，且推送成功后待删列表被清空。
func TestP110LegacyPendingDeleteForDocTreatedAsDoc(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)

	d, err := c.CreateDoc("升级前删掉的文档", "", "")
	if err != nil {
		t.Fatalf("CreateDoc: %v", err)
	}
	p110Baseline(t, f, c, e) // 推掉并建立 DocHashes 基线
	if e.Bind.DocHashes[d.UID] == "" {
		t.Fatal("前置条件：该文档应已有推送 hash 基线")
	}

	// 模拟升级前的遗留状态：文件已被删（旧版 DeleteDoc 也会删文件），墓碑只留 uid 字符串
	full := filepath.Join(c.Dir, filepath.FromSlash(d.Path))
	if err := os.Remove(full); err != nil {
		t.Fatalf("移除文档文件 %s: %v", full, err)
	}
	legacy := "[\n  \"" + d.UID + "\"\n]\n"
	if err := os.WriteFile(filepath.Join(c.Dir, ".sync-deletes.json"), []byte(legacy), 0o644); err != nil {
		t.Fatalf("写旧格式待删列表: %v", err)
	}
	// 前置条件：反序列化侧只做「旧数据一律 api」的兼容，类型纠正必须发生在引擎侧
	pend := c.LoadPendingDeletes()
	if len(pend) != 1 || pend[0].UID != d.UID || pend[0].Type != collection.PendingDeleteAPI {
		t.Fatalf("旧格式应反序列化为 {uid:%s type:api}，got %+v", d.UID, pend)
	}

	base := p110PushCount(f)
	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("pushDirty: %v", err)
	}
	if p110PushCount(f) == base {
		t.Fatal("旧格式文档墓碑也必须被推送出去（否则删除永远同步不到服务端）")
	}
	ops := p110LastOps(t, f)
	if len(ops) != 1 {
		t.Fatalf("旧格式文档墓碑应恰好 1 条 op，got %d: %+v", len(ops), ops)
	}
	if ops[0].Op != "delete" || ops[0].Type != "doc" || ops[0].UID != d.UID {
		t.Fatalf("命中文档基线的墓碑必须按 delete/doc/%s 推送，got %+v", d.UID, ops[0])
	}
	if del := c.LoadPendingDeletes(); len(del) != 0 {
		t.Fatalf("推送成功后必须清空待删列表（否则每轮重复推送并报错），got %+v", del)
	}
	if _, ok := e.Bind.DocHashes[d.UID]; ok {
		t.Fatal("删除推送成功后应清掉该 doc 的 hash 基线")
	}
}

// TestP110ExternalDocRemovalStillEmitsDocDelete 文档文件被用户在资源管理器里直接删掉
// （没走 DeleteDoc，待删列表里没有它）：仍必须由 collectDocOps 基于 Bind.DocHashes 出 1 条 doc delete。
// 这条锁住「修复去重时不要把差异检测那段删掉」。
func TestP110ExternalDocRemovalStillEmitsDocDelete(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)

	d, err := c.CreateDoc("外部删除", "", "")
	if err != nil {
		t.Fatalf("CreateDoc: %v", err)
	}
	base := p110Baseline(t, f, c, e)
	if e.Bind.DocHashes[d.UID] == "" {
		t.Fatal("前置条件：该文档应已有推送 hash 基线")
	}

	full := filepath.Join(c.Dir, filepath.FromSlash(d.Path))
	if err := os.Remove(full); err != nil {
		t.Fatalf("外部删除文档文件 %s: %v", full, err)
	}
	// 前置条件：这条路径绕过了 DeleteDoc，所以待删列表为空 —— 删除只能靠差异检测发现
	if del := c.LoadPendingDeletes(); len(del) != 0 {
		t.Fatalf("前置条件不成立：外部删除不应产生墓碑，got %+v", del)
	}

	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("pushDirty: %v", err)
	}
	if p110PushCount(f) == base {
		t.Fatal("文档被外部删除也必须推送 delete（否则服务端永远留着这篇文档）")
	}
	ops := p110LastOps(t, f)
	if len(ops) != 1 {
		t.Fatalf("外部删除应恰好推送 1 条 op，got %d: %+v", len(ops), ops)
	}
	if ops[0].Op != "delete" || ops[0].Type != "doc" || ops[0].UID != d.UID {
		t.Fatalf("应推送 delete/doc/%s，got %+v", d.UID, ops[0])
	}
	if _, ok := e.Bind.DocHashes[d.UID]; ok {
		t.Fatal("删除推送成功后应清掉该 doc 的 hash 基线（否则每轮重复推）")
	}
}
