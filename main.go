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
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// 须在 wails.Run（GTK 初始化）之前：无会话总线时改用内存 backend，避免文件对话框刷 dconf 警告
	platform.EnsureGSettingsBackend()
	core := app.NewApp()
	// 窗口底色跟随保存的主题（暗色下启动/缩放不露白边）
	bgR, bgG, bgB := app.InitialWindowBackground()
	err := wails.Run(&options.App{
		Title:     "api-doc-go",
		Width:     1600,
		Height:    1000,
		MinWidth:  1120,
		MinHeight: 720,
		// 无边框窗口：标题栏由前端自绘（design-spec §2），拖动区靠 CSS --wails-draggable
		Frameless:        true,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: bgR, G: bgG, B: bgB, A: 1},
		OnStartup:        core.Startup,
		Bind:             []interface{}{core},
		Linux: &linux.Options{
			WebviewGpuPolicy: linux.WebviewGpuPolicyOnDemand,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
