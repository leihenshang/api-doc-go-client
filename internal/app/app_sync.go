package app

// 同步入口与状态（P2-8：从 app.go 按子域拆出）。
// 具体协议与引擎在 internal/syncengine；这里只做门面转发与状态聚合。

import (
	"errors"
	"strings"
	"sync"
	"time"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/syncengine"
)

// errSyncBusy 该集合已有一轮同步在跑（手动入口直接拒绝，不排队）。
//
// 为什么手动拒绝而不是排队：连点两次「立即同步」的用户，排队会让他等满两整轮网络往返，
// 而第二轮基本是把第一轮刚拉下来的东西再走一遍（多余），且按钮的「同步中」状态与实际
// 执行轮次对不上。拒绝 + 明确提示更可控——想再跑一次，再点一次就是了。
// 定时同步遇到它必须**静默跳过**（定时器弹错是噪声）。
var errSyncBusy = errors.New("该集合正在同步中，请稍后再试")

// ---- 同步（C7 / I1–I6）----

// SyncBindInfo 绑定信息（不含 PAT）。
type SyncBindInfo = syncengine.BindInfo

// SyncReport 一轮同步摘要。
type SyncReport = syncengine.Report

// GetSyncBind 读取活动集合的同步绑定。
func (a *App) GetSyncBind() (*syncengine.BindInfo, error) {
	return a.GetSyncBindFor("")
}

// GetSyncBindFor 读取指定工作目录的同步绑定（root 为空 = 活动根）。
func (a *App) GetSyncBindFor(root string) (*syncengine.BindInfo, error) {
	c, err := a.collOf(root)
	if err != nil {
		return nil, err
	}
	b, err := syncengine.LoadBind(c.Dir)
	if err != nil {
		// 未绑定不是错误
		return &syncengine.BindInfo{}, nil
	}
	return &b, nil
}

// SyncBindReq 绑定/更新同步设置的入参（Root 为空 = 活动根）。
type SyncBindReq struct {
	Root      string `json:"root"`
	ServerURL string `json:"serverUrl"`
	ProjectID uint64 `json:"projectId"`
	Mode      string `json:"mode"`
	Token     string `json:"token"`
}

// SetSyncBind 绑定/更新活动集合的服务端项目。
func (a *App) SetSyncBind(serverURL string, projectID uint64, mode, token string) (*syncengine.BindInfo, error) {
	return a.SetSyncBindFor(SyncBindReq{ServerURL: serverURL, ProjectID: projectID, Mode: mode, Token: token})
}

// SetSyncBindFor 绑定/更新指定根的服务端项目。
//
// PAT 随 .sync.json 持久化（0600）：重启客户端后免重填；本次未填令牌时沿用同一服务端
// 上已保存的令牌（改模式不必重输）。地址必须通过校验：非回环地址禁止明文 http。
//
// 与同步互斥同一把锁：runSyncFor 结尾会把**整份 Bind** 回写（cursor + 引擎侧状态），
// 若同步进行中改了绑定（地址/项目/令牌），这轮回写会把新设置盖回旧值——令牌可能被静默回滚。
// 这里「读旧绑定 → 合并 → 写新绑定」本身也是读-改-写，同样需要临界区。
func (a *App) SetSyncBindFor(req SyncBindReq) (*syncengine.BindInfo, error) {
	c, err := a.collOf(req.Root)
	if err != nil {
		return nil, err
	}
	mu := a.syncMuFor(c)
	if !mu.TryLock() {
		return nil, errSyncBusy // 正在同步：让用户稍后重试，别把新设置写进一轮进行中的回写里
	}
	defer mu.Unlock()

	if strings.TrimSpace(req.ServerURL) == "" || req.ProjectID == 0 {
		return nil, errors.New("服务端地址与项目 ID 必填")
	}
	base, err := syncengine.ValidateServerURL(req.ServerURL)
	if err != nil {
		return nil, err
	}
	mode := req.Mode
	if mode == "" {
		mode = syncengine.ModeAuto
	}
	tok := strings.TrimSpace(req.Token)
	b := syncengine.BindInfo{
		Linked: true, ServerURL: base,
		ProjectID: req.ProjectID, Mode: mode, Token: tok,
	}
	// 继承已有 cursor；未填 PAT 且服务端未变则沿用已保存令牌（只改模式/项目时不用重输）
	if prev, perr := syncengine.LoadBind(c.Dir); perr == nil {
		if prev.ProjectID == req.ProjectID {
			b.Cursor = prev.Cursor
		}
		if tok == "" && prev.ServerURL == base {
			b.Token = prev.Token
		}
	}
	if err := syncengine.SaveBind(c.Dir, b); err != nil {
		return nil, err
	}
	a.setSyncToken(c, b.Token)
	a.invalidateSyncDiskCache(c) // linked/mode/cursor 变了：状态栏下一次轮询立即反映
	return &b, nil
}

// ServerProject 服务端项目（同步设置的项目选择器用）。
type ServerProject struct {
	ID    uint64 `json:"id"`
	Title string `json:"title"`
}

// ListServerProjects 用当前填写的服务端地址与令牌拉取可见项目列表。
//
// 为什么要这个入口：项目 ID 是纯数字，用户在服务端 Web 与客户端之间来回找很容易填错，
// 而服务端 GET /api/project 已挂在 AuthOrPAT 下（PAT 可直接访问），一次请求就能列出来。
// 令牌留空时沿用当前集合已保存的 PAT，避免「先获取项目还得先保存一次」。
func (a *App) ListServerProjects(serverURL, token string) ([]ServerProject, error) {
	if strings.TrimSpace(serverURL) == "" {
		return nil, errors.New("请先填写服务端地址")
	}
	base, err := syncengine.ValidateServerURL(serverURL)
	if err != nil {
		return nil, err
	}
	tok := strings.TrimSpace(token)
	if tok == "" {
		if c, cerr := a.requireCollection(); cerr == nil {
			tok = a.syncTokenOf(c)
			if tok == "" {
				if b, berr := syncengine.LoadBind(c.Dir); berr == nil {
					tok = b.Token
				}
			}
		}
	}
	if tok == "" {
		return nil, errors.New("缺少访问令牌（PAT），请先填写")
	}
	projects, err := syncengine.New(base, tok, syncengine.DeviceID()).Projects()
	if err != nil {
		return nil, err
	}
	out := make([]ServerProject, 0, len(projects))
	for _, p := range projects {
		out = append(out, ServerProject{ID: p.ID, Title: p.Title})
	}
	return out, nil
}

// UnbindSync 解绑活动集合（清空 .sync.json，含 PAT）。
func (a *App) UnbindSync() error {
	return a.UnbindSyncFor("")
}

// UnbindSyncFor 解绑指定根。
//
// 与同步互斥同一把锁：解绑写的是整份 .sync.json，同步进行中写会被 runSyncFor 结尾的
// 整份回写盖回来（刚解绑完又被「复活」），所以这里也拒绝而不是排队。
func (a *App) UnbindSyncFor(root string) error {
	c, err := a.collOf(root)
	if err != nil {
		return err
	}
	mu := a.syncMuFor(c)
	if !mu.TryLock() {
		return errSyncBusy
	}
	defer mu.Unlock()

	b := syncengine.BindInfo{}
	if err := syncengine.SaveBind(c.Dir, b); err != nil {
		return err
	}
	a.setSyncToken(c, "")
	a.invalidateSyncDiskCache(c) // 解绑后 linked/mode/cursor 归零：立即反映
	return nil
}

// GetSyncPassphraseSet 返回本机是否已设置同步口令（secret 变量加解密用）。
// 刻意不回传口令本身——UI 只需要知道"是否已设置"。
func (a *App) GetSyncPassphraseSet() bool {
	return syncengine.SyncPassphrase() != ""
}

// SetSyncPassphrase 设置/清除同步口令（空串=清除）。口令存配置目录 0600，不进集合文件。
// 未设置口令时 secret 变量不会出本机（推送跳过该条，明文不外泄）。
func (a *App) SetSyncPassphrase(pass string) error {
	return syncengine.SetSyncPassphrase(pass)
}

// RunSync 在**活动根**上执行一轮同步（先 pull 后 push）。token 为空时用已保存的 PAT。
// 该集合已有一轮在跑时立即返回 errSyncBusy（不排队，见 errSyncBusy 的说明）。
func (a *App) RunSync(token string) (*syncengine.Report, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	return a.runSyncFor(c, token, false)
}

// RunSyncForRoot 在**指定工作目录**上执行一轮同步，不切换活动根/页签组。
// 供侧栏根行的「刷新」按钮调用：点哪个根就同步哪个根。
func (a *App) RunSyncForRoot(root string) (*syncengine.Report, error) {
	c, err := a.collOf(root)
	if err != nil {
		return nil, err
	}
	return a.runSyncFor(c, "", false)
}

// syncMuFor 取某集合的同步互斥量（按 rootKey(c.Dir) 归属：不同根并行、同一根串行）。
// 懒创建在 a.mu 下进行，表本身不会被并发写。
func (a *App) syncMuFor(c *collection.Collection) *sync.Mutex {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := rootKey(c.Dir)
	if key == "" {
		key = c.Dir // 目录名为空是异常态，退化成一个进程级共享锁也比不加锁强
	}
	if a.syncMus == nil {
		a.syncMus = map[string]*sync.Mutex{}
	}
	mu, ok := a.syncMus[key]
	if !ok {
		mu = &sync.Mutex{}
		a.syncMus[key] = mu
	}
	return mu
}

// runSyncFor 对指定集合跑一轮同步。
//
// 为什么把集合当参数：定时同步的 goroutine 属于某个根，而活动根可能在它跑之前就切换了；
// 若这里仍取「活动集合」，自动同步会打到用户刚切过去的另一个目录上。
//
// P1-5：整轮互斥。锁覆盖**整个同步体**（读绑定 → 令牌 → 引擎 → 回写 cursor → 重建索引），
// 而不只是 Engine.RunOne：两轮并发会把同一批 dirty 重复推上去、互相覆盖 .sync.json 的 cursor，
// 将来接 Git 还会直接撞 index.lock。此前只有 setSyncRunning 这个「给状态栏看的标志」，
// 读它与设置它不在同一临界区，手动点击与定时器完全可能同时进来。
//
// wait 语义：
//   - false：TryLock，抢不到立即返回 errSyncBusy（手动点击 / 定时器都用这条——定时器不该排队，
//     下一拍自然会再来）；
//   - true：Lock 排队（留给「必须跑完一轮」的调用方，例如将来后台补跑一轮 Git 同步）。
func (a *App) runSyncFor(c *collection.Collection, token string, wait bool) (*syncengine.Report, error) {
	mu := a.syncMuFor(c)
	if wait {
		mu.Lock()
	} else if !mu.TryLock() {
		return nil, errSyncBusy
	}
	defer mu.Unlock()

	b, err := syncengine.LoadBind(c.Dir)
	if err != nil || !b.Linked {
		return nil, errors.New("当前集合未关联服务端项目")
	}
	// 绑定信息来自集合内的 .sync.json（可被手工/外部改动），用前再校验一次地址
	if base, verr := syncengine.ValidateServerURL(b.ServerURL); verr != nil {
		return nil, verr
	} else {
		b.ServerURL = base
	}
	// 令牌解析顺序：显式参数 → 会话内存 → .sync.json 持久化的 PAT（重启后免重填）
	tok := token
	if tok == "" {
		tok = a.syncTokenOf(c)
	}
	if tok == "" {
		tok = b.Token
	}
	if tok == "" {
		return nil, errors.New("缺少访问令牌（PAT），请在同步设置中填写")
	}
	a.setSyncToken(c, tok)
	cli := syncengine.New(b.ServerURL, tok, syncengine.DeviceID())
	eng := &syncengine.Engine{Coll: c, Bind: b, CLI: cli}

	// 「同步中」用 defer 复位：RunOne panic 时锁会由上面的 defer 放掉，但状态栏的标志
	// 若是手动复位的就永远停在「同步中」。defer 也让它覆盖到回写/重建索引结束，比原来更准确。
	a.setSyncRunning(c, true)
	defer a.setSyncRunning(c, false)

	rep, err := eng.RunOne()

	// 回写引擎侧状态（cursor + 响应示例推送状态）
	b = eng.Bind
	// 绑定落盘失败只影响「下次打开自动恢复」，本轮同步已生效；不回滚以免把用户刚同步的内容当成失败。
	_ = syncengine.SaveBind(c.Dir, b)
	// 同步后重建索引
	_ = c.RebuildIndex()

	// E19 文档图片离线映射：成功轮后 best-effort 镜像绑定服务端的同源图片
	// （内容寻址已存在即跳过；失败不影响本轮结果，也不报错刷屏）
	if err == nil && rep != nil {
		if n, aerr := syncDocAssets(c, b.ServerURL, tok); aerr == nil && n > 0 {
			rep.Assets = n
		}
	}

	// 记状态（按集合各自记录，状态栏切到哪个根就看哪个根）
	a.updateSyncStatus(c, func(st *SyncStatus) {
		if err != nil {
			st.LastError = err.Error()
			return
		}
		st.LastError = ""
		st.LastSyncAt = time.Now().UnixMilli()
		if rep != nil {
			st.LastReport = rep
		}
	})
	a.invalidateSyncDiskCache(c) // 同步改动了 cursor/dirty/冲突：让下一次状态栏轮询立即重读
	if err == nil {
		a.emitAppEvent("collection:changed", map[string]any{"root": rootKey(c.Dir), "paths": []string{"sync"}})
	}
	return rep, err
}

// ---- 同步状态（T31：状态栏展示）----

// SyncStatus 状态栏用的同步状态。
type SyncStatus struct {
	Linked     bool               `json:"linked"`
	Mode       string             `json:"mode"`
	Running    bool               `json:"running"`
	LastSyncAt int64              `json:"lastSyncAt"` // Unix 毫秒
	LastError  string             `json:"lastError"`
	DirtyCount int                `json:"dirtyCount"`
	Conflicts  int                `json:"conflicts"`
	Cursor     int64              `json:"cursor"`
	LastReport *syncengine.Report `json:"lastReport,omitempty"`
}

// GetSyncStatus 活动根的同步状态（供状态栏轮询）。
//
// 内存里即时更新的字段（Running/LastSyncAt/LastError/LastReport）直接取；
// 需要读盘的字段（.sync.json / 索引 dirty / .conflicts 目录）走短 TTL 缓存，
// 避免状态栏每 5s 轮询都做三次磁盘访问（大集合下 dirty 查询 + 冲突目录列举会拖慢）。
func (a *App) GetSyncStatus() *SyncStatus {
	c, err := a.requireCollection()
	if err != nil {
		return &SyncStatus{}
	}
	st := a.syncStatusOf(c)
	disk := a.syncDiskSnapshot(c)
	st.Linked = disk.Linked
	st.Mode = disk.Mode
	st.Cursor = disk.Cursor
	st.DirtyCount = disk.DirtyCount
	st.Conflicts = disk.Conflicts
	return &st
}

// syncDiskTTL GetSyncStatus 磁盘派生字段的缓存有效期。
// 同步 / binding / 冲突变更后主动失效（见 invalidateSyncDiskCache），保证「立即同步」后状态即时刷新。
const syncDiskTTL = 2 * time.Second

// syncDiskSnapshot 取磁盘派生状态快照：TTL 内复用缓存，过期则重读并回存（按根各自缓存）。
func (a *App) syncDiskSnapshot(c *collection.Collection) syncDiskSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	e := a.entryOfLocked(c)
	if e == nil {
		return syncDiskSnapshot{}
	}
	if e.syncDisk != nil && time.Since(e.syncDiskAt) < syncDiskTTL {
		return *e.syncDisk
	}
	b, _ := syncengine.LoadBind(c.Dir)
	dirty, _ := c.DirtyNodesFast()
	confs, _ := c.ListConflicts()
	snap := syncDiskSnapshot{
		Linked:     b.Linked,
		Mode:       b.Mode,
		Cursor:     b.Cursor,
		DirtyCount: len(dirty),
		Conflicts:  len(confs),
	}
	e.syncDisk = &snap
	e.syncDiskAt = time.Now()
	return snap
}

// invalidateSyncDiskCache 丢弃某根的磁盘状态缓存，令下一次 GetSyncStatus 立即重读。
func (a *App) invalidateSyncDiskCache(c *collection.Collection) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e := a.entryOfLocked(c); e != nil {
		e.syncDisk = nil
		e.syncDiskAt = time.Time{}
	}
}

// setSyncRunning 标记该集合是否正在同步（状态栏「同步中」）。
func (a *App) setSyncRunning(c *collection.Collection, v bool) {
	a.updateSyncStatus(c, func(st *SyncStatus) { st.Running = v })
}

// StartAutoSync 为**活动根**启动定时自动同步（mode=auto 时每 intervalSec 一轮）。
// token 可为空（用会话里已存的）。
//
// 同一时刻只让活动根自动同步：启动时先把所有根的定时器停掉（含本根上一次的）。
// 否则多根并存会变成 N 个定时器一起打服务端，切换根之后旧根还会在后台继续同步。
func (a *App) StartAutoSync(intervalSec int) {
	if intervalSec < 30 {
		intervalSec = 120 // 默认 2 分钟
	}
	a.mu.Lock()
	for _, e := range a.colls {
		if e.syncStop != nil {
			close(e.syncStop)
			e.syncStop = nil
		}
	}
	e := a.activeEntryLocked()
	if e == nil {
		a.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	e.syncStop = stop
	root := a.activeRoot
	a.mu.Unlock()

	go func() {
		t := time.NewTicker(time.Duration(intervalSec) * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				a.autoSyncOnce(root)
			}
		}
	}()
}

// StopAutoSync 停止所有根的定时同步。
func (a *App) StopAutoSync() {
	a.mu.Lock()
	for _, e := range a.colls {
		if e.syncStop != nil {
			close(e.syncStop)
			e.syncStop = nil
		}
	}
	a.mu.Unlock()
}

// autoSyncOnce 某个根的一轮自动同步：仅 auto 模式且已绑定、未在跑时执行。
//
// st.Running 只是先做一次廉价的跳过（省掉读 .sync.json），真正的互斥在 runSyncFor 的锁里：
// 读 Running 与设置它不在同一临界区，仅凭它无法阻止「手动同步刚好在这一瞬开始」。
// 抢不到锁说明手动同步正在跑，本轮静默跳过——定时器不弹错、不排队。
func (a *App) autoSyncOnce(root string) {
	c, err := a.collOf(root)
	if err != nil {
		return // 该根已被关闭
	}
	st := a.syncStatusOf(c)
	if st.Running {
		return
	}
	b, err := syncengine.LoadBind(c.Dir)
	if err != nil || !b.Linked || b.Mode != syncengine.ModeAuto {
		return
	}
	// 内存没有时用 .sync.json 里持久化的 PAT：重启后定时同步仍能自动跑
	token := a.syncTokenOf(c)
	if token == "" {
		token = b.Token
	}
	if token == "" {
		return
	}
	if _, err := a.runSyncFor(c, token, false); errors.Is(err, errSyncBusy) {
		return // 手动同步正在跑：静默跳过（失败已由 runSyncFor 记进 SyncStatus.LastError）
	}
}

// ListConflicts 列出活动集合 .conflicts/ 冲突副本（I4）。
func (a *App) ListConflicts() ([]collection.ConflictItem, error) {
	return a.ListConflictsFor("")
}

// ListConflictsFor 列出指定根的冲突副本（root 为空 = 活动根）。
func (a *App) ListConflictsFor(root string) ([]collection.ConflictItem, error) {
	c, err := a.collOf(root)
	if err != nil {
		return nil, err
	}
	return c.ListConflicts()
}

// ConflictDetail 查看活动集合单个冲突副本的差异字段（本地 → 服务器）。
func (a *App) ConflictDetail(file string) (*collection.ConflictDetail, error) {
	return a.ConflictDetailFor("", file)
}

// ConflictDetailFor 查看指定根的冲突副本详情。
func (a *App) ConflictDetailFor(root, file string) (*collection.ConflictDetail, error) {
	c, err := a.collOf(root)
	if err != nil {
		return nil, err
	}
	return c.ConflictDetail(file)
}

// ResolveConflict 对活动集合应用冲突取舍：local | remote | copy。
func (a *App) ResolveConflict(file, choice string) error {
	return a.ResolveConflictFor("", file, choice)
}

// ResolveConflictFor 对指定根应用冲突取舍。
func (a *App) ResolveConflictFor(root, file, choice string) error {
	c, err := a.collOf(root)
	if err != nil {
		return err
	}
	if err := c.ResolveConflict(file, choice); err != nil {
		return err
	}
	a.invalidateSyncDiskCache(c) // 冲突数与 dirty 可能变化：状态栏下一次轮询立即反映
	return nil
}
