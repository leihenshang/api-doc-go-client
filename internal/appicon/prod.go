//go:build !dev

// Package appicon 提供 Wails Linux 窗口图标与稳定的 WM_CLASS 程序名。
// prod 版本使用正式版绿色插头图标，程序名为 ApiDocClient。
package appicon

import _ "embed"

//go:embed appicon.png
var Data []byte

// ProgramName 作为 GTK g_set_prgname() 的值，决定窗口的 WM_CLASS。
// Deepin/DDE 等桌面环境的任务栏通过 StartupWMClass 与此值匹配，从而实现
// 正确的图标显示、分组与固定。
const ProgramName = "ApiDocClient"
