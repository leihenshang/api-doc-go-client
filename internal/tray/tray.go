// Package tray 提供系统托盘驻留（任务栏通知区图标）能力，Windows / Linux 通用。
//
// 实现基于 energye/systray v1.0.3：Windows 走原生 Shell_NotifyIcon，
// Linux 走 DBus 的 StatusNotifierItem 协议（Deepin/DDE、KDE、GNOME 均支持，无需额外运行库）。
// macOS 未适配（本项目不发 mac 包）。
//
// 交互：双击托盘图标 = 显示主窗口；右键 = 菜单（显示主窗口 / 退出）。
package tray

import (
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/energye/systray"
)

// Callbacks 托盘交互回调（由 App 注入，见 internal/app/tray.go）。
type Callbacks struct {
	OnShow func() // 双击图标 / 菜单「显示主窗口」
	OnQuit func() // 菜单「退出」：等价于用户确认退出（放行守卫 → Quit）
}

// Manager 托盘管理器。
//
// ⚠️ 进程内只能启动一次，且启动后不可停止（直到进程退出）—— 这是上游库的硬限制：
//
//  1. systray.Quit() 由 sync.Once 守护，quitChan 是包级全局 channel，关闭后无法重建；
//     再次 Run 时 nativeLoop 会立刻从已关闭的 channel 返回，托盘根本起不来。
//  2. 更糟的是 nativeStart() 在**重新赋值 instance.props 之前**就先触发了 onReady，
//     于是第二次启动时 onReady 里的 SetIcon 会打到上一条已 Close 的 DBus 连接上：
//     panic: dbus: connection closed by user —— 而它跑在库自己起的 goroutine 里，
//     无法被调用方 recover，直接把整个应用进程带走（表现为「程序消失」）。
//
// 因此本管理器刻意不提供 Stop：设置里关掉「驻留托盘」只改变关闭窗口的行为，
// 图标保留到应用退出；进程退出时 DBus 连接断开，桌面会自动摘掉图标，不会留残影。
type Manager struct {
	mu      sync.Mutex
	started bool
	cb      Callbacks
	// ready 在 onReady 完整成功（图标/菜单/回调就绪）后才置位；
	// started 只表示「发起过启动」，不代表可用——审核 M#5：HideToTray 需要区分两者。
	ready atomic.Bool
}

// NewManager 创建托盘管理器。
func NewManager(cb Callbacks) *Manager {
	return &Manager{cb: cb}
}

// Started 托盘是否已发起启动（含启动失败后不再重试的情况）。不代表可用，可用性用 Ready。
func (m *Manager) Started() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started
}

// Ready 托盘图标是否已真正可用（onReady 完整成功；panic/无 SNI 宿主都会停在 false）。
func (m *Manager) Ready() bool { return m.ready.Load() }

// WaitReady 等待托盘就绪，超时返回 false。供 HideToTray 前调用：
// 藏窗口之前必须确认图标存在，否则应用再也唤不回来（审核 M#5）。
func (m *Manager) WaitReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for !m.ready.Load() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
	return true
}

// Start 启动托盘（幂等，非阻塞）。整个进程生命周期内只有第一次调用真正生效。
// iconWindows 传 ICO 字节，iconLinux 传 PNG 字节。
func (m *Manager) Start(iconWindows, iconLinux []byte) {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	// 先置位再启动：即使 onReady 里出错也不重试 —— 重试正是上面第 2 条崩溃的触发条件。
	m.started = true
	m.mu.Unlock()

	go func() {
		// ⚠️ 必须锁 OS 线程：本库 init() 里的 LockOSThread 锁的是 main goroutine（假定你在
		// main 里直接 Run），但 Wails 独占主线程，只能把 systray.Run 放到新 goroutine 跑。
		// 若不锁，Windows 下托盘窗口（CreateWindowEx）与消息泵（GetMessage）会随 goroutine
		// 被调度到不同系统线程，而窗口消息只投递到创建它的那个线程 → 图标收不到点击/双击回调，
		// 表现为「最小化到托盘不生效 / 唤不回」。
		runtime.LockOSThread()
		// onExit 传空实现：我们从不调 Quit，进程退出时由运行时收尾。
		systray.Run(func() { m.onReady(iconWindows, iconLinux) }, func() {})
	}()
}

// onReady 托盘就绪：装图标、建菜单、挂事件。
//
// 这里跑在库自起的 goroutine 上（systray.Register 内部 go func(){ <-readyCh; onReady() }），
// 任何 panic 都会终止整个进程，所以必须自己 recover —— 托盘坏了不该连累主程序。
func (m *Manager) onReady(iconWindows, iconLinux []byte) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("tray: 托盘初始化失败（主程序不受影响）: %v", r)
		}
	}()

	setIcon(iconWindows, iconLinux)
	systray.SetTitle("")
	systray.SetTooltip(tooltip)

	mShow := systray.AddMenuItem(menuShow, menuShowTip)
	systray.AddSeparator()
	mQuit := systray.AddMenuItem(menuQuit, menuQuitTip)

	// energye/systray v1.0.3 为回调式菜单（ClickedCh 已私有化）
	if m.cb.OnShow != nil {
		mShow.Click(m.cb.OnShow)
		// 双击图标 = 显示主窗口。
		//   Linux：SNI 规范没有双击，库内用 500ms 间隔把两次 Activate 判定为双击后转 dActivate；
		//   Windows：WM_LBUTTONDBLCLK → onDClick。
		systray.SetOnDClick(func(systray.IMenu) { m.cb.OnShow() })
		// 单击显式设为空实现：Linux 侧若 activate 为 nil，库会向桌面返回
		// ErrMsgUnknownMethod，部分实现会因此不再上报后续 Activate（双击就废了）。
		systray.SetOnClick(func(systray.IMenu) {})
	}
	if m.cb.OnQuit != nil {
		mQuit.Click(m.cb.OnQuit)
	}
	// 菜单/回调全部挂完才置 ready：上方任何 panic 都会跳过这里（recover 后不再往后走），
	// 外部 WaitReady 超时即知道托盘不可用，从而避免 HideToTray 把应用藏进看不见的地方。
	m.ready.Store(true)
}
