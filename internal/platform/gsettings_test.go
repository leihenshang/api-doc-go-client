package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureGSettingsBackendUsesMemoryWithoutBus(t *testing.T) {
	t.Setenv("GSETTINGS_BACKEND", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	EnsureGSettingsBackend()

	if got := os.Getenv("GSETTINGS_BACKEND"); got != "memory" {
		t.Fatalf("无会话总线时应切到内存 backend，实际 %q", got)
	}
}

func TestEnsureGSettingsBackendKeepsUserChoice(t *testing.T) {
	t.Setenv("GSETTINGS_BACKEND", "dconf")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	EnsureGSettingsBackend()

	if got := os.Getenv("GSETTINGS_BACKEND"); got != "dconf" {
		t.Fatalf("不应覆盖用户显式设置，实际 %q", got)
	}
}

func TestEnsureGSettingsBackendSkipsWhenBusAddressPresent(t *testing.T) {
	t.Setenv("GSETTINGS_BACKEND", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/run/user/1000/bus")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	EnsureGSettingsBackend()

	if got := os.Getenv("GSETTINGS_BACKEND"); got != "" {
		t.Fatalf("有总线时不应改动设置，实际 %q", got)
	}
}

func TestEnsureGSettingsBackendSkipsWhenRuntimeBusExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bus"), []byte("socket"), 0o600); err != nil {
		t.Fatalf("准备 bus 文件: %v", err)
	}
	t.Setenv("GSETTINGS_BACKEND", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	t.Setenv("XDG_RUNTIME_DIR", dir)

	EnsureGSettingsBackend()

	if got := os.Getenv("GSETTINGS_BACKEND"); got != "" {
		t.Fatalf("检测到 $XDG_RUNTIME_DIR/bus 时不应改动设置，实际 %q", got)
	}
}
