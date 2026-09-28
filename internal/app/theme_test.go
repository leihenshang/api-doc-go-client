package app

import "testing"

// 窗口底色必须与前端 --app-bg 对齐，未知主题回落浅色。
func TestWindowBackgroundFollowsTheme(t *testing.T) {
	cases := []struct {
		theme   string
		r, g, b uint8
		name    string
	}{
		{theme: "light", r: 0xf5, g: 0xf5, b: 0xf5, name: "浅色"},
		{theme: "dark", r: 0x17, g: 0x18, b: 0x1a, name: "暗色"},
		{theme: "nonsense", r: 0xf5, g: 0xf5, b: 0xf5, name: "未知主题"},
	}
	for _, c := range cases {
		r, g, b := windowBackground(c.theme)
		if r != c.r || g != c.g || b != c.b {
			t.Fatalf("%s(%q) 底色应为 #%02x%02x%02x，实际 #%02x%02x%02x", c.name, c.theme, c.r, c.g, c.b, r, g, b)
		}
	}
}

// 未设置 XDG_CONFIG_HOME 时也不应报错：读不到配置按浅色处理。
func TestInitialWindowBackgroundFallsBackToLight(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if r, g, b := InitialWindowBackground(); r != 0xf5 || g != 0xf5 || b != 0xf5 {
		t.Fatalf("无配置时应为浅色底，实际 #%02x%02x%02x", r, g, b)
	}
}
