package app

import (
	"context"
	"errors"

	// 标准库的 runtime 与下面的 wails runtime 同名，这里起别名避免撞车。
	goruntime "runtime"
	"strings"
	"sync"
	"sync/atomic"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/config"
	"api-doc-go-client/internal/cookiejar"
	"api-doc-go-client/internal/index"
	"api-doc-go-client/internal/mcpaudit"
	"api-doc-go-client/internal/mcpauth"
	"api-doc-go-client/internal/mocksrv"
	"api-doc-go-client/internal/tray"
	"api-doc-go-client/internal/watch"

	runtime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App 暴露给前端的全部方法（Wails Bind；devserver 亦通过反射调用同一实现）。
// 约定：集合目录（文件）是真相源；前端只通过这些方法读写。
type App struct {
	ctx         context.Context
	mu          sync.Mutex
	headlessDir string // devserver 模式：PickDirectory 直接返回该目录
	settings    config.Settings
	jar         *cookiejar.Jar // 惰性创建的 Cookie 罐（按域名存取，与集合无关）
	// colls 已打开的工作目录（集合），键为 rootKey（规范化绝对路径）—— 多根并存的核心（见 multicoll.go）
	colls map[string]*collEntry
	// collOrder 打开顺序：侧栏根行的展示顺序（关闭某个根时保持其余顺序）
	collOrder []string
	// activeRoot 活动根的标识：所有集合级方法（不带 root 参数的那些）都作用于它
	activeRoot string
	// syncMus 按集合的同步互斥量（key = rootKey(c.Dir)）：同一集合同一时刻只允许一轮同步，
	// 不同根之间互不影响。用 a.mu 保护，懒创建（见 syncMuFor）。
	syncMus map[string]*sync.Mutex
	// sendCancels 按「工作目录:请求 uid」记录在途发送（指针身份用于避免旧请求结束时误删新请求）；
	// 带目录前缀是为了多根并存时不同目录里的同名 uid 不会互相取消。
	sendCancels map[string]*inflightSend
	// draftSends 未落盘草稿（uid 为空）的在途发送：它们共用不了 uid 键，
	// 单列一份避免「发送草稿 A 把草稿 B 取消掉」；每条记着自己的目录，取消时只动活动根的草稿。
	draftSends []*inflightSend
	// mock 本地 Mock 服务（H7）：同一时刻只服务一个根（mockRoot 记着是谁），换根启动会先停旧的
	mock     *mocksrv.Server
	mockRoot string
	// eventSubs 浏览器态事件订阅（devserver 经 SSE 转发给前端；id 用于退订）
	eventSubs   map[int]*eventSub
	eventSubSeq int
	// mcpBackend 内嵌 MCP HTTP 服务实现（由 main/devserver 注入，见 internal/mcp/embed.go）
	mcpBackend MCPBackend
	// mcpStatus 内嵌 MCP 服务最近一次状态（设置页展示；避免前端自己拼）
	mcpStatus *MCPStatus
	// MCP 交互授权（T2）：authzMu 守护 pending/inflight；mcpGrants 用 a.mu（与 settings 同锁）。
	authzMu     sync.Mutex
	mcpPending  map[string]*pendingAuthz // requestId → 进行中的弹窗
	mcpInflight map[string]*pendingAuthz // session|tool|project → 进行中的弹窗（并发去重）
	mcpGrants   *mcpauth.GrantStore      // 「始终允许」内存表（跨 MCP 重启存活）
	auditor     mcpaudit.Auditor         // T3 工具调用审计器（ensureAuditor 惰性创建）
	auditCancel func()                   // 审计事件订阅退订（Shutdown 用）
	// signalQuit 通知「外部已在收尾」（wails dev 的 Ctrl+C 会先打到 CLI 再到我们），
	// 让信号处理器不必自己重复退出
	signalQuit chan struct{}
	// quitAllowed 前端已确认退出（Alt+F4 / 系统关闭时先被 WindowShouldClose 拦截，
	// 前端弹「保存并退出」确认后置 true 再调 Quit，避免直接 Quit 再次触发关闭而循环）
	quitAllowed bool
	// 任务栏驻留（系统托盘）：trayOnce 保证管理器只建一次，tray 为 nil = 从未启用
	tray     *tray.Manager
	trayOnce sync.Once
	// trayWasMaximised 记录「隐藏到托盘」前窗口是否最大化；WindowHide 不保留最大化态，
	// 恢复时需据此重新 WindowMaximise，否则全屏状态下最小化到托盘再点开会退回窗口模式。用 atomic 跨 goroutine 读写。
	trayWasMaximised atomic.Bool
}

// inflightSend 一次在途发送的取消句柄（带工作目录归属，见 sendCancels 的键说明）。
type inflightSend struct {
	cancel context.CancelFunc
	root   string
	uid    string
}

func NewApp() *App {
	a := &App{
		settings:    config.Default(),
		colls:       map[string]*collEntry{},
		eventSubs:   map[int]*eventSub{},
		sendCancels: map[string]*inflightSend{},
	}
	if s, err := config.Load(); err == nil {
		a.settings = s
	}
	return a
}

// ---------- 应用事件订阅（浏览器态 / devserver） ----------

// eventSub 一路订阅：SSE 连接各自持有一个 channel，断开时退订。
type eventSub struct {
	ch   chan map[string]any
	self int
	// dropped 该订阅因「积压太多」被丢弃的事件数（P2-18）。
	// 只在 emitAppEvent 的发送方 goroutine 上自增（每个订阅同一时刻只有一个发送者持锁快照后串行发），
	// 读取方是诊断/测试，用 atomic 避免与读者构成竞争。
	dropped atomic.Int64
}

// SubscribeEvents 订阅应用事件（collection:changed / mcp:authz-* 等）。
// channel 元素是统一信封 {"name":事件名,"payload":载荷}；devserver 按 name 透传成 SSE 帧。
// 桌面态走 Wails EventsEmit，不需要订阅。退订由返回的 func 负责。
func (a *App) SubscribeEvents() (<-chan map[string]any, func()) {
	a.mu.Lock()
	a.eventSubSeq++
	id := a.eventSubSeq
	ch := make(chan map[string]any, 16)
	sub := &eventSub{ch: ch, self: id}
	a.eventSubs[id] = sub
	a.mu.Unlock()
	return ch, func() {
		// 只从订阅列表移除、不 close channel：fan-out 持有快照在锁外发送，close 会引发向已关闭通道发送
		a.mu.Lock()
		delete(a.eventSubs, id)
		a.mu.Unlock()
	}
}

// setAppCtx 记录 Wails 运行时上下文。
//
// P2-9：ctx 存在结构体里是 Wails 的既定形态（绑定方法不带 ctx 参数），
// 但写入发生在 Startup / DomReady 钩子里，读取发生在任意前端调用或后台 goroutine 上，
// 裸字段构成数据竞争。统一走 setter/getter，所有读写都在 a.mu 下。
func (a *App) setAppCtx(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
}

// appCtx 取 Wails 运行时上下文；非桌面环境（devserver）返回 nil。
func (a *App) appCtx() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ctx
}

// emitAppEvent 向下行发送命名应用事件：桌面态走 Wails EventsEmit，浏览器态经
// SubscribeEvents 的信封通道由 devserver SSE 转发（事件名由载荷自带，不再硬编码）。
// 订阅方堵塞时丢弃该事件，保证调用方（含 MCP 工具调用线程）不被慢前端拖住。
func (a *App) emitAppEvent(name string, payload any) {
	a.mu.Lock()
	ctx := a.ctx
	subs := make([]*eventSub, 0, len(a.eventSubs))
	for _, s := range a.eventSubs {
		subs = append(subs, s)
	}
	a.mu.Unlock()
	if ctx != nil {
		runtime.EventsEmit(ctx, name, payload)
	}
	ev := map[string]any{"name": name, "payload": payload}
	for _, sub := range subs {
		// P2-18：通道满时丢**最旧**的一条再塞新的，而不是直接丢掉新事件。
		// 事件语义是「状态变更通知」（前端收到就重扫），丢新的会让界面停在过期状态，
		// 丢旧的则最坏只是少刷一次中间态；同时累加 dropped，便于判断前端是否长期跟不上。
		select {
		case sub.ch <- ev:
			continue
		default:
		}
		select {
		case <-sub.ch:
			sub.dropped.Add(1)
		default:
		}
		select {
		case sub.ch <- ev:
		default:
			sub.dropped.Add(1)
		}
	}
}

// 初始窗口几何（逻辑像素；Wails 的 Width/Height 与 ScreenGetAll 都是逻辑像素）。
//
// 窗口尺寸 = 内容尺寸 + 四周的前端留白。留白来自 `--app-frame-gap`（给自绘阴影让位，见 base.css），
// 但它**不是每个平台都有**：Windows 拿不到逐像素透明（Wails 走 blur-behind / SystemBackdropType），
// 留白会变成窗口四周一圈厚白框，所以前端在 windows 上把留白归零、改用系统自带的 Aero 阴影与圆角。
// 因此这里按平台算，内容区在三端都是同一尺寸（1600×1000 / 最小 1120×720）。
const (
	contentWindowWidth  = 1600
	contentWindowHeight = 1000
	minContentWidth     = 1120
	minContentHeight    = 720
	// frameGap 与前端 --app-frame-gap 的桌面态取值保持一致（改了要同步，见 base.css）
	frameGap = 32
	// 首选尺寸四周留白下限：避免窗口贴着屏幕边缘
	windowFitMargin = 24
	// 任务栏 / Dock 的保守预留：ScreenGetAll 只给显示器矩形，不含工作区
	windowVerticalReserve = 64
)

// windowFrameInset 是窗口尺寸里为前端阴影留白预留的总量（四周各一份）。
// Windows 不预留（见上面的说明），返回 0。
func windowFrameInset() int {
	if goruntime.GOOS == "windows" {
		return 0
	}
	return 2 * frameGap
}

var (
	preferredWindowWidth  = contentWindowWidth + windowFrameInset()
	preferredWindowHeight = contentWindowHeight + windowFrameInset()
)

// MinWindowWidth / MinWindowHeight 供 main.go 设置启动尺寸下限（同样含留白）。
func MinWindowWidth() int  { return minContentWidth + windowFrameInset() }
func MinWindowHeight() int { return minContentHeight + windowFrameInset() }

// startup Wails 生命周期。
// Startup Wails 生命周期钩子（必须导出：OnStartup 引用跨包方法）。
func (a *App) Startup(ctx context.Context) {
	a.setAppCtx(ctx)
	a.signalQuit = make(chan struct{})
	a.watchSignals()
	// T2/T3：把授权 UI 桥与审计器注入内嵌 MCP 服务（开关关时授权链为 nil；审计恒开）。
	if a.mcpBackend != nil {
		a.mcpBackend.SetAuthorizer(a.buildMCPAuthorizer())
		if aud := a.ensureAuditor(); aud != nil {
			a.mcpBackend.SetAuditor(aud)
		}
	}
	// 设置里启用了 MCP 就随应用起服务；此时通常还没打开集合，状态会提示先打开集合
	if a.settings.MCP.Enabled {
		a.applyMCP("")
	}
	// 任务栏驻留：是否常驻由「关闭窗口时」派生（非「直接退出」即启动托盘）
	a.applyTray(trayResidentOf(a.settings.TrayCloseAction))
}

// DomReady Wails 生命周期钩子：窗口与 WebView 就绪、首帧渲染前，把初始几何收敛到屏幕内再显示窗口。
//
// 为什么需要：Wails 的 Width/Height 是**逻辑像素**，高分屏（如 125% 缩放）下逻辑屏可能只有
// 1536×960，而首选 1600×1000 放不下 —— 系统会把它按工作区居中，于是顶部/底部被推出屏幕
// （实测：逻辑屏 1536×960 时窗口矩形 T=-33、B=945）。ScreenGetAll 返回的 Size 恰好是逻辑像素，
// 用它判断：放得下就保持首选尺寸并居中，放不下就直接最大化（Windows 侧 Wails 已按工作区裁剪客户区，
// 任务栏自动让开）。窗口先隐藏（main.go 的 StartHidden），自适应完再 Show，避免启动闪一下再跳。
func (a *App) DomReady(ctx context.Context) {
	a.setAppCtx(ctx)
	a.fitInitialWindow(ctx)
	runtime.WindowShow(ctx)
}

// SetQuitAllowed 前端在确认退出后调用：把关闭守卫放行（见 WindowShouldClose 注释）。
func (a *App) SetQuitAllowed(allowed bool) {
	a.mu.Lock()
	a.quitAllowed = allowed
	a.mu.Unlock()
}

// OnBeforeClose 拦截系统关闭（Alt+F4 / 任务栏 / 系统关闭），让前端先确认未保存改动。
// 返回 true = 阻止关闭；false = 放行。
//
// 与 OnShutdown 的配合：前端确认退出后先调 SetQuitAllowed(true) 再 Quit()。
// 之所以需要这个守卫：避免「前端调 Quit() 时若某些平台仍触发关闭回调」造成反复拦截/确认的循环。
func (a *App) OnBeforeClose(ctx context.Context) bool {
	a.mu.Lock()
	allowed := a.quitAllowed
	a.quitAllowed = false
	a.mu.Unlock()
	if allowed {
		return false
	}
	runtime.EventsEmit(ctx, "app:close-requested", nil)
	return true
}

// fitInitialWindow 按主屏可用范围决定初始尺寸：放得下 = 首选尺寸居中，放不下 = 最大化。
func (a *App) fitInitialWindow(ctx context.Context) {
	screens, err := runtime.ScreenGetAll(ctx)
	if err != nil || len(screens) == 0 {
		return // 拿不到屏幕信息：保持 main.go 里的安全下限尺寸，交给系统摆放
	}
	screen := screens[0]
	for _, s := range screens {
		if s.IsPrimary {
			screen = s
			break
		}
	}
	if windowFitsScreen(screen.Size.Width, screen.Size.Height) {
		runtime.WindowSetSize(ctx, preferredWindowWidth, preferredWindowHeight)
		runtime.WindowCenter(ctx)
		return
	}
	runtime.WindowMaximise(ctx)
}

// windowFitsScreen 首选尺寸能否完整放进该屏幕（含任务栏预留与四周留白）。
func windowFitsScreen(screenW, screenH int) bool {
	return screenW >= preferredWindowWidth+windowFitMargin &&
		screenH >= preferredWindowHeight+windowVerticalReserve
}

// SetHeadlessDir 供 devserver（无对话框环境）使用。
func (a *App) SetHeadlessDir(dir string) { a.headlessDir = dir }

// requireCollection / collOf 见 multicoll.go：集合从集合表里按「活动根 / 指定 uid」解析。

// PickDirectory 选择集合目录（桌面 = 系统对话框；devserver = 启动参数）。
func (a *App) PickDirectory() (string, error) {
	if a.headlessDir != "" {
		return a.headlessDir, nil
	}
	ctx := a.appCtx()
	if ctx == nil {
		return "", errors.New("应用未就绪")
	}
	return runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{Title: "选择集合目录"})
}

// PickFile 选择本地文件（multipart 上传用）。
func (a *App) PickFile() (string, error) {
	ctx := a.appCtx()
	if ctx == nil {
		return "", errors.New("应用未就绪")
	}
	return runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{Title: "选择上传文件"})
}

// PickGrpcProtoFiles 选择 .proto 定义（可多选，带 .proto 过滤）：gRPC 请求的「导入 / 更新定义」用。
//
// 必须支持多选：一个服务的定义常拆在多个文件（service 与 message 分文件 + 同目录互相 import），
// 分开导入会因「import 依赖还没进集合」而解析失败（设计文档 D2/G2.2）。
func (a *App) PickGrpcProtoFiles() ([]string, error) {
	ctx := a.appCtx()
	if ctx == nil {
		return nil, errors.New("应用未就绪")
	}
	return runtime.OpenMultipleFilesDialog(ctx, runtime.OpenDialogOptions{
		Title:   "选择 .proto 定义",
		Filters: []runtime.FileFilter{{DisplayName: "Proto 定义 (*.proto)", Pattern: "*.proto"}},
	})
}

// OpenCollection 打开（不存在则初始化）一个工作目录并置为活动根，返回概要（树 + 环境）。
//
// 多根并存后语义从「替换」改为「追加」：同一目录重复打开是**幂等**的（只置为活动根、
// 不重复起监听、不重建索引），这样「恢复上次打开的目录」与「欢迎页再点一次」都安全。
func (a *App) OpenCollection(dir string) (*collection.CollectionInfo, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("目录为空")
	}
	root := rootKey(dir)

	// 已经打开过同一目录：只置为活动根（幂等）
	a.mu.Lock()
	if e := a.colls[root]; e != nil {
		a.activeRoot = root
		exist := e.coll
		mcpOn := a.settings.MCP.Enabled
		a.mu.Unlock()
		a.afterActiveChange(mcpOn)
		// 概要可能是陈旧的（期间有外部改动）：重扫一次再返回，避免前端拿到旧树
		return a.refreshInfo(exist)
	}
	a.mu.Unlock()

	c, err := collection.Open(dir)
	if err != nil {
		return nil, err
	}
	// 集合自己也会规范化目录：以它的结果为准（软链接/相对路径写法可能不同）
	key := rootKey(c.Dir)

	// 索引键必须在**登记进 a.colls 之前**定下来。
	// 一旦登记，任何 goroutine 都能拿到这份集合去搜索/重建索引；此时再去改它的索引键
	// （SetIndexKey/RebuildIndex 会换掉内部句柄）就是锁外改共享状态——P1-10。
	// 集合此刻还没有任何外部引用，改它是安全的。
	a.mu.Lock()
	if e := a.colls[key]; e != nil {
		a.activeRoot = key
		dup := e.coll
		mcpOn := a.settings.MCP.Enabled
		a.mu.Unlock()
		_ = c.Close() // 丢掉这份没人消费它的事件与索引句柄
		a.afterActiveChange(mcpOn)
		return a.refreshInfo(dup)
	}
	// 同一份集合的拷贝（清单 uid 相同、路径不同）已打开：索引按 uid 会落到同一个 sqlite 文件，
	// 两边的搜索结果 / 同步哈希会互相污染，给后来者换一个区分用的索引键
	uidTaken := false
	for other, e := range a.colls {
		if other != key && e.coll.UID == c.UID {
			uidTaken = true
			break
		}
	}
	a.mu.Unlock()

	if uidTaken {
		c.SetIndexKey(c.UID + "--" + shortHash(key))
		_ = c.RebuildIndex() // 新键指向的索引文件是空的，先按磁盘重建一份
	}

	// 回到锁内正式登记：期间可能已被别的入口抢先打开同一目录，按同样规则让位。
	a.mu.Lock()
	if e := a.colls[key]; e != nil {
		a.activeRoot = key
		dup := e.coll
		mcpOn := a.settings.MCP.Enabled
		a.mu.Unlock()
		_ = c.Close()
		a.afterActiveChange(mcpOn)
		return a.refreshInfo(dup)
	}
	a.colls[key] = &collEntry{coll: c}
	a.collOrder = append(a.collOrder, key)
	a.activeRoot = key
	mcpOn := a.settings.MCP.Enabled
	a.mu.Unlock()

	go a.watchLoop(c, key)
	// 新开工作目录后让 MCP 重扫项目目录（不重启服务；白名单来自设置，根变化不改配置）
	if mcpOn {
		a.refreshMCP()
	}
	info, err := a.refreshInfo(c)
	if err != nil {
		// 列不出概要的根留着只会是坏状态：撤回登记，把原因如实回报
		a.mu.Lock()
		delete(a.colls, key)
		a.collOrder = removeFromOrder(a.collOrder, key)
		if a.activeRoot == key {
			if n := len(a.collOrder); n > 0 {
				a.activeRoot = a.collOrder[n-1]
			} else {
				a.activeRoot = ""
			}
		}
		a.mu.Unlock()
		_ = c.Close()
		return nil, err
	}
	return info, nil
}

// watchLoop 把某个工作目录的外部改动事件转发给前端（Wails EventsEmit；devserver 经 SubscribeEvents 的 SSE 转发）。
//
// 退出条件有两个：watcher 关闭信号（CloseCollection / Shutdown 时 StopWatch）与事件通道关闭。
// 只等事件通道会在根被关掉后永久阻塞（通道不会被关闭），每个被关掉的根都泄漏一个 goroutine。
//
// 事件里必须带上工作目录标识（root）：多根并存时前端只重载「发生改动的那一根」，
// 而不是把 N 个根全量重扫。
func (a *App) watchLoop(c *collection.Collection, root string) {
	ch := c.WatchEvents()
	if ch == nil {
		return
	}
	done := c.WatchDone()
	for {
		var ev watch.Event
		select {
		case e, ok := <-ch:
			if !ok {
				return
			}
			ev = e
		case <-done:
			return
		}
		a.mu.Lock()
		// 该集合是否仍是已登记的根（root + 指针身份双保险）：被关掉的根不再上报
		e := a.colls[root]
		still := e != nil && e.coll == c
		a.mu.Unlock()
		if !still {
			return
		}
		// 同步重建索引，保证搜索结果与磁盘一致
		_ = c.RebuildIndex()
		// 顺带刷新该根的概要缓存：前端收到事件后重列根行（ListCollections）就是最新的，
		// 不必再为「看树」单独走一次全量扫描
		if info, ierr := c.Info(); ierr == nil {
			a.mu.Lock()
			if cur := a.colls[root]; cur != nil && cur.coll == c {
				cur.info = info
			}
			a.mu.Unlock()
		}
		// 桌面 Wails 事件 + 浏览器 SSE 信封统一从 emitAppEvent 下发
		a.emitAppEvent("collection:changed", map[string]any{"root": root, "paths": ev.Paths})
	}
}

// ReloadCollection 重新扫描**活动根**（外部改动 / git 操作后）。
// 指定其它根用 ReloadCollectionOf(uid)。
func (a *App) ReloadCollection() (*collection.CollectionInfo, error) {
	return a.ReloadCollectionOf("")
}

// SearchIndex 本地索引搜索（C1/H4）：按标题/URL/方法/路径模糊匹配。
//
// limit <= 0 = 不限条数（取全量）。内部调用方（冲突检测要全部请求的 hash、MCP 的
// hashOf/searchIn）都按「0 = 全量」使用；此前在这里兜底成 200，会让 >200 个请求的
// 集合静默丢结果（冲突检测直接退化成不检测）。界面搜索自己传 limit。
func (a *App) SearchIndex(q string, limit int) ([]index.Node, error) {
	return a.SearchIndexFor("", q, limit)
}

// SearchIndexFor 按工作目录检索本地索引（root 为空 = 活动根）。
// 名称匹配在前端即时完成，这里补 URL/方法/路径 的索引口径，多根场景（侧栏、跨目录搜索面板）逐根查询。
func (a *App) SearchIndexFor(root string, q string, limit int) ([]index.Node, error) {
	c, err := a.collOf(root)
	if err != nil {
		return nil, err
	}
	return c.Search(q, limit)
}

// RebuildIndex 手动重建本地索引（删库后可一键恢复）。
func (a *App) RebuildIndex() error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.RebuildIndex()
}
