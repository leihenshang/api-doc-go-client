//go:build dev || devbuild

package tray

import _ "embed"

// 开发版托盘图标（与正式版区分，避免两个托盘图标混淆）。
var (
	//go:embed icons/icon-dev.ico
	IconWindows []byte
	//go:embed icons/icon-dev.png
	IconLinux []byte
)
