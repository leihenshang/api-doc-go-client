package app

// P1-5：同一集合同一时刻只允许一轮同步。
//
// 这里不建 HTTP 服务，"并发"用「预先占住该集合的同步锁」来模拟：占锁 = 已有一轮在跑。
// 这样验证的是入口语义（拒绝 / 静默跳过 / 正常放行），毫秒级、无网络、无 flake。

import (
	"errors"
	"strings"
	"testing"
	"time"

	"api-doc-go-client/internal/syncengine"
)

// TestRunSyncRejectsWhenBusy 手动入口：锁被占时立即返回 errSyncBusy，释放后能正常往下走。
func TestRunSyncRejectsWhenBusy(t *testing.T) {
	isolateConfigDir(t)
	core := NewApp()
	openRoot(t, core, "manual")
	c, err := core.requireCollection()
	if err != nil {
		t.Fatalf("requireCollection: %v", err)
	}

	mu := core.syncMuFor(c)
	mu.Lock()
	_, err = core.RunSync("")
	mu.Unlock()
	if !errors.Is(err, errSyncBusy) {
		t.Fatalf("已有一轮在跑时手动同步应立即返回 errSyncBusy，got %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "正在同步中") {
		t.Fatalf("busy 文案应可直接给前端 toast，got %v", err)
	}

	// 锁释放后同一条路径必须能往下走：证明上面的 busy 来自锁，而不是别的分支提前返回。
	_, err = core.RunSync("")
	if err == nil || errors.Is(err, errSyncBusy) {
		t.Fatalf("锁释放后不该再报 busy，got %v", err)
	}
	if !strings.Contains(err.Error(), "未关联服务端项目") {
		t.Fatalf("锁释放后应走到「未绑定」分支，got %v", err)
	}
}

// TestRunSyncForRootRejectsWhenBusy 按根入口（侧栏根行的「刷新」）同样受互斥保护。
func TestRunSyncForRootRejectsWhenBusy(t *testing.T) {
	isolateConfigDir(t)
	core := NewApp()
	root := openRoot(t, core, "rooted")
	c, err := core.requireCollection()
	if err != nil {
		t.Fatalf("requireCollection: %v", err)
	}

	mu := core.syncMuFor(c)
	mu.Lock()
	_, err = core.RunSyncForRoot(root)
	mu.Unlock()
	if !errors.Is(err, errSyncBusy) {
		t.Fatalf("已有一轮在跑时按根同步应立即返回 errSyncBusy，got %v", err)
	}

	_, err = core.RunSyncForRoot(root)
	if err == nil || errors.Is(err, errSyncBusy) {
		t.Fatalf("锁释放后不该再报 busy，got %v", err)
	}
	if !strings.Contains(err.Error(), "未关联服务端项目") {
		t.Fatalf("锁释放后应走到「未绑定」分支，got %v", err)
	}
}

// TestAutoSyncSkipsWhenBusy 定时入口：锁被占时静默跳过——不阻塞、不真跑一轮、不记错误。
//
// "不真跑一轮"的判据是 SyncStatus.LastError：这里把集合绑定到一个没有监听的本机端口，
// 真跑一轮必然连接失败并把错误写进 LastError（见本用例前半段的对照）。
func TestAutoSyncSkipsWhenBusy(t *testing.T) {
	isolateConfigDir(t)
	core := NewApp()
	root := openRoot(t, core, "auto")
	c, err := core.requireCollection()
	if err != nil {
		t.Fatalf("requireCollection: %v", err)
	}
	// auto 模式 + 本机空端口：真跑一轮会立刻失败（回环地址允许明文 http）。
	if _, err := core.SetSyncBindFor(SyncBindReq{
		Root: root, ServerURL: "http://127.0.0.1:1", ProjectID: 1,
		Mode: syncengine.ModeAuto, Token: "pat-test",
	}); err != nil {
		t.Fatalf("SetSyncBindFor: %v", err)
	}

	// 对照（先证明这个断言口径有牙）：不占锁时 autoSyncOnce 真的跑了一轮并记下错误。
	core.autoSyncOnce(root)
	if st := core.syncStatusOf(c); st.LastError == "" {
		t.Fatalf("对照失败：未占锁时 autoSyncOnce 应真的跑一轮并记 LastError，got %+v", st)
	}
	// 清干净，准备验证"占锁 → 什么都不发生"。
	core.updateSyncStatus(c, func(st *SyncStatus) {
		st.LastError = ""
		st.Running = false
	})

	mu := core.syncMuFor(c)
	mu.Lock()
	defer mu.Unlock()

	start := time.Now()
	core.autoSyncOnce(root) // 抢不到锁：必须立刻返回，不能排队
	elapsed := time.Since(start)
	if elapsed > time.Second {
		t.Fatalf("占锁时定时同步应立刻跳过，实际耗时 %v", elapsed)
	}
	st := core.syncStatusOf(c)
	if st.LastError != "" {
		t.Fatalf("占锁时不该真的跑一轮（LastError 应为空），got %+v", st)
	}
	if st.Running {
		t.Fatalf("占锁时不该改动状态栏的同步中标志，got %+v", st)
	}
}

// TestRunSyncPassesWhenLockFree 不并发时能过锁（锁不是永远 busy），且返回后必定释放。
func TestRunSyncPassesWhenLockFree(t *testing.T) {
	isolateConfigDir(t)
	core := NewApp()
	freeRoot := openRoot(t, core, "free")
	// 先取到第一个根的集合（openRoot 会把新开的根置为活动根，所以必须在开第二个之前拿）
	c, err := core.requireCollection()
	if err != nil {
		t.Fatalf("requireCollection: %v", err)
	}
	otherRoot := openRoot(t, core, "other") // 第二个根：顺带验证锁按根隔离

	// 活动根是 other，这里按根点名第一个根：顺带验证锁确实是按集合取的。
	_, err = core.RunSyncForRoot(freeRoot)
	if err == nil || errors.Is(err, errSyncBusy) {
		t.Fatalf("未占锁时不该报 busy，got %v", err)
	}
	if !strings.Contains(err.Error(), "未关联服务端项目") {
		t.Fatalf("未绑定集合应返回「未关联服务端项目」，got %v", err)
	}
	// 返回后锁必须已释放（defer mu.Unlock 真的执行了）。
	mu := core.syncMuFor(c)
	if !mu.TryLock() {
		t.Fatal("RunSync 返回后锁未释放")
	}
	mu.Unlock()

	// 锁按根隔离：不同集合拿到的不是同一把锁，否则多根并存时会互相堵。
	mu2 := core.syncMuFor(c)
	if mu2 != mu {
		t.Fatal("同一集合每次都应拿到同一把锁（懒创建的稳定性）")
	}
	otherC, err := core.collOf(otherRoot)
	if err != nil {
		t.Fatalf("collOf(other): %v", err)
	}
	if otherMu := core.syncMuFor(otherC); otherMu == mu {
		t.Fatal("两个根不该共用同一把锁")
	}
	core.mu.Lock()
	n := len(core.syncMus)
	core.mu.Unlock()
	if n != 2 {
		t.Fatalf("两个根应各有一把锁，实际 %d", n)
	}
}

// TestSetSyncBindRejectsWhenBusy 保存绑定与同步共用同一把锁：同步进行中写绑定会被
// runSyncFor 结尾的整份回写盖掉（令牌可能被静默回滚），所以这里也是「占锁即拒」。
func TestSetSyncBindRejectsWhenBusy(t *testing.T) {
	isolateConfigDir(t)
	core := NewApp()
	root := openRoot(t, core, "bind")
	c, err := core.requireCollection()
	if err != nil {
		t.Fatalf("requireCollection: %v", err)
	}
	req := SyncBindReq{
		Root: root, ServerURL: "http://127.0.0.1:1", ProjectID: 1,
		Mode: syncengine.ModeAuto, Token: "pat-test",
	}

	mu := core.syncMuFor(c)
	mu.Lock()
	_, busyErr := core.SetSyncBindFor(req)
	mu.Unlock()
	if !errors.Is(busyErr, errSyncBusy) {
		t.Fatalf("同步进行中保存绑定应返回 errSyncBusy，got %v", busyErr)
	}
	// 被拒时不能留下半截写入：集合仍是未绑定状态。
	if prev, perr := syncengine.LoadBind(c.Dir); perr == nil && prev.Linked {
		t.Fatalf("busy 被拒时不该写入绑定，got %+v", prev)
	}

	// 解锁后同一请求必须成功，且锁已释放。
	b, err := core.SetSyncBindFor(req)
	if err != nil {
		t.Fatalf("解锁后保存绑定应成功，got %v", err)
	}
	if !b.Linked || b.Token != "pat-test" || b.ProjectID != 1 {
		t.Fatalf("绑定内容不符，got %+v", b)
	}
	if !core.syncMuFor(c).TryLock() {
		t.Fatal("SetSyncBindFor 返回后锁未释放")
	} else {
		core.syncMuFor(c).Unlock()
	}
}

// TestUnbindSyncRejectsWhenBusy 解绑同样写整份 .sync.json：同步进行中解绑会被回写「复活」，
// 所以一并纳入同一把锁。
func TestUnbindSyncRejectsWhenBusy(t *testing.T) {
	isolateConfigDir(t)
	core := NewApp()
	root := openRoot(t, core, "unbind")
	c, err := core.requireCollection()
	if err != nil {
		t.Fatalf("requireCollection: %v", err)
	}
	// 先正常绑定（未占锁，应当成功）。
	if _, err := core.SetSyncBindFor(SyncBindReq{
		Root: root, ServerURL: "http://127.0.0.1:1", ProjectID: 1,
		Mode: syncengine.ModeAuto, Token: "pat-test",
	}); err != nil {
		t.Fatalf("前置绑定: %v", err)
	}

	mu := core.syncMuFor(c)
	mu.Lock()
	busyErr := core.UnbindSyncFor(root)
	mu.Unlock()
	if !errors.Is(busyErr, errSyncBusy) {
		t.Fatalf("同步进行中解绑应返回 errSyncBusy，got %v", busyErr)
	}
	// 被拒时绑定必须原样还在（不能被半解绑）。
	if prev, perr := syncengine.LoadBind(c.Dir); perr != nil || !prev.Linked {
		t.Fatalf("busy 被拒时绑定应保持原样，got %+v err=%v", prev, perr)
	}

	// 解锁后解绑成功。
	if err := core.UnbindSyncFor(root); err != nil {
		t.Fatalf("解锁后解绑应成功，got %v", err)
	}
	if prev, perr := syncengine.LoadBind(c.Dir); perr != nil || prev.Linked {
		t.Fatalf("解绑后应为未绑定，got %+v err=%v", prev, perr)
	}
	if !core.syncMuFor(c).TryLock() {
		t.Fatal("UnbindSyncFor 返回后锁未释放")
	} else {
		core.syncMuFor(c).Unlock()
	}
}
