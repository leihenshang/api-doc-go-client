package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func TestLoadWithoutFileReturnsDefault(t *testing.T) {
	isolateConfigDir(t)
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Settings 含 AllowOrigins slice，不能用 == 比较（结构体含不可比较字段）
	if !reflect.DeepEqual(got, Default()) {
		t.Fatalf("缺文件时应返回默认值，实际 %+v", got)
	}
}

func TestSaveThenLoadRoundTrip(t *testing.T) {
	isolateConfigDir(t)
	s := Default()
	s.InsecureSSL = true
	s.TimeoutSec = 5
	s.FollowRedirects = false
	s.MaxRedirects = 2
	s.HistoryLimit = 7
	if err := Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, s) {
		t.Fatalf("round-trip 不一致:\n want %+v\n got  %+v", s, got)
	}
}

func TestNormalizeFillsNumericZeroValues(t *testing.T) {
	got := Settings{}.Normalize()
	if got.TimeoutSec != 30 || got.MaxRedirects != 5 || got.HistoryLimit != 200 {
		t.Fatalf("Normalize 未补齐零值: %+v", got)
	}
	// 布尔项必须保持调用方原意：显式关闭「跟随重定向」不能被改回 true
	if got.FollowRedirects {
		t.Fatalf("Normalize 不应改写布尔开关")
	}
}

// 主题：默认浅色、暗色原样保留、未知值回落浅色；旧配置文件缺 theme 时也应是浅色。
func TestThemeNormalize(t *testing.T) {
	if Default().Theme != ThemeLight {
		t.Fatalf("默认主题应为浅色，实际 %q", Default().Theme)
	}
	if got := (Settings{Theme: ThemeDark}).Normalize().Theme; got != ThemeDark {
		t.Fatalf("Normalize 不应改写合法暗色，实际 %q", got)
	}
	for _, bad := range []string{"", "Dark", "system", " light"} {
		if got := (Settings{Theme: bad}).Normalize().Theme; got != ThemeLight {
			t.Fatalf("非法主题 %q 应回落浅色，实际 %q", bad, got)
		}
	}
	if !(Settings{Theme: ThemeDark}).IsDark() || (Settings{Theme: ThemeLight}).IsDark() {
		t.Fatalf("IsDark 判定错误")
	}
}

// 缺字段的文件（例如旧版本写的配置）应按默认值补齐，而不是被 false/0 覆盖。
func TestLoadMergesMissingFieldsWithDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := writeFile(path, `{"insecureSsl": true}`); err != nil {
		t.Fatalf("写入: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got.InsecureSSL || !got.FollowRedirects || got.TimeoutSec != 30 {
		t.Fatalf("缺失字段未回落默认: %+v", got)
	}
}

// isolateConfigDir 把「用户配置目录」指向临时目录，避免读到/写到真实配置。
// 覆盖变量在两端都生效：设了它就一定落在指定目录（e2e / devserver 隔离靠它）。
func TestDirOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDirOverride, dir)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())

	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if got != filepath.Clean(dir) {
		t.Fatalf("Dir() = %q，期望覆盖值 %q", got, filepath.Clean(dir))
	}
	// Save/Load 一并验证：文件确实写在覆盖目录里
	if err := Save(Default()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, fileName)); err != nil {
		t.Fatalf("设置文件未落在覆盖目录: %v", err)
	}
}

// Windows 上 os.UserConfigDir() 只看 %AppData%（忽略 XDG_CONFIG_HOME），
// Linux/macOS 反而看 XDG_CONFIG_HOME —— 两个都设，保证跨平台隔离一致。
func isolateConfigDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("AppData", dir)
}
