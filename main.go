package main

import (
	"embed"
	"log"
	"os"

	"api-doc-go-client/internal/app"
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
	err := wails.Run(&options.App{
		Title: "api-doc-go",
		// 初始尺寸只给「安全下限」：真正尺寸在 DomReady 里按屏幕可用范围决定（见 app.DomReady）——
		// Width/Height 是逻辑像素，高分屏（125% 缩放）下逻辑屏可能只有 1536×960，
		// 首选 1600×1000 放不下就会被系统居中到屏幕外（顶部/底部被推出）。
		Width:     1120,
		Height:    720,
		MinWidth:  1120,
		MinHeight: 720,
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
		// 退出前收尾：停内嵌 MCP 服务（释放端口）与集合监听。
		// wails dev 的 Ctrl+C 打到应用时也走这里（见 internal/app 的 watchSignals 注释）。
		OnShutdown: core.Shutdown,
		Bind:             []interface{}{core},
		// 三端都显式开透明：Linux 侧由 gdk_screen_is_composited 守卫（无合成器时保持不透明），
		// Windows 侧走 DWM（< 22621 退化为 blur-behind），macOS 侧置 window.opaque = NO。
		Windows: &windows.Options{WindowIsTranslucent: true, WebviewIsTransparent: true},
		Mac:     &mac.Options{WindowIsTranslucent: true, WebviewIsTransparent: true},
		Linux: &linux.Options{
			WindowIsTranslucent: true,
			WebviewGpuPolicy:    linux.WebviewGpuPolicyOnDemand,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
