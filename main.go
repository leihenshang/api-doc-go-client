package main

import (
	"embed"
	"log"
	"os"

	"api-doc-go-client/internal/app"
	"api-doc-go-client/internal/appicon"
	"api-doc-go-client/internal/devicon"
	mcpsrv "api-doc-go-client/internal/mcp"
	"api-doc-go-client/internal/platform"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// 须在 wails.Run（GTK 初始化）之前：无会话总线时改用内存 backend，避免文件对话框刷 dconf 警告
	platform.EnsureGSettingsBackend()
	core := app.NewApp()
	// 内嵌 MCP 服务：实现放在 internal/mcp（那边已经要用 App 的集合运行时，
	// 反向依赖会成环），这里注入；设置页「MCP 服务」分区靠它启停。
	core.SetMCPBackend(mcpsrv.NewBackend(log.New(os.Stderr, "mcp ", log.LstdFlags)))
	// 开发版（dev 标签）Windows：后台把窗口/任务栏图标换成 dev 版（正式版与非 Windows 为空实现）。
	devicon.Apply(appTitle)
	err := wails.Run(&options.App{
		Title: appTitle,
		// 初始尺寸只给「安全下限」：真正尺寸在 DomReady 里按屏幕可用范围决定（见 app.DomReady）——
		// Width/Height 是逻辑像素，高分屏（125% 缩放）下逻辑屏可能只有 1536×960，
		// 首选 1600×1000 放不下就会被系统居中到屏幕外（顶部/底部被推出）。
		// 数值含窗口外框留白（前端 --app-frame-gap，桌面态 32px；Windows 不预留，见 app.windowFrameInset）。
		// 内容区固定 1120×720 起，随平台换算成窗口尺寸。
		Width:     app.MinWindowWidth(),
		Height:    app.MinWindowHeight(),
		MinWidth:  app.MinWindowWidth(),
		MinHeight: app.MinWindowHeight(),
		// 无边框窗口：标题栏由前端自绘（design-spec §2），拖动区靠 CSS --wails-draggable
		Frameless: true,
		// 先隐藏：DomReady 里自适应尺寸/位置后再显示，避免启动瞬间闪一下超大窗口
		StartHidden: true,
		// 窗口整体透明（alpha 0）：外壳圆角由 CSS 裁出（styles/base.css 的 --app-radius-window），
		// 圆角以外的像素要透出桌面；任何不透明底色都会把圆角顶成直角。
		BackgroundColour: &options.RGBA{},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        core.Startup,
		OnDomReady:       core.DomReady,
		// Alt+F4 / 系统关闭先拦截：前端确认未保存改动后再放行（见 app.OnBeforeClose）
		OnBeforeClose: core.OnBeforeClose,
		// 退出前收尾：停内嵌 MCP 服务（释放端口）与集合监听。
		// wails dev 的 Ctrl+C 打到应用时也走这里（见 internal/app 的 watchSignals 注释）。
		OnShutdown: core.Shutdown,
		Bind:       []interface{}{core},
		// 三端都开透明：Linux 由 gdk_screen_is_composited 守卫（无合成器时保持不透明），
		// macOS 置 window.opaque = NO，Windows 走 DWM（Win11 22H2+ 是系统材质，
		// 更早的系统退化成 blur-behind）。
		//
		// 注意：Windows 上的透明**不是逐像素的**，页面透明的像素会被填成系统材质（浅色下近乎白色）。
		// 所以前端在 windows 下把外框留白归零、不画自绘阴影（见 styles/base.css 的 windows 覆盖），
		// 外壳直接铺满窗口、阴影交给系统的 Aero 阴影 —— 否则那圈留白就是一圈厚白框。
		Windows: &windows.Options{WindowIsTranslucent: true, WebviewIsTransparent: true},
		Mac:     &mac.Options{WindowIsTranslucent: true, WebviewIsTransparent: true},
		Linux: &linux.Options{
			Icon:                appicon.Data,
			ProgramName:         appicon.ProgramName,
			WindowIsTranslucent: true,
			WebviewGpuPolicy:    linux.WebviewGpuPolicyOnDemand,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
