// internal/app/tray.go —— 任务栏驻留（系统托盘）能力，见 internal/tray 包注释。
//
// 行为约定：
//   - 设置 settings.trayResident 开启时，Startup 后启动托盘；
//     SaveSettings 里开关变化即时启停（无需重启应用）。
//   - 「关闭窗口」在驻留开启时由前端分流为隐藏（见前端 quit.ts → HideToTray），
//     托盘菜单「退出」走真实退出（放行关闭守卫 → Quit → OnShutdown 停托盘）。
package app

import (
	"api-doc-go-client/internal/tray"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ensureTray 返回托盘管理器（首次调用时创建）。
func (a *App) ensureTray() *tray.Manager {
	a.trayOnce.Do(func() {
		a.tray = tray.NewManager(tray.Callbacks{
			OnShow: a.trayShow,
			OnQuit: a.trayQuit,
		})
	})
	return a.tray
}

// applyTray 按设置同步托盘状态（Startup 与 SaveSettings 都走这里）。
func (a *App) applyTray(enabled bool) {
	if enabled {
		if !a.ensureTray().Enabled() {
			go a.ensureTray().Enable(tray.IconWindows, tray.IconLinux)
		}
		return
	}
	if a.tray != nil {
		a.tray.Disable()
	}
}

// trayShow 托盘菜单「显示主窗口」。
func (a *App) trayShow() {
	ctx := a.appCtx()
	if ctx == nil {
		return
	}
	runtime.WindowUnminimise(ctx)
	runtime.WindowShow(ctx)
}

// trayQuit 托盘菜单「退出」：等价于前端确认退出（放行守卫再 Quit，见 OnBeforeClose 注释）。
func (a *App) trayQuit() {
	a.SetQuitAllowed(true)
	a.Quit()
}

// HideToTray 驻留模式下前端调用的「关闭窗口 = 隐藏」。
// 数据仍在内存/磁盘，无需走未保存确认；隐藏即可。
func (a *App) HideToTray() {
	ctx := a.appCtx()
	if ctx == nil {
		return
	}
	runtime.WindowHide(ctx)
}
