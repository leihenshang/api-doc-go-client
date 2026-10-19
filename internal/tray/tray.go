// Package tray 提供系统托盘驻留（任务栏通知区图标）能力，Windows / Linux 通用。
//
// 实现基于 energye/systray：Windows 走原生 Shell_NotifyIcon，Linux 走 GTK/AppIndicator
// （Deepin/DDE 支持 AppIndicator 协议）。macOS 未适配（本项目不发 mac 包）。
//
// 生命周期：Enable 在独立 goroutine 里 systray.Run；Disable 调 systray.Quit 结束循环。
// 菜单：显示主窗口 / 退出 —— 回调由 App 注入（显示 = WindowShow，退出 = 放行关闭守卫后 Quit）。
package tray

import (
	"sync"

	"github.com/energye/systray"
)

// Callbacks 托盘菜单动作回调（由 App 注入，见 internal/app/tray.go）。
type Callbacks struct {
	OnShow func() // 「显示主窗口」：WindowShow + 前端刷新托盘状态
	OnQuit func() // 「退出」：等价于用户确认退出（放行守卫 → Quit）
}

type Manager struct {
	mu      sync.Mutex
	running bool
	cb      Callbacks
	// 点击托盘图标本身（Linux AppIndicator 部分桌面左键单击无事件；Windows 有）
	onClickCh func()
}

// NewManager 创建托盘管理器；同一个实例可反复 Enable/Disable。
func NewManager(cb Callbacks) *Manager {
	return &Manager{cb: cb}
}

// Enabled 当前托盘是否在运行。
func (m *Manager) Enabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// Enable 启动托盘（幂等：已运行则忽略）。阻塞直到 onReady 或出错。
// iconWindows 传 ICO 字节，iconLinux 传 PNG 字节。
func (m *Manager) Enable(iconWindows, iconLinux []byte) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()

	systray.Run(func() {
		setIcon(iconWindows, iconLinux)
		systray.SetTitle("")
		systray.SetTooltip(tooltip)

		mShow := systray.AddMenuItem(menuShow, menuShowTip)
		systray.AddSeparator()
		mQuit := systray.AddMenuItem(menuQuit, menuQuitTip)

		// energye/systray v1.0.3 为回调式菜单（ClickedCh 已私有化）
		if m.cb.OnShow != nil {
			mShow.Click(m.cb.OnShow)
		}
		if m.cb.OnQuit != nil {
			mQuit.Click(m.cb.OnQuit)
		}
	}, func() {
		// onExit：systray.Quit 返回后的收尾
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()
	})
}

// Disable 停止托盘（幂等）。
func (m *Manager) Disable() {
	m.mu.Lock()
	running := m.running
	m.mu.Unlock()
	if running {
		systray.Quit()
	}
}

// Restart 用于设置切换（图标/回调变化时其实不必重启，但保证状态干净）。
func (m *Manager) Restart(iconWindows, iconLinux []byte) {
	m.Disable()
	go m.Enable(iconWindows, iconLinux)
}
