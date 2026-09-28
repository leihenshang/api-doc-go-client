package main

import (
	"embed"
	"log"

	"api-doc-go-client/internal/app"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	core := app.NewApp()
	err := wails.Run(&options.App{
		Title:            "API-DOC Client",
		Width:            1360,
		Height:           860,
		MinWidth:         980,
		MinHeight:        640,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 255, G: 255, B: 255, A: 1},
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
