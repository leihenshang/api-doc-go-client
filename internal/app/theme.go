package app

import (
	"api-doc-go-client/internal/config"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// 窗口底色与前端的 --app-bg 对齐（浅色 #f5f5f5 / 暗色 #17181a）：
// 无边框窗口在启动、缩放、拖动边缘时露出的区域由原生底色填充，配色不一致会闪出白边。
var windowBG = map[string][3]uint8{
	config.ThemeLight: {0xf5, 0xf5, 0xf5},
	config.ThemeDark:  {0x17, 0x18, 0x1a},
}

func windowBackground(theme string) (uint8, uint8, uint8) {
	c, ok := windowBG[theme]
	if !ok {
		c = windowBG[config.ThemeLight]
	}
	return c[0], c[1], c[2]
}

// InitialWindowBackground 启动时的窗口底色：跟随磁盘里保存的主题。
func InitialWindowBackground() (uint8, uint8, uint8) {
	theme := config.Default().Theme
	if s, err := config.Load(); err == nil {
		theme = s.Theme
	}
	return windowBackground(theme)
}

// applyWindowTheme 主题切换后同步原生窗口底色；devserver 模式没有窗口（ctx 为空）时静默跳过。
func (a *App) applyWindowTheme(theme string) {
	if a.ctx == nil {
		return
	}
	r, g, b := windowBackground(theme)
	runtime.WindowSetBackgroundColour(a.ctx, r, g, b, 1)
}
