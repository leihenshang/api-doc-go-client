package syncengine

// 引擎级端到端测试（P0-4）：用 httptest 起一个假的同步服务端，真实驱动
// Engine.RunOne 走「协商 → 拉取 → 应用落盘 → 推送 → 处理回执」全链路。
//
// 这里刻意不做单元级打桩：协议协商、cursor/gap 降级、冲突副本、脏检测这些
// 分支只有把整条链路跑起来才验证得到，也正是之前覆盖率 11% 时最大的空洞。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"api-doc-go-client/internal/collection"
)

// fakeServer 假同步服务端：按测试需要返回 meta/state/snapshot/changes/push。
type fakeServer struct {
	mu       sync.Mutex
	meta     Meta
	state    State
	snapshot Snapshot
	changes  Changes
	pushFn   func(*PushReq) *PushResp
	pushes   []*PushReq
	// 断言用：记录被请求过的路径
	paths []string
}

func newFakeServer(t *testing.T) (*fakeServer, *Client) {
	t.Helper()
	f := &fakeServer{
		meta:  Meta{ServerVersion: "test", SyncProto: 1, Features: []string{"sync", "change_log"}},
		state: State{Cursor: 0},
	}
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, data any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "success", "data": data})
	}
	mux.HandleFunc("/api/meta", func(w http.ResponseWriter, r *http.Request) {
		f.record(r.URL.Path)
		write(w, f.meta)
	})
	mux.HandleFunc("/api/sync/state", func(w http.ResponseWriter, r *http.Request) {
		f.record(r.URL.Path)
		write(w, f.state)
	})
	mux.HandleFunc("/api/sync/snapshot", func(w http.ResponseWriter, r *http.Request) {
		f.record(r.URL.Path)
		write(w, f.snapshot)
	})
	mux.HandleFunc("/api/sync/changes", func(w http.ResponseWriter, r *http.Request) {
		f.record(r.URL.Path)
		write(w, f.changes)
	})
	mux.HandleFunc("/api/sync/push", func(w http.ResponseWriter, r *http.Request) {
		f.record(r.URL.Path)
		var req PushReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.pushes = append(f.pushes, &req)
		fn := f.pushFn
		f.mu.Unlock()
		if fn == nil {
			// 默认全部 ok
			res := make([]OpResult, 0, len(req.Ops))
			for _, op := range req.Ops {
				res = append(res, OpResult{OpID: op.OpID, Status: "ok", ItemRev: op.BaseRev + 1})
			}
			write(w, PushResp{Results: res, Cursor: f.state.Cursor})
			return
		}
		write(w, fn(&req))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return f, New(srv.URL, "test-token", "dev-1")
}

func (f *fakeServer) record(p string) {
	f.mu.Lock()
	f.paths = append(f.paths, p)
	f.mu.Unlock()
}

func (f *fakeServer) pushedOps() []Op {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Op
	for _, p := range f.pushes {
		out = append(out, p.Ops...)
	}
	return out
}

// newBoundEngine 建一个真实集合目录并返回绑定好假服务端的 Engine。
func newBoundEngine(t *testing.T, cli *Client) (*collection.Collection, *Engine) {
	t.Helper()
	c, err := collection.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open collection: %v", err)
	}
	e := &Engine{
		Coll: c,
		Bind: BindInfo{Linked: true, ServerURL: cli.BaseURL, ProjectID: 1, Mode: ModeAuto},
		CLI:  cli,
	}
	return c, e
}

func payloadOf(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return b
}

// TestRunOneSnapshotPullThenPush 首次同步（cursor=0）：全量拉取 → 落盘 → 推送本地新建。
func TestRunOneSnapshotPullThenPush(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 7}
	f.snapshot = Snapshot{
		Cursor: 7,
		Items: []Item{
			{Type: "group", UID: "g1", Op: "upsert", ItemRev: 1,
				Payload: payloadOf(t, Payload{Title: "用户"})},
			{Type: "api", UID: "a1", Op: "upsert", ItemRev: 2,
				Payload: payloadOf(t, Payload{Name: "列表", Method: "GET", URL: "/v1/user", GroupUID: "g1"})},
		},
	}
	c, e := newBoundEngine(t, cli)

	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	if rep.Pulled != 2 {
		t.Fatalf("应拉取 2 条，got %+v", rep)
	}
	if e.Bind.Cursor != 7 {
		t.Fatalf("cursor 应推进到 7，got %d", e.Bind.Cursor)
	}

	// 落盘验证：分组成为目录，接口成为请求文件。
	reqs, err := c.ListAllRequests()
	if err != nil {
		t.Fatalf("ListAllRequests: %v", err)
	}
	if len(reqs) != 1 || reqs[0].Name != "列表" || reqs[0].URL != "/v1/user" {
		t.Fatalf("服务端接口未正确落盘: %+v", reqs)
	}

	// 本地新建一条请求后再次同步：应推送 1 条 api upsert。
	if _, err := c.CreateRequest("", "本地新建", "POST"); err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	f.state = State{Cursor: 12}
	rep, err = e.RunOne()
	if err != nil {
		t.Fatalf("RunOne(第二论): %v", err)
	}
	if rep.Pushed == 0 {
		t.Fatalf("本地新建应被推送，got %+v", rep)
	}
	ops := f.pushedOps()
	var found bool
	for _, op := range ops {
		if op.OpID == "" {
			t.Fatal("推送 op 必须带 op_id（幂等凭据）")
		}
		if op.Type != "api" || op.Op != "upsert" {
			continue
		}
		var p Payload
		if err := json.Unmarshal(op.Payload, &p); err != nil {
			t.Fatalf("解析推送载荷失败: %v", err)
		}
		if p.Name == "本地新建" {
			found = true
			if p.Method != "POST" {
				t.Fatalf("推送载荷方法应保留，got %q", p.Method)
			}
		}
	}
	if !found {
		t.Fatalf("本地新建应作为 api upsert 推送，got %+v", ops)
	}
}

// TestRunOneProtocolTooOld 协议过旧必须在拉取前就失败，不能带过期协议白跑。
func TestRunOneProtocolTooOld(t *testing.T) {
	f, cli := newFakeServer(t)
	f.meta = Meta{ServerVersion: "old", SyncProto: 0}
	_, e := newBoundEngine(t, cli)

	if _, err := e.RunOne(); err == nil {
		t.Fatal("sync_proto=0 应报错")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.paths {
		if strings.Contains(p, "/api/sync/") {
			t.Fatalf("协商失败后不应再请求同步接口，实际请求了 %s", p)
		}
	}
}

// TestRunOneMissingFeature 缺少关键能力同样要拒绝。
func TestRunOneMissingFeature(t *testing.T) {
	f, cli := newFakeServer(t)
	f.meta = Meta{SyncProto: 1, Features: []string{"sync"}} // 缺 change_log
	_, e := newBoundEngine(t, cli)

	_, err := e.RunOne()
	if err == nil || !strings.Contains(err.Error(), "change_log") {
		t.Fatalf("缺少 change_log 能力应报错并指出缺失项，got %v", err)
	}
}

// TestRunOneGapFallsBackToSnapshot 增量返回 gap 时降级全量，且记录在报告里。
func TestRunOneGapFallsBackToSnapshot(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 20, OldestAvailable: 15}
	f.changes = Changes{Gap: true, Cursor: 20}
	f.snapshot = Snapshot{Cursor: 20, Items: []Item{
		{Type: "group", UID: "g9", Op: "upsert", ItemRev: 9, Payload: payloadOf(t, Payload{Title: "补拉"})},
	}}
	c, e := newBoundEngine(t, cli)
	e.Bind.Cursor = 3 // 落后于 oldest_available，服务端会判 gap

	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	if !rep.Gap {
		t.Fatalf("应记录 gap，got %+v", rep)
	}
	if rep.Pulled != 1 {
		t.Fatalf("降级全量后应拉取 1 条，got %+v", rep)
	}
	info, err := c.Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	var found bool
	for _, n := range info.Tree {
		if n.Name == "补拉" {
			found = true
		}
	}
	if !found {
		t.Fatalf("gap 降级后应落盘服务端内容，tree=%+v", info.Tree)
	}
}

// TestRunOneConflictWritesCopy 冲突要留副本且条目仍为脏（下次重推）。
func TestRunOneConflictWritesCopy(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)
	req, err := c.CreateRequest("", "会冲突", "GET")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	f.pushFn = func(r *PushReq) *PushResp {
		res := make([]OpResult, 0, len(r.Ops))
		for _, op := range r.Ops {
			res = append(res, OpResult{OpID: op.OpID, Status: "conflict", Reason: "base_rev 不匹配"})
		}
		return &PushResp{Results: res}
	}

	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	// 新建集合自带默认目录/环境，所以推送的不止一条：冲突数应等于本次推送的 op 数。
	ops := f.pushedOps()
	if len(ops) == 0 {
		t.Fatal("应有推送")
	}
	if rep.Conflicts != len(ops) {
		t.Fatalf("冲突数应等于推送条数 %d，got %+v", len(ops), rep)
	}
	// 只有请求会落冲突副本（分组/环境不进冲突副本体系）。
	conflicts, err := c.ListConflicts()
	if err != nil {
		t.Fatalf("ListConflicts: %v", err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("请求应落 1 份冲突副本，got %d 条", len(conflicts))
	}
	// 冲突条目必须保持脏，否则本地改动就永远丢了
	if !dirtyContains(t, c, req.UID) {
		t.Fatalf("冲突后请求 %s 应仍为脏", req.UID)
	}
}

// TestRunOneRejectedReportsReason 服务端 rejected 要带回原因，且不标记已同步。
func TestRunOneRejectedReportsReason(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)
	req, err := c.CreateRequest("", "会被拒", "GET")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	f.pushFn = func(r *PushReq) *PushResp {
		res := make([]OpResult, 0, len(r.Ops))
		for _, op := range r.Ops {
			res = append(res, OpResult{OpID: op.OpID, Status: "rejected", Reason: "变更流水写入失败"})
		}
		return &PushResp{Results: res}
	}

	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	ops := f.pushedOps()
	if rep.Rejected != len(ops) || len(rep.Errors) == 0 {
		t.Fatalf("应逐条记录 rejected 与原因，got %+v (ops=%d)", rep, len(ops))
	}
	if !strings.Contains(strings.Join(rep.Errors, ";"), "变更流水") {
		t.Fatalf("错误原因应透传，got %v", rep.Errors)
	}
	if !dirtyContains(t, c, req.UID) {
		t.Fatalf("rejected 后请求 %s 应仍为脏，下次重试", req.UID)
	}
}

// dirtyContains 判断某个 uid 是否仍在脏队列里。
func dirtyContains(t *testing.T, c *collection.Collection, uid string) bool {
	t.Helper()
	dirty, err := c.DirtyNodes()
	if err != nil {
		t.Fatalf("DirtyNodes: %v", err)
	}
	for _, n := range dirty {
		if n.UID == uid {
			return true
		}
	}
	return false
}

// TestRunOneMirrorModeSkipsPush mirror 模式只拉不推。
func TestRunOneMirrorModeSkipsPush(t *testing.T) {
	f, cli := newFakeServer(t)
	c, e := newBoundEngine(t, cli)
	e.Bind.Mode = ModeMirror
	if _, err := c.CreateRequest("", "镜像不推", "GET"); err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}

	if _, err := e.RunOne(); err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	if ops := f.pushedOps(); len(ops) != 0 {
		t.Fatalf("mirror 模式不应推送，got %+v", ops)
	}
}

// TestRunOneServerUnreachable 服务端不可达要报错而不是静默成功。
func TestRunOneServerUnreachable(t *testing.T) {
	c, err := collection.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	cli := New("http://127.0.0.1:1", "t", "d")
	e := &Engine{Coll: c, Bind: BindInfo{Linked: true, ProjectID: 1, Mode: ModeAuto}, CLI: cli}
	if _, err := e.RunOne(); err == nil {
		t.Fatal("服务端不可达应报错")
	}
}

// TestSaveBindRoundTrip 绑定信息（含 cursor）要能持久化，否则每次同步都全量重拉。
func TestSaveBindRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := BindInfo{Linked: true, ServerURL: "http://x", ProjectID: 9, Mode: ModeAuto, Cursor: 42,
		DocHashes: map[string]string{"d1": "h"}}
	if err := SaveBind(dir, in); err != nil {
		t.Fatalf("SaveBind: %v", err)
	}
	// 权限必须是 0600：绑定里含 PAT
	fi, err := os.Stat(filepath.Join(dir, ".sync.json"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf(".sync.json 权限应为 0600，got %v", fi.Mode().Perm())
	}
	out, err := LoadBind(dir)
	if err != nil {
		t.Fatalf("LoadBind: %v", err)
	}
	if out.Cursor != 42 || out.ProjectID != 9 || out.DocHashes["d1"] != "h" {
		t.Fatalf("绑定信息未正确持久化: %+v", out)
	}
	if !IsLinked(dir) {
		t.Fatal("IsLinked 应为 true")
	}
}

// TestRunOneAppliesAllEntityTypes 快照里一次下发全部实体类型：
// group / api / doc / example / variable_env / variable，外加一条 delete。
// 覆盖 applyDoc / applyExample / applyVarEnv / applyVariable / applyDelete
// 各自的新建分支——这正是「协议跑通但实体没落盘」最容易漏掉的地方。
func TestRunOneAppliesAllEntityTypes(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 30}
	enabled := true
	f.snapshot = Snapshot{
		Cursor: 30,
		Items: []Item{
			{Type: "group", UID: "g1", Op: "upsert", ItemRev: 1, Payload: payloadOf(t, Payload{Title: "订单"})},
			{Type: "api", UID: "a1", Op: "upsert", ItemRev: 2,
				Payload: payloadOf(t, Payload{Name: "下单", Method: "POST", URL: "/v1/order", GroupUID: "g1"})},
			{Type: "example", UID: "ex1", Op: "upsert", ItemRev: 3,
				Payload: payloadOf(t, Payload{Name: "成功", APIUID: "a1", Status: 201, ContentType: "application/json", Sample: `{"ok":true}`})},
			{Type: "doc", UID: "d1", Op: "upsert", ItemRev: 4,
				Payload: payloadOf(t, Payload{Name: "接入说明", Content: "# 正文", GroupUID: "g1"})},
			{Type: "variable_env", UID: "env1", Op: "upsert", ItemRev: 5,
				Payload: payloadOf(t, Payload{Name: "联调"})},
			{Type: "variable", UID: "var1", Op: "upsert", ItemRev: 6,
				Payload: payloadOf(t, Payload{Name: "host", Value: "http://api.test", EnvUID: "env1", Enabled: &enabled})},
		},
	}
	c, e := newBoundEngine(t, cli)

	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	if rep.Pulled != 6 || len(rep.Errors) != 0 {
		t.Fatalf("6 条应全部应用成功，got %+v", rep)
	}

	reqs, err := c.ListAllRequests()
	if err != nil {
		t.Fatalf("ListAllRequests: %v", err)
	}
	if len(reqs) != 1 || reqs[0].URL != "/v1/order" {
		t.Fatalf("接口落盘异常: %+v", reqs)
	}
	docs, err := c.ListDocs()
	if err != nil {
		t.Fatalf("ListDocs: %v", err)
	}
	if len(docs) != 1 || docs[0].Content != "# 正文" {
		t.Fatalf("文档落盘异常: %+v", docs)
	}
	envs, err := c.ListEnvs()
	if err != nil {
		t.Fatalf("ListEnvs: %v", err)
	}
	var env *collection.Env
	for i := range envs {
		if envs[i].UID == "env1" {
			env = &envs[i]
		}
	}
	if env == nil || env.Name != "联调" {
		t.Fatalf("环境未落盘: %+v", envs)
	}
	if len(env.Vars) != 1 || env.Vars[0].Name != "host" || env.Vars[0].Value != "http://api.test" {
		t.Fatalf("变量未落盘: %+v", env.Vars)
	}
	// 摘要要记下来，否则下一轮会把刚拉下来的内容当成脏数据再推回去。
	if e.Bind.EnvHashes["env1"] == "" || e.Bind.VarHashes["var1"] == "" {
		t.Fatalf("环境/变量摘要未记录: env=%v var=%v", e.Bind.EnvHashes, e.Bind.VarHashes)
	}

	// 第二轮：服务端下发 delete（文档 + 变量 + 环境），本地应同步移除。
	f.state = State{Cursor: 31}
	e.Bind.Cursor = 30
	f.changes = Changes{Cursor: 31, Items: []Item{
		{Type: "doc", UID: "d1", Op: "delete", Deleted: true, ItemRev: 7},
		{Type: "variable", UID: "var1", Op: "delete", Deleted: true, ItemRev: 8},
	}}
	rep, err = e.RunOne()
	if err != nil {
		t.Fatalf("RunOne(delete): %v", err)
	}
	docs, err = c.ListDocs()
	if err != nil {
		t.Fatalf("ListDocs: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("文档应已被删除（进回收站），got %+v", docs)
	}
	envs, err = c.ListEnvs()
	if err != nil {
		t.Fatalf("ListEnvs: %v", err)
	}
	for _, ev := range envs {
		for _, v := range ev.Vars {
			if v.UID == "var1" {
				t.Fatalf("变量 var1 应已被删除，got %+v", v)
			}
		}
	}
	if rep.Pulled != 0 {
		t.Fatalf("delete 不计入 Pulled（只统计 upsert），got %+v", rep)
	}
}

// TestDeviceIDStable 设备标识必须落盘且稳定：它是幂等与审计的主体，
// 每次启动都变会让服务端把同一台机器当成无限多台设备。
func TestDeviceIDStable(t *testing.T) {
	id := DeviceID()
	if id == "" {
		t.Fatal("DeviceID 不应为空")
	}
	if again := DeviceID(); again != id {
		t.Fatalf("同一进程内 DeviceID 应稳定，got %q then %q", id, again)
	}
}
