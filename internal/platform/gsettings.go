// Package platform 桌面环境兼容处理：抹平不同 Linux 环境下 GTK/WebView 的依赖差异。
package platform

import (
	"log"
	"os"
	"path/filepath"
)

// EnsureGSettingsBackend 在没有 D-Bus 会话总线时改用 GSettings 内存 backend。
//
// 原因：GTK 的文件选择器会通过 dconf 持久化状态（最近目录等），dconf 需要会话总线；
// 若无总线也没有 dbus-launch（未装 dbus-x11），GLib 会打印
//
//	dconf-WARNING **: failed to commit changes to dconf: Failed to execute child
//	process "dbus-launch" (No such file or directory)
//
// 该警告不影响功能，但在 WSLg / 最小容器里很吵。内存 backend 只是不再持久化对话框状态，
// 与本客户端无关（"上次打开的集合"由前端自己持久化）。
// 用户显式设置过 GSETTINGS_BACKEND 时不覆盖。
func EnsureGSettingsBackend() {
	if os.Getenv("GSETTINGS_BACKEND") != "" || hasSessionBus() {
		return
	}
	if err := os.Setenv("GSETTINGS_BACKEND", "memory"); err != nil {
		return
	}
	log.Println("platform: 未检测到 D-Bus 会话总线，使用 GSETTINGS_BACKEND=memory（文件对话框不再输出 dconf 警告）")
}

// hasSessionBus 判断当前环境是否有可用的会话总线。
func hasSessionBus() bool {
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") != "" {
		return true
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, "bus"))
	return err == nil
}
