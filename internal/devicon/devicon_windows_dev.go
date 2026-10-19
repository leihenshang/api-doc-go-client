//go:build windows && (dev || devbuild)

// Package devicon 仅开发版 Windows 生效：把窗口/任务栏图标换成 icon-dev.ico。
//
// wails v2 的运行时选项不支持换图标（exe 图标在打包时烘进 syso，且 dev 与正式版
// 共用 build/windows/icon.ico），这里改用 Win32 惯例：PrivateExtractIconsW 从
// 临时落盘的 .ico 取 HICON，FindWindowW 按窗口标题轮询到句柄后 WM_SETICON 覆盖。
// 标题是启动参数（appTitle，运行期无人改），窗口创建前 FindWindow 也能命中隐藏窗口。
package devicon

import (
	_ "embed"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

//go:embed icon-dev.ico
var devIco []byte

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	procFindWindow   = user32.NewProc("FindWindowW")
	procSendMessage  = user32.NewProc("SendMessageW")
	procExtractIcons = user32.NewProc("PrivateExtractIconsW")
)

const (
	wmSetIcon = 0x0080
	iconSmall = 0
	iconBig   = 1
)

// Apply 后台把开发版图标设到标题为 title 的窗口；失败静默（只影响观感，不影响功能）。
func Apply(title string) {
	go applyLoop(title)
}

func applyLoop(title string) {
	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	tmp := filepath.Join(os.TempDir(), "api-doc-go-dev-icon.ico")
	if err := os.WriteFile(tmp, devIco, 0o600); err != nil {
		return
	}
	defer func() { _ = os.Remove(tmp) }()
	pathPtr, err := syscall.UTF16PtrFromString(tmp)
	if err != nil {
		return
	}
	small := loadIcon(pathPtr, 16)
	big := loadIcon(pathPtr, 32)
	if small == 0 && big == 0 {
		return
	}
	// 窗口在 wails.Run 里异步创建：最多轮询 ~10s，命中即设两个尺寸后收工
	for i := 0; i < 100; i++ {
		hwnd, _, _ := procFindWindow.Call(0, uintptr(unsafe.Pointer(titlePtr)))
		if hwnd != 0 {
			if small != 0 {
				procSendMessage.Call(hwnd, wmSetIcon, iconSmall, small)
			}
			if big != 0 {
				procSendMessage.Call(hwnd, wmSetIcon, iconBig, big)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// loadIcon 从 .ico 文件按尺寸抽一个 HICON（取索引 0 的最佳匹配）。
func loadIcon(path *uint16, size int) uintptr {
	var hicon uintptr
	var id uint32
	n, _, _ := procExtractIcons.Call(
		uintptr(unsafe.Pointer(path)), 0,
		uintptr(size), uintptr(size),
		uintptr(unsafe.Pointer(&hicon)), uintptr(unsafe.Pointer(&id)),
		1, 0,
	)
	if n == 0 {
		return 0
	}
	return hicon
}
