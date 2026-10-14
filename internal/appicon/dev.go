//go:build dev

// Package appicon 提供 Wails Linux 窗口图标与稳定的 WM_CLASS 程序名。
// dev 版本使用开发版图标与程序名，避免与正式版在任务栏混叠。
package appicon

import _ "embed"

//go:embed appicon_dev.png
var Data []byte

// ProgramName 作为 GTK g_set_prgname() 的值，决定窗口的 WM_CLASS。
// 使用与正式版不同的类名，使开发版在 Deepin/DDE 任务栏中独立显示与分组。
const ProgramName = "ApiDocClientDev"
