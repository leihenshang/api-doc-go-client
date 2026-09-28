package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func TestLoadWithoutFileReturnsDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != Default() {
		t.Fatalf("缺文件时应返回默认值，实际 %+v", got)
	}
}

func TestSaveThenLoadRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
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
	if got != s {
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
