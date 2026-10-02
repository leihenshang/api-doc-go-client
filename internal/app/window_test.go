package app

import "testing"

// TestWindowFitsScreen 初始窗口尺寸判定（逻辑像素）。
// 首选 1600×1000 放不下时必须走「最大化」：旧实现直接把 1600×1000 交给 Wails，
// 在逻辑屏只有 1536×960 的机器（1920×1200 @125%）上会被系统居中到屏幕外，顶部被推出屏幕。
func TestWindowFitsScreen(t *testing.T) {
	cases := []struct {
		name     string
		screenW  int
		screenH  int
		wantFits bool
	}{
		{"本机逻辑屏 1536×960（1920×1200 @125%）", 1536, 960, false},
		{"1080p @150% 缩放", 1280, 720, false},
		{"1024×768", 1024, 768, false},
		{"1080p @100%", 1920, 1080, true},
		{"1440p", 2560, 1440, true},
		{"4K @200% 缩放（逻辑 1920×1080）", 1920, 1080, true},
		{"刚好够（宽度含留白、高度含任务栏预留）", preferredWindowWidth + windowFitMargin, preferredWindowHeight + windowVerticalReserve, true},
		{"宽度差 1px", preferredWindowWidth + windowFitMargin - 1, preferredWindowHeight + windowVerticalReserve, false},
		{"高度差 1px", preferredWindowWidth + windowFitMargin, preferredWindowHeight + windowVerticalReserve - 1, false},
	}
	for _, c := range cases {
		if got := windowFitsScreen(c.screenW, c.screenH); got != c.wantFits {
			t.Errorf("%s：windowFitsScreen(%d, %d) = %v，期望 %v", c.name, c.screenW, c.screenH, got, c.wantFits)
		}
	}
}
