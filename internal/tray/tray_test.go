package tray

import (
	"os"
	"testing"
	"time"
)

// TestStartIsIdempotent 锁死「托盘只启不停」这条不变量。
//
// 回归背景：energye/systray v1.0.3 的 Quit() 是 sync.Once + 包级 channel，
// 停掉之后再 Start 会在库自起的 goroutine 里 panic（dbus: connection closed by user），
// 直接把整个应用进程带走。Manager 因此不再提供 Stop，Start 必须幂等。
//
// 需要 DBus 会话总线；CI/无桌面环境下跳过。
func TestStartIsIdempotent(t *testing.T) {
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		t.Skip("无 DBus 会话总线，跳过托盘实跑")
	}
	m := NewManager(Callbacks{OnShow: func() {}, OnQuit: func() {}})
	m.Start(IconWindows, IconLinux)
	// 连续重复调用：第二次起必须是纯 no-op，绝不能再次进入 systray.Run
	for i := 0; i < 3; i++ {
		m.Start(IconWindows, IconLinux)
	}
	time.Sleep(1500 * time.Millisecond) // 给 onReady 留出执行时间（panic 会在此期间炸出来）
	if !m.Started() {
		t.Fatal("Start 后 Started() 应为 true")
	}
}
