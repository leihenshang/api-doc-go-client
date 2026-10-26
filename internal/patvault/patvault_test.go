package patvault

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"api-doc-go-client/internal/config"
)

// isolate 把密钥文件指到临时配置目录，并清掉上一轮的内存缓存。
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(config.EnvDirOverride, dir)
	ResetCache()
	t.Cleanup(ResetCache)
	return dir
}

func TestSealOpenRoundTrip(t *testing.T) {
	isolate(t)
	blob, err := Seal("pat-abc")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if !IsEncrypted(blob) || strings.Contains(blob, "pat-abc") {
		t.Fatalf("密文格式不对或泄露明文: %q", blob)
	}
	got, err := Open(blob)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got != "pat-abc" {
		t.Fatalf("round trip = %q", got)
	}
}

func TestKeyFilePermission(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 无 POSIX 权限位")
	}
	dir := isolate(t)
	if _, err := Seal("x"); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	fi, err := os.Stat(filepath.Join(dir, keyFile))
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("密钥文件权限应为 0600，got %04o", perm)
	}
}

func TestOpenRejectsTampered(t *testing.T) {
	isolate(t)
	blob, err := Seal("pat-abc")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	// 改最后一位：GCM 校验失败
	bad := blob[:len(blob)-1]
	if blob[len(blob)-1] == 'A' {
		bad += "B"
	} else {
		bad += "A"
	}
	if _, err := Open(bad); err == nil {
		t.Fatal("篡改后的密文应解密失败")
	}
	if _, err := Open("plain-token"); err == nil {
		t.Fatal("非本包格式应拒绝")
	}
}

// 换机 / 删掉密钥后旧密文不可解；重新 Seal 会生成新密钥（不阻塞用户重填 PAT）。
func TestLostKeyInvalidatesOldCiphertext(t *testing.T) {
	dir := isolate(t)
	blob, err := Seal("pat-old")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, keyFile)); err != nil {
		t.Fatalf("remove key: %v", err)
	}
	ResetCache()
	if _, err := Open(blob); err == nil {
		t.Fatal("密钥丢失后不应还能解密旧密文")
	}
	// 新密钥可正常加密新值
	blob2, err := Seal("pat-new")
	if err != nil {
		t.Fatalf("Seal after key loss: %v", err)
	}
	got, err := Open(blob2)
	if err != nil || got != "pat-new" {
		t.Fatalf("新密文应可解密，got %q err=%v", got, err)
	}
}
