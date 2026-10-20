package tray

import (
	"testing"
	"time"
)

// TestWaitReadyUnstarted 审核 M#5：未就绪（未启动/onReady panic）时 WaitReady 超时返回 false，
// 调用方据此退化为最小化而不是隐藏窗口。
func TestWaitReadyUnstarted(t *testing.T) {
	m := NewManager(Callbacks{})
	if m.Ready() {
		t.Fatal("初始应未就绪")
	}
	start := time.Now()
	if m.WaitReady(80 * time.Millisecond) {
		t.Fatal("未就绪应超时返回 false")
	}
	if time.Since(start) < 60*time.Millisecond {
		t.Fatal("WaitReady 应等到超时而非立即返回")
	}
}
