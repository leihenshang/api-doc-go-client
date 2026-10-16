package syncengine

import (
	"os"
	"testing"

	"api-doc-go-client/internal/config"
	"api-doc-go-client/internal/keychain"
)

// fakeKeyring 注入内存密钥环；unavailable=true 模拟无密钥环环境（回退分支）。
// 恢复统一走 t.Cleanup(keychain.ResetHooks)，绝不触碰真实密钥环。
func fakeKeyring(t *testing.T, unavailable bool) map[string]string {
	t.Helper()
	t.Cleanup(keychain.ResetHooks)
	store := map[string]string{}
	if unavailable {
		keychain.SetHooks(
			func(string, string) error { return keychain.ErrNotAvailable },
			func(string) (string, error) { return "", keychain.ErrNotAvailable },
			func(string) error { return keychain.ErrNotAvailable },
		)
		return store
	}
	keychain.SetHooks(
		func(u, v string) error { store[u] = v; return nil },
		func(u string) (string, error) {
			if v, ok := store[u]; ok {
				return v, nil
			}
			return "", keychain.ErrNotFound
		},
		func(u string) error { delete(store, u); return nil },
	)
	return store
}

func isolateConfigDir(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvDirOverride, t.TempDir())
}

// G6：密钥环可用时，口令只进密钥环、不留文件；清除时两处都删。
func TestPassphrasePrefersKeychain(t *testing.T) {
	isolateConfigDir(t)
	store := fakeKeyring(t, false)

	if err := SetSyncPassphrase("p1"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if store["sync_passphrase"] != "p1" {
		t.Fatalf("口令应入密钥环，store=%v", store)
	}
	if _, err := os.Stat(syncPassphrasePath()); !os.IsNotExist(err) {
		t.Error("密钥环成功后不应保留口令文件")
	}
	if got := SyncPassphrase(); got != "p1" {
		t.Fatalf("应直接从密钥环读回，got %q", got)
	}

	if err := SetSyncPassphrase(""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, ok := store["sync_passphrase"]; ok {
		t.Error("清除后密钥环不应残留")
	}
	if got := SyncPassphrase(); got != "" {
		t.Fatalf("清除后应读空，got %q", got)
	}
}

// G6：密钥环不可用时保持历史行为——配置目录 0600 文件。
func TestPassphraseFallsBackToFile(t *testing.T) {
	isolateConfigDir(t)
	fakeKeyring(t, true)

	if err := SetSyncPassphrase("p2"); err != nil {
		t.Fatalf("set: %v", err)
	}
	data, err := os.ReadFile(syncPassphrasePath())
	if err != nil || string(data) != "p2" {
		t.Fatalf("应回退写 0600 文件：err=%v content=%q", err, data)
	}
	if got := SyncPassphrase(); got != "p2" {
		t.Fatalf("应从文件读回，got %q", got)
	}
}

// G6 迁移：旧版本留下的口令文件，在密钥环可用时首次读取即迁入并删除文件。
func TestPassphraseMigratesFromFile(t *testing.T) {
	isolateConfigDir(t)
	fakeKeyring(t, true)
	if err := SetSyncPassphrase("p3"); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	store := fakeKeyring(t, false) // 换成可用密钥环（空）
	if got := SyncPassphrase(); got != "p3" {
		t.Fatalf("迁移读回应为 p3，got %q", got)
	}
	if store["sync_passphrase"] != "p3" {
		t.Fatalf("应已迁入密钥环，store=%v", store)
	}
	if _, err := os.Stat(syncPassphrasePath()); !os.IsNotExist(err) {
		t.Error("迁移成功后应删除口令文件")
	}
}
