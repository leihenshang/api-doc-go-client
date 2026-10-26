package syncengine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"api-doc-go-client/internal/config"
	"api-doc-go-client/internal/patvault"
)

// prepareVault 隔离配置目录并清掉上一轮的 patvault 密钥缓存。
func prepareVault(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(config.EnvDirOverride, dir)
	patvault.ResetCache()
	t.Cleanup(patvault.ResetCache)
	return dir
}

// PAT 落盘必须是密文：.sync.json 不含明文令牌，LoadBind 能还原。
func TestBindTokenEncryptedAtRest(t *testing.T) {
	prepareVault(t)
	dir := t.TempDir()
	if err := SaveBind(dir, BindInfo{
		Linked: true, ServerURL: "http://127.0.0.1:1", ProjectID: 1,
		Mode: ModeAuto, Token: "pat-secret",
	}); err != nil {
		t.Fatalf("SaveBind: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, ".sync.json"))
	if err != nil {
		t.Fatalf("read bind: %v", err)
	}
	text := string(raw)
	if strings.Contains(text, "pat-secret") {
		t.Fatalf(".sync.json 不得包含明文 PAT: %s", text)
	}
	if !strings.Contains(text, `"tokenEnc": "pat:v1:`) {
		t.Fatalf(".sync.json 应含 pat:v1 密文: %s", text)
	}

	b, err := LoadBind(dir)
	if err != nil {
		t.Fatalf("LoadBind: %v", err)
	}
	if b.Token != "pat-secret" || b.TokenLocked {
		t.Fatalf("LoadBind 应还原 PAT，got token=%q locked=%v", b.Token, b.TokenLocked)
	}
}

// 旧版明文 token 在首次读取时自动迁移成 tokenEnc，并删除明文。
func TestBindLegacyPlainTokenMigrates(t *testing.T) {
	prepareVault(t)
	dir := t.TempDir()
	legacy := `{"linked":true,"serverUrl":"http://127.0.0.1:1","projectId":1,"mode":"auto","cursor":7,"token":"pat-legacy"}`
	if err := os.WriteFile(filepath.Join(dir, ".sync.json"), []byte(legacy), 0o600); err != nil {
		t.Fatalf("write legacy: %v", err)
	}

	b, err := LoadBind(dir)
	if err != nil {
		t.Fatalf("LoadBind: %v", err)
	}
	if b.Token != "pat-legacy" || b.TokenLocked {
		t.Fatalf("旧明文应可继续使用，got token=%q locked=%v", b.Token, b.TokenLocked)
	}
	if b.Cursor != 7 {
		t.Fatalf("迁移不应丢字段，cursor=%d", b.Cursor)
	}

	raw, err := os.ReadFile(filepath.Join(dir, ".sync.json"))
	if err != nil {
		t.Fatalf("read migrated: %v", err)
	}
	text := string(raw)
	if strings.Contains(text, "pat-legacy") {
		t.Fatalf("迁移后不得保留明文: %s", text)
	}
	if !strings.Contains(text, `"tokenEnc": "pat:v1:`) {
		t.Fatalf("迁移后应写 tokenEnc: %s", text)
	}
}

// 密钥丢失（换机/删配置目录）后密文解不开：标记 TokenLocked，且再次保存时保留密文不误删。
func TestBindLockedTokenIsPreserved(t *testing.T) {
	prepareVault(t)
	dir := t.TempDir()
	if err := SaveBind(dir, BindInfo{
		Linked: true, ServerURL: "http://127.0.0.1:1", ProjectID: 1,
		Mode: ModeAuto, Token: "pat-secret",
	}); err != nil {
		t.Fatalf("SaveBind: %v", err)
	}

	// 换一个配置目录 = 换一把密钥（模拟换机/删掉密钥）
	t.Setenv(config.EnvDirOverride, t.TempDir())
	patvault.ResetCache()

	b, err := LoadBind(dir)
	if err != nil {
		t.Fatalf("LoadBind: %v", err)
	}
	if b.Token != "" || !b.TokenLocked {
		t.Fatalf("密钥丢失后应标记 TokenLocked，got token=%q locked=%v", b.Token, b.TokenLocked)
	}

	// 只改模式再保存：不能让「解不开」把既有密文清掉
	b.Mode = ModeManual
	if err := SaveBind(dir, b); err != nil {
		t.Fatalf("SaveBind: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".sync.json"))
	if err != nil {
		t.Fatalf("read bind: %v", err)
	}
	if !strings.Contains(string(raw), `"tokenEnc": "pat:v1:`) {
		t.Fatalf("保存不应删掉无法解密的密文: %s", raw)
	}
}

// 解绑（Linked=false）清空密文。
func TestBindUnbindClearsToken(t *testing.T) {
	prepareVault(t)
	dir := t.TempDir()
	if err := SaveBind(dir, BindInfo{
		Linked: true, ServerURL: "http://127.0.0.1:1", ProjectID: 1,
		Mode: ModeAuto, Token: "pat-secret",
	}); err != nil {
		t.Fatalf("SaveBind: %v", err)
	}
	if err := SaveBind(dir, BindInfo{}); err != nil {
		t.Fatalf("unbind SaveBind: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".sync.json"))
	if err != nil {
		t.Fatalf("read bind: %v", err)
	}
	if strings.Contains(string(raw), "tokenEnc") {
		t.Fatalf("解绑后应清空 tokenEnc: %s", raw)
	}
}
