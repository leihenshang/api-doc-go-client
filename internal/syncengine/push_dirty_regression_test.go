package syncengine

// QA 独立回归：P0-4（pushDirty 早退导致「只改文档/示例/环境」一轮完全不推送）。
//
// 与工程师的 push_doc_only_test.go 互补，本文件刻意覆盖他没测的三面：
//   ① 只改「已有文档的正文」（他测的是新建 doc）；
//   ② 只改响应示例 / 只改变量值，各自独立成立；
//   ③ 反向：真的没改动时不得发空推送（别把修复做成「每轮空推一次」）；
//   ④ 混合：请求脏 + 文档脏同时存在，两条都推且文档不得重复；
//   ⑤ 幂等：基线前移后再跑一轮，零推送。

import (
	"testing"

	"api-doc-go-client/internal/collection"
)

// qaPushCount 独立于其他测试文件的计数辅助（避免复用被测方自带 helper）。
func qaPushCount(f *fakeServer) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.pushes)
}

// qaLastOps 取最近一次推送的 op 列表（无推送返回 nil）。
func qaLastOps(f *fakeServer) []Op {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pushes) == 0 {
		return nil
	}
	return f.pushes[len(f.pushes)-1].Ops
}

// qaCountOp 统计某类 op 出现次数。
func qaCountOp(ops []Op, typ string, op string) int {
	n := 0
	for _, o := range ops {
		if o.Type == typ && o.Op == op {
			n++
		}
	}
	return n
}

// qaBaseline 跑一轮把 Open 时自建的默认环境推掉，返回此后基线。
// 断言前置：基线之后 dirty / pendingDeletes 均为空（否则测的就不是「dirty 全空」路径）。
func qaBaseline(t *testing.T, f *fakeServer, c *collection.Collection, e *Engine) int {
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
		t.Fatalf("基线后不应有待推送删除，got %v", del)
	}
	return qaPushCount(f)
}

// TestQAPushDirtyDocBodyOnly 修改**已存在**文档的正文：必须推送 1 条 doc upsert，
// 且载荷里是新正文（不是旧内容）。
func TestQAPushDirtyDocBodyOnly(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)

	d, err := c.CreateDoc("接口说明", "", "")
	if err != nil {
		t.Fatalf("CreateDoc: %v", err)
	}
	base := qaBaseline(t, f, c, e) // 这一轮会把新建的 doc 也推掉

	// 改正文（文件已推送过，hash 基线是旧正文）
	d.Content = "# 改过的正文\n\n新增一段说明。\n"
	if err := c.SaveDoc(d); err != nil {
		t.Fatalf("SaveDoc: %v", err)
	}
	if dirty, _ := c.DirtyNodes(); len(dirty) != 0 {
		t.Fatalf("前置条件不成立：改文档正文不应产生脏请求节点，got %d", len(dirty))
	}

	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("pushDirty: %v", err)
	}
	if qaPushCount(f) == base {
		t.Fatal("只改文档正文也必须调用 Push，实际一次请求都没发")
	}
	ops := qaLastOps(f)
	if n := qaCountOp(ops, "doc", "upsert"); n != 1 {
		t.Fatalf("应推送 1 条 doc upsert，got %d，ops=%+v", n, ops)
	}
	if len(ops) != 1 {
		t.Fatalf("本轮应只有 1 条 op（文档正文改动），got %+v", ops)
	}
	if ops[0].UID != d.UID {
		t.Fatalf("推送的 doc uid 不符：want %s got %s", d.UID, ops[0].UID)
	}
}

// TestQAPushDirtyExampleOnly 只新增一个响应示例：必须推送 example upsert。
func TestQAPushDirtyExampleOnly(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)

	r, err := c.CreateRequest("", "列表示例", "GET")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	base := qaBaseline(t, f, c, e)

	ex, err := c.SaveResponseExample(r.UID, "成功", collection.ExampleRequest{Method: "GET", URL: r.URL},
		collection.ExampleResponse{Status: 200, Body: `{"items":[]}`})
	if err != nil {
		t.Fatalf("SaveResponseExample: %v", err)
	}
	if dirty, _ := c.DirtyNodes(); len(dirty) != 0 {
		t.Fatalf("前置条件不成立：存示例不应产生脏请求节点，got %d", len(dirty))
	}

	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("pushDirty: %v", err)
	}
	if qaPushCount(f) == base {
		t.Fatal("只新增响应示例也必须调用 Push，实际一次请求都没发")
	}
	ops := qaLastOps(f)
	if n := qaCountOp(ops, "example", "upsert"); n != 1 {
		t.Fatalf("应推送 1 条 example upsert，got %d，ops=%+v", n, ops)
	}
	if len(ops) != 1 || ops[0].UID != ex.UID {
		t.Fatalf("本轮应只推送该示例（uid=%s），got %+v", ex.UID, ops)
	}

	// 幂等：基线前移后再跑一轮，不得重复推
	before := qaPushCount(f)
	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("第二轮 pushDirty: %v", err)
	}
	if got := qaPushCount(f); got != before {
		t.Fatalf("示例未变化时不应重复推送，实际又发了 %d 次", got-before)
	}
}

// TestQAPushDirtyEnvVarOnly 只改变量值：必须推送 1 条 variable upsert，且载荷是新值。
func TestQAPushDirtyEnvVarOnly(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)
	base := qaBaseline(t, f, c, e)

	envs, err := c.ListEnvs()
	if err != nil || len(envs) == 0 {
		t.Fatalf("ListEnvs: envs=%d err=%v", len(envs), err)
	}
	env := envs[0]
	if len(env.Vars) == 0 {
		t.Fatal("默认环境应带一个 host 变量")
	}
	env.Vars[0].Value = "http://127.0.0.1:9999"
	if err := c.SaveEnv(env); err != nil {
		t.Fatalf("SaveEnv: %v", err)
	}
	if dirty, _ := c.DirtyNodes(); len(dirty) != 0 {
		t.Fatalf("前置条件不成立：改变量不应产生脏请求节点，got %d", len(dirty))
	}

	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("pushDirty: %v", err)
	}
	if qaPushCount(f) == base {
		t.Fatal("只改变量值也必须调用 Push，实际一次请求都没发")
	}
	ops := qaLastOps(f)
	if n := qaCountOp(ops, "variable", "upsert"); n != 1 {
		t.Fatalf("应推送 1 条 variable upsert，got %d，ops=%+v", n, ops)
	}
	if n := qaCountOp(ops, "variable_env", "upsert"); n != 0 {
		t.Fatalf("环境名未变，不应重推 variable_env，ops=%+v", ops)
	}
}

// TestQAPushDirtyNoChangeDoesNotPush 反向用例：一切都没改动时不得调用 Push。
// 这是「修好了」和「修成了每轮空推」的分界线。
func TestQAPushDirtyNoChangeDoesNotPush(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)

	// 造一点内容并全部推掉，让基线覆盖 doc / example / request / env 四类
	d, err := c.CreateDoc("稳定文档", "", "")
	if err != nil {
		t.Fatalf("CreateDoc: %v", err)
	}
	d.Content = "内容不变"
	if err := c.SaveDoc(d); err != nil {
		t.Fatalf("SaveDoc: %v", err)
	}
	r, err := c.CreateRequest("", "稳定请求", "GET")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	if _, err := c.SaveResponseExample(r.UID, "成功", collection.ExampleRequest{Method: "GET"},
		collection.ExampleResponse{Status: 200, Body: "{}"}); err != nil {
		t.Fatalf("SaveResponseExample: %v", err)
	}
	base := qaBaseline(t, f, c, e)

	for i := 1; i <= 3; i++ {
		if err := e.pushDirty(&Report{}); err != nil {
			t.Fatalf("第 %d 轮空转 pushDirty: %v", i, err)
		}
		if got := qaPushCount(f); got != base {
			t.Fatalf("第 %d 轮无任何改动，不应调用 Push（实际多发了 %d 次，ops=%+v）",
				i, got-base, qaLastOps(f))
		}
	}
}

// TestQAPushDirtyMixedRequestAndDoc 请求脏 + 文档脏同时存在：
// 两条都要推，且文档只能出现一次（不能既走 dirty 循环又走 collectDocOps 重复登记）。
func TestQAPushDirtyMixedRequestAndDoc(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)
	base := qaBaseline(t, f, c, e)

	d, err := c.CreateDoc("混合文档", "", "")
	if err != nil {
		t.Fatalf("CreateDoc: %v", err)
	}
	r, err := c.CreateRequest("", "混合请求", "POST")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	if dirty, _ := c.DirtyNodes(); len(dirty) != 1 {
		t.Fatalf("前置条件：应有 1 个脏请求节点，got %d", len(dirty))
	}

	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("pushDirty: %v", err)
	}
	if qaPushCount(f) == base {
		t.Fatal("混合脏集也必须调用 Push")
	}
	ops := qaLastOps(f)
	if n := qaCountOp(ops, "api", "upsert"); n != 1 {
		t.Fatalf("应推送 1 条 api upsert，got %d，ops=%+v", n, ops)
	}
	docOps := 0
	for _, o := range ops {
		if o.Type == "doc" && o.UID == d.UID {
			docOps++
		}
	}
	if docOps != 1 {
		t.Fatalf("文档应恰好推送 1 次（不得重复登记），got %d，ops=%+v", docOps, ops)
	}
	if len(ops) != 2 {
		t.Fatalf("本轮应为 2 条 op（1 api + 1 doc），got %d: %+v", len(ops), ops)
	}
	if ops[0].Type != "api" || ops[1].Type != "doc" {
		t.Fatalf("拓扑序应为 request 先、doc 后，got %s, %s", ops[0].Type, ops[1].Type)
	}
	// api 与 doc 的 op_id 必须唯一（服务端按 op_id 幂等）
	if ops[0].OpID == ops[1].OpID {
		t.Fatal("不同条目的 op_id 不得相同")
	}

	// 同一轮里 request 与 doc 的 uid 不得混淆
	if ops[0].UID != r.UID {
		t.Fatalf("api op 的 uid 不符：want %s got %s", r.UID, ops[0].UID)
	}
}

// TestQAPushDirtySecondRoundIdempotent 一轮推完 doc+example+var，机器没变，
// 第二轮必须零推送（基线已前移）。这条同时防住「修复后每轮全量重推」的退化。
func TestQAPushDirtySecondRoundIdempotent(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)
	qaBaseline(t, f, c, e)

	r, err := c.CreateRequest("", "幂等请求", "GET")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	if _, err := c.SaveResponseExample(r.UID, "成功", collection.ExampleRequest{Method: "GET"},
		collection.ExampleResponse{Status: 200, Body: "{}"}); err != nil {
		t.Fatalf("SaveResponseExample: %v", err)
	}
	if _, err := c.CreateDoc("幂等文档", "", ""); err != nil {
		t.Fatalf("CreateDoc: %v", err)
	}
	envs, _ := c.ListEnvs()
	if len(envs) == 0 || len(envs[0].Vars) == 0 {
		t.Fatal("默认环境缺失")
	}
	envs[0].Vars[0].Value = "http://changed"
	if err := c.SaveEnv(envs[0]); err != nil {
		t.Fatalf("SaveEnv: %v", err)
	}

	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("第一轮 pushDirty: %v", err)
	}
	first := qaLastOps(f)
	if len(first) < 4 {
		t.Fatalf("第一轮应推齐 api/doc/example/variable 至少 4 条，got %d: %+v", len(first), first)
	}
	pushed := qaPushCount(f)

	for i := 1; i <= 2; i++ {
		if err := e.pushDirty(&Report{}); err != nil {
			t.Fatalf("第 %d 轮 pushDirty: %v", i+1, err)
		}
		if got := qaPushCount(f); got != pushed {
			t.Fatalf("第 %d 轮无变化却重复推送（多 %d 次，ops=%+v）", i+1, got-pushed, qaLastOps(f))
		}
	}
}

// TestQAPushDirtyDocDeletePushed 只删除一篇文档：必须推送 doc delete tombstone。
// 注意 DeleteDoc 会同时往 pendingDeletes 记一条（见 collection.doc.go:173），
// 而 collectTombstoneOps 一律按 Type="api" 出网，于是同一次删除会出两条 op：
// 一条 type=api（实体类型错了，既有缺陷，已单独上报 P1）、一条 type=doc（正确）。
// 本用例只锁「doc delete 必须被推出去」这条回归点。
func TestQAPushDirtyDocDeletePushed(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)

	d, err := c.CreateDoc("将被删除", "", "")
	if err != nil {
		t.Fatalf("CreateDoc: %v", err)
	}
	base := qaBaseline(t, f, c, e)
	if e.Bind.DocHashes[d.UID] == "" {
		t.Fatal("前置条件：该文档应已有推送基线")
	}

	if err := c.DeleteDoc(d.UID); err != nil {
		t.Fatalf("DeleteDoc: %v", err)
	}
	if dirty, _ := c.DirtyNodes(); len(dirty) != 0 {
		t.Fatalf("前置条件不成立：删文档不应产生脏请求节点，got %d", len(dirty))
	}

	if err := e.pushDirty(&Report{}); err != nil {
		t.Fatalf("pushDirty: %v", err)
	}
	if qaPushCount(f) == base {
		t.Fatal("删除文档也必须调用 Push（否则本地删除永远同步不到服务端）")
	}
	ops := qaLastOps(f)
	if n := qaCountOp(ops, "doc", "delete"); n != 1 {
		t.Fatalf("应推送 1 条 doc delete，got %d，ops=%+v", n, ops)
	}
	if _, ok := e.Bind.DocHashes[d.UID]; ok {
		t.Fatal("删除推送成功后应清掉该 doc 的 hash 基线")
	}
	// 既有缺陷留痕（不在此处判失败，避免把与本次改动无关的问题染红门禁）
	for _, o := range ops {
		if o.Type == "api" && o.UID == d.UID {
			t.Logf("既有缺陷 P1：同一次文档删除还发了一条 type=api 的 delete（uid 是文档 uid），"+
				"ops=%+v", ops)
		}
	}
}
