package app

// QA 独立验证（P1-5）：同一集合的同步互斥。
//
// 与工程师 app_sync_mutex_test.go 的关键区别：这里**不靠"预先占锁"模拟并发**，
// 而是真的起两路 RunSync 打到一个会阻塞的 HTTP 服务端上——第一路卡在网络里持锁，
// 第二路必须当场 busy；同时用服务端请求计数证明"只有第一路真的跑了"。
// 其余覆盖：定时入口被占锁时零 HTTP、早退路径的锁释放（三种）、按根隔离、wait=true 排队不卡死。

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"api-doc-go-client/internal/syncengine"
)

// syncMuTestServer 可编排的假同步服务端：统计每个路径被访问次数，可阻塞第一次 /api/meta，或一律 500。
type syncMuTestServer struct {
	srv      *httptest.Server
	total    atomic.Int64
	mu       sync.Mutex
	perPath  map[string]int
	reached  chan struct{}
	release  chan struct{}
	blockOne sync.Once
	failAll  bool
	blocked  bool
}

// syncMuTestStart 起一个假服务端；blockFirstMeta=true 时第一次 /api/meta 会一直等到 Release。
func syncMuTestStart(t *testing.T, blockFirstMeta, failAll bool) *syncMuTestServer {
	t.Helper()
	s := &syncMuTestServer{
		perPath: map[string]int{},
		reached: make(chan struct{}),
		release: make(chan struct{}),
		failAll: failAll,
		blocked: blockFirstMeta,
	}
	write := func(w http.ResponseWriter, data any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "success", "data": data})
	}
	base := func(next func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			s.total.Add(1)
			s.mu.Lock()
			s.perPath[r.URL.Path]++
			s.mu.Unlock()
			if s.failAll {
				w.WriteHeader(http.StatusInternalServerError) // 真跑一轮必然失败
				return
			}
			next(w, r)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/meta", base(func(w http.ResponseWriter, r *http.Request) {
		if s.blocked {
			s.blockOne.Do(func() { close(s.reached) })
			<-s.release
		}
		write(w, syncengine.Meta{ServerVersion: "qa15", SyncProto: 1, Features: []string{"sync", "change_log"}})
	}))
	mux.HandleFunc("/api/sync/state", base(func(w http.ResponseWriter, r *http.Request) {
		write(w, syncengine.State{Cursor: 0})
	}))
	mux.HandleFunc("/api/sync/snapshot", base(func(w http.ResponseWriter, r *http.Request) {
		write(w, syncengine.Snapshot{Cursor: 0, Items: []syncengine.Item{}})
	}))
	mux.HandleFunc("/api/sync/changes", base(func(w http.ResponseWriter, r *http.Request) {
		write(w, syncengine.Changes{Cursor: 0, Items: []syncengine.Item{}})
	}))
	mux.HandleFunc("/api/sync/push", base(func(w http.ResponseWriter, r *http.Request) {
		write(w, syncengine.PushResp{Results: []syncengine.OpResult{}, Cursor: 0})
	}))
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

// Release 放行被阻塞的请求。
func (s *syncMuTestServer) Release() {
	if s.blocked {
		close(s.release)
	}
}

// Hits 某路径被访问的次数。
func (s *syncMuTestServer) Hits(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.perPath[path]
}

// Reset 清零计数（用于"对照组之后"的干净起点）。
func (s *syncMuTestServer) Reset() {
	s.total.Store(0)
	s.mu.Lock()
	s.perPath = map[string]int{}
	s.mu.Unlock()
}

// syncMuTestBind 把某个根绑定到一个必然失败的地址（回环允许明文 http），auto 模式 + PAT。
func syncMuTestBind(t *testing.T, core *App, root, url string) {
	t.Helper()
	if _, err := core.SetSyncBindFor(SyncBindReq{
		Root: root, ServerURL: url, ProjectID: 1, Mode: syncengine.ModeAuto, Token: "pat-qa15",
	}); err != nil {
		t.Fatalf("SetSyncBindFor: %v", err)
	}
}

// TestQA15ConcurrentRunSyncExactlyOneBusy 真并发：两路 RunSync 同时进来，
// 恰好一路被拒（errSyncBusy）、另一路完整跑完；且只有一路真的发出请求。
func TestQA15ConcurrentRunSyncExactlyOneBusy(t *testing.T) {
	isolateConfigDir(t)
	core := NewApp()
	root := openRoot(t, core, "conc")
	c, err := core.requireCollection()
	if err != nil {
		t.Fatalf("requireCollection: %v", err)
	}
	srv := syncMuTestStart(t, true, false) // 第一次 /api/meta 会阻塞
	syncMuTestBind(t, core, root, srv.srv.URL)

	type outcome struct {
		rep *syncengine.Report
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		rep, err := core.RunSync("")
		done <- outcome{rep, err}
	}()

	// 等第一路真的进到服务端（说明它已持有该集合的同步锁）。
	select {
	case <-srv.reached:
	case <-time.After(10 * time.Second):
		t.Fatal("第一路 RunSync 没有发出请求")
	}

	// 持锁期间状态栏语义：Running 必须为 true。
	if st := core.syncStatusOf(c); !st.Running {
		t.Fatalf("同步进行中 Running 应为 true，got %+v", st)
	}

	// 第二路（同一集合、同一活动根）：必须当场 busy。
	start := time.Now()
	_, err2 := core.RunSync("")
	busyCost := time.Since(start)
	if !errors.Is(err2, errSyncBusy) {
		t.Fatalf("并发第二路应返回 errSyncBusy，got %v", err2)
	}
	if err2 != nil && strings.Contains(err2.Error(), "未关联") {
		t.Fatalf("第二路不该继续往下跑（应卡在锁上），got %v", err2)
	}
	if busyCost > time.Second {
		t.Fatalf("busy 必须立即返回，实际耗时 %v", busyCost)
	}

	srv.Release()
	select {
	case out := <-done:
		if out.err != nil {
			t.Fatalf("第一路应完整跑完，got err=%v", out.err)
		}
		if out.rep == nil {
			t.Fatal("第一路应返回 Report")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("第一路没有结束（可能被第二路堵死）")
	}

	// 只有第一路真的跑了：/api/meta 恰好命中 1 次（第二路一条 HTTP 都不该发）。
	if n := srv.Hits("/api/meta"); n != 1 {
		t.Fatalf("只应有一路真正执行同步，/api/meta 命中 %d 次", n)
	}
	// 结束后锁必须已释放，Running 回 false。
	mu := core.syncMuFor(c)
	if !mu.TryLock() {
		t.Fatal("第一路返回后锁未释放")
	}
	mu.Unlock()
	if st := core.syncStatusOf(c); st.Running {
		t.Fatalf("同步结束后 Running 应回 false，got %+v", st)
	}
	if st := core.syncStatusOf(c); st.LastError != "" {
		t.Fatalf("成功的一轮不该留下 LastError，got %+v", st)
	}
}

// TestQA15AutoSyncSkipsSilentlyWithoutHTTP 定时入口被占锁时：零 HTTP、毫秒返回、不写 LastError；
// 反向（不占锁）确实跑一轮并记错误——证明断言有牙。
func TestQA15AutoSyncSkipsSilentlyWithoutHTTP(t *testing.T) {
	isolateConfigDir(t)
	core := NewApp()
	root := openRoot(t, core, "auto")
	c, err := core.requireCollection()
	if err != nil {
		t.Fatalf("requireCollection: %v", err)
	}
	srv := syncMuTestStart(t, false, true) // 一律 500：真跑一轮必然报错
	syncMuTestBind(t, core, root, srv.srv.URL)

	// 对照组：不占锁 → 真的发 HTTP 并记 LastError。
	core.autoSyncOnce(root)
	if n := srv.total.Load(); n == 0 {
		t.Fatal("对照失败：未占锁时定时同步应真的发出 HTTP")
	}
	if st := core.syncStatusOf(c); st.LastError == "" {
		t.Fatalf("对照失败：真跑一轮失败应写 LastError，got %+v", st)
	}

	// 复位后占锁：本轮必须什么都不做。
	srv.Reset()
	core.updateSyncStatus(c, func(st *SyncStatus) {
		st.LastError = ""
		st.Running = false
	})
	mu := core.syncMuFor(c)
	mu.Lock()
	start := time.Now()
	core.autoSyncOnce(root)
	elapsed := time.Since(start)
	mu.Unlock()

	if n := srv.total.Load(); n != 0 {
		t.Fatalf("占锁时定时同步不该发出任何 HTTP，实际 %d 次（path=%v）", n, srv.perPath)
	}
	if elapsed > 300*time.Millisecond {
		t.Fatalf("占锁时应毫秒级返回，实际 %v", elapsed)
	}
	st := core.syncStatusOf(c)
	if st.LastError != "" || st.Running {
		t.Fatalf("静默跳过不该改动状态（LastError/Running），got %+v", st)
	}
}

// TestQA15LockReleasedOnEveryEarlyReturn 三种早退路径（未绑定 / 地址非法 / 缺令牌）
// 返回后锁都必须已释放，且重复调用仍然是同一种早退错误、不能变成 busy。
func TestQA15LockReleasedOnEveryEarlyReturn(t *testing.T) {
	isolateConfigDir(t)
	core := NewApp()
	openRoot(t, core, "early")
	c, err := core.requireCollection()
	if err != nil {
		t.Fatalf("requireCollection: %v", err)
	}

	cases := []struct {
		name string
		prep func()
		want string
	}{
		{
			name: "未绑定",
			prep: func() { /* 新开的集合没有 .sync.json */ },
			want: "未关联服务端项目",
		},
		{
			name: "地址非法（明文 http 非回环）",
			prep: func() {
				if err := syncengine.SaveBind(c.Dir, syncengine.BindInfo{
					Linked: true, ServerURL: "http://192.0.2.10:8080",
					ProjectID: 1, Mode: syncengine.ModeAuto, Token: "pat-qa15",
				}); err != nil {
					t.Fatalf("SaveBind: %v", err)
				}
			},
			want: "明文 http",
		},
		{
			name: "缺少令牌",
			prep: func() {
				core.setSyncToken(c, "")
				if err := syncengine.SaveBind(c.Dir, syncengine.BindInfo{
					Linked: true, ServerURL: "http://127.0.0.1:1",
					ProjectID: 1, Mode: syncengine.ModeAuto,
				}); err != nil {
					t.Fatalf("SaveBind: %v", err)
				}
			},
			want: "缺少访问令牌",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.prep()
			_, err := core.RunSync("")
			if err == nil {
				t.Fatalf("%s：应返回错误", tc.name)
			}
			if errors.Is(err, errSyncBusy) {
				t.Fatalf("%s：不该报 busy，got %v", tc.name, err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%s：错误文案应含 %q，got %v", tc.name, tc.want, err)
			}
			// 锁必须已释放：否则一次早退就会把该集合永久卡死。
			mu := core.syncMuFor(c)
			if !mu.TryLock() {
				t.Fatalf("%s：早退后锁未释放", tc.name)
			}
			mu.Unlock()
			// 再调一次：仍是同一种早退错误（不是 busy），说明锁真的可用。
			if _, err := core.RunSync(""); err == nil || errors.Is(err, errSyncBusy) {
				t.Fatalf("%s：重复调用应复现同一种早退错误，got %v", tc.name, err)
			}
		})
	}
}

// TestQA15LockGranularityTwoRoots 锁按根隔离：占住 A 时 B 能完整跑一轮，
// 同一根每次拿到同一把锁，不同根拿到不同的锁；wait=true 排队不会卡死。
func TestQA15LockGranularityTwoRoots(t *testing.T) {
	isolateConfigDir(t)
	core := NewApp()
	rootA := openRoot(t, core, "granA")
	cA, err := core.requireCollection()
	if err != nil {
		t.Fatalf("requireCollection(A): %v", err)
	}
	rootB := openRoot(t, core, "granB")
	cB, err := core.collOf(rootB)
	if err != nil {
		t.Fatalf("collOf(B): %v", err)
	}

	if core.syncMuFor(cA) != core.syncMuFor(cA) {
		t.Fatal("同一集合每次都应拿到同一把锁")
	}
	if core.syncMuFor(cA) == core.syncMuFor(cB) {
		t.Fatal("两个根不该共用同一把锁（会互相堵死）")
	}

	srv := syncMuTestStart(t, false, false) // 全部成功
	syncMuTestBind(t, core, rootB, srv.srv.URL)

	muA := core.syncMuFor(cA)
	muA.Lock() // 刻意不用 defer：必须在 wait=true 那条 goroutine 起来之后显式放锁

	// B 不受 A 的锁影响：应能完整跑完。
	rep, err := core.RunSyncForRoot(rootB)
	if errors.Is(err, errSyncBusy) {
		t.Fatalf("不同根不该互相阻塞，got busy")
	}
	if err != nil {
		muA.Unlock()
		t.Fatalf("B 应能完整跑一轮，got %v", err)
	}
	if rep == nil {
		muA.Unlock()
		t.Fatal("B 应返回 Report")
	}
	// A 自己被占着：走 A 的入口必须 busy。
	if _, aerr := core.RunSyncForRoot(rootA); !errors.Is(aerr, errSyncBusy) {
		muA.Unlock()
		t.Fatalf("被占用的根应返回 busy，got %v", aerr)
	}

	// wait=true 的排队语义：占锁期间阻塞、放锁后继续（不能死等或 panic）。
	waited := make(chan error, 1)
	go func() {
		_, werr := core.runSyncFor(cA, "", true)
		waited <- werr
	}()
	select {
	case werr := <-waited:
		muA.Unlock()
		t.Fatalf("wait=true 在锁被占用时应排队等待，实际立刻返回 %v", werr)
	case <-time.After(200 * time.Millisecond):
	}
	muA.Unlock()
	select {
	case werr := <-waited:
		if werr == nil || !strings.Contains(werr.Error(), "未关联服务端项目") {
			t.Fatalf("放锁后排队的一轮应继续走到未绑定早退，got %v", werr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("放锁后排队的一轮仍未返回（死锁？）")
	}
}
