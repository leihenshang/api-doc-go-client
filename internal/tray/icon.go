package tray

import (
	"runtime"

	"github.com/energye/systray"
)

// setIcon 按平台设置图标：Windows 需要 ICO 字节，Linux 用 PNG。
// 传入的字节类型由调用方保证（App 侧按 GOOS 选），这里做兜底：缺失时退回另一种。
func setIcon(iconWindows, iconLinux []byte) {
	if runtime.GOOS == "windows" {
		if len(iconWindows) > 0 {
			systray.SetIcon(iconWindows)
			return
		}
		systray.SetIcon(iconLinux) // 兜底：Windows 也能解析部分 PNG（systray 会尝试转换）
		return
	}
	if len(iconLinux) > 0 {
		systray.SetIcon(iconLinux)
		return
	}
	systray.SetIcon(iconWindows)
}
