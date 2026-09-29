package main

import (
	"embed"
	"log"

	"api-doc-go-client/internal/app"
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
	err := wails.Run(&options.App{
		Title:     "api-doc-go",
		Width:     1600,
		Height:    1000,
		MinWidth:  1120,
		MinHeight: 720,
		// 无边框窗口：标题栏由前端自绘（design-spec §2），拖动区靠 CSS --wails-draggable
		Frameless: true,
		// 窗口整体透明（alpha 0）：外壳圆角由 CSS 裁出（styles/base.css 的 --app-radius-window），
		// 圆角以外的像素要透出桌面；任何不透明底色都会把圆角顶成直角。
		BackgroundColour: &options.RGBA{},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        core.Startup,
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
