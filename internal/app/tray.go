// internal/app/tray.go —— 任务栏驻留（系统托盘）能力，见 internal/tray 包注释。
//
// 行为约定：
//   - settings.trayResident 开启时启动托盘（Startup 与 SaveSettings 都走 applyTray）；
//   - 托盘**只启不停**：上游库的 Quit 是 sync.Once + 包级 channel，停掉后本进程内
//     再也起不来，且二次启动会 panic 崩掉整个应用（详见 tray.Manager 的说明）。
//     所以关掉「驻留托盘」只改变关闭窗口的行为，图标保留到应用退出；
//   - 「关闭窗口」的行为由 settings.trayCloseAction 决定，分流在前端 quit.ts：
//     ask = 弹框问「放入托盘 / 直接关闭」，tray = 直接隐藏，quit = 直接退出；
//   - 托盘的「退出」走真实退出（放行关闭守卫 → Quit → OnShutdown）。
package app

import (
	"log"
	"time"

	"api-doc-go-client/internal/config"
	"api-doc-go-client/internal/tray"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// trayResidentOf 由「关闭窗口时」的取值派生是否常驻托盘：非「直接退出」（即 ask / tray）就常驻。
// 合并设置后前端只暴露单个下拉，TrayResident 字段不再单独决定行为（仅保留兼容）。
func trayResidentOf(closeAction string) bool {
	return closeAction != config.TrayCloseQuit
}

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

// applyTray 按设置启动托盘（幂等；enabled=false 时什么都不做，见文件头「只启不停」）。
func (a *App) applyTray(enabled bool) {
	if !enabled {
		return
	}
	a.ensureTray().Start(tray.IconWindows, tray.IconLinux)
}

// trayShow 显示主窗口（托盘菜单「显示主窗口」与双击图标共用）。
func (a *App) trayShow() {
	ctx := a.appCtx()
	if ctx == nil {
		return
	}
	runtime.WindowUnminimise(ctx)
	runtime.WindowShow(ctx)
	// 隐藏前是最大化（全屏）→ 恢复时重新最大化，否则 WindowHide/Show 会退回普通窗口尺寸。
	if a.trayWasMaximised.Load() {
		runtime.WindowMaximise(ctx)
	}
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
	// 兜底：隐藏前确保托盘已起。正常路径下开启驻留时就已启动，
	// 但前端可以在任何时刻调到这里 —— 没有托盘图标就把窗口藏起来，应用将再也唤不回来。
	// 审核 M#5：Start 只是「发起」；onReady panic（如无 DBus/SNI 宿主、图标设置失败）时
	// 托盘永远不可用 —— 等就绪，超时则退化为最小化（窗口可见可恢复），绝不藏进看不见的地方。
	a.applyTray(true)
	if !a.ensureTray().WaitReady(1200 * time.Millisecond) {
		log.Printf("tray: 未就绪，HideToTray 退化为最小化（避免应用不可达）")
		runtime.WindowMinimise(ctx)
		return
	}
	// 记住隐藏前的最大化态（WindowHide 不保留它），供 trayShow 恢复时重新最大化。
	a.trayWasMaximised.Store(runtime.WindowIsMaximised(ctx))
	runtime.WindowHide(ctx)
}
