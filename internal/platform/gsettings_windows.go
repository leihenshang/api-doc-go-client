//go:build windows

package platform

// EnsureGSettingsBackend 在 Windows 上是空实现：GSettings/dconf 与 D-Bus 会话总线是
// Linux GTK 的机制，Windows 桌面既无会话总线也无 dconf，无需设置 GSETTINGS_BACKEND，
// 更不该在构建/运行时打印那条无关日志。真正的实现见 gsettings.go（//go:build !windows）。
func EnsureGSettingsBackend() {}
