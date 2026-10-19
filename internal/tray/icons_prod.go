//go:build !(dev || devbuild)

package tray

import _ "embed"

// 正式版托盘图标：Windows 用 ICO（原生 API 要求），Linux 用 PNG。
var (
	//go:embed icons/icon-prod.ico
	IconWindows []byte
	//go:embed icons/icon-prod.png
	IconLinux []byte
)
