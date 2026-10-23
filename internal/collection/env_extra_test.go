package collection

// M-1：环境主文件与 *.secrets.yml 的未知顶层字段，在整份重写/改名时必须保留。
// 共享库已支持 Extra round-trip；这里锁定客户端读写路径真的把它带回去。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const envExtraMain = `info:
  name: dev
uid: env-dev
vars:
  - { name: host, value: "http://x", enabled: true }
  - { name: token, value: "", enabled: true, secret: true }
future:
  keep: yes
`

const envExtraSecrets = `secrets:
  token: tok
vault:
  hint: h
`

// seedEnvWithExtras 直接落一份带未知字段的环境文件（绕过 SaveEnv 的读写逻辑）。
func seedEnvWithExtras(t *testing.T, c *Collection) (mainP, secP string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(c.Dir, "environments"), 0o755); err != nil {
		t.Fatalf("mkdir environments: %v", err)
	}
	mainP, secP = c.envPaths("dev")
	if err := os.WriteFile(mainP, []byte(envExtraMain), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	if err := os.WriteFile(secP, []byte(envExtraSecrets), 0o600); err != nil {
		t.Fatalf("write secrets: %v", err)
	}
	return mainP, secP
}

func assertContains(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("%s 丢失 %q:\n%s", path, want, data)
	}
}

// TestSaveEnvPreservesUnknownFields 覆盖两种调用形态：readEnv 读出的 Env、
// 以及 UI 重建的 Env（不带 Extra，必须从磁盘补齐）。
func TestSaveEnvPreservesUnknownFields(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	mainP, secP := seedEnvWithExtras(t, c)

	env, err := c.readEnv("dev")
	if err != nil {
		t.Fatalf("readEnv: %v", err)
	}
	env.Vars[0].Value = "http://y"
	if err := c.SaveEnv(*env); err != nil {
		t.Fatalf("SaveEnv(readEnv): %v", err)
	}
	assertContains(t, mainP, "future:")
	assertContains(t, secP, "vault:")

	// UI 保存时 Env 是 JSON 反序列化出来的，不带 unexported 的 Extra。
	uiEnv := Env{UID: "env-dev", Name: "dev", Vars: []Var{
		{Name: "host", Value: "http://z", Enabled: true},
		{Name: "token", Value: "tok2", Enabled: true, Secret: true},
	}}
	if err := c.SaveEnv(uiEnv); err != nil {
		t.Fatalf("SaveEnv(ui): %v", err)
	}
	assertContains(t, mainP, "future:")
	assertContains(t, secP, "vault:")

	back, err := c.readEnv("dev")
	if err != nil {
		t.Fatalf("readEnv(round-trip): %v", err)
	}
	if back.Vars[0].Value != "http://z" || back.extra["future"] == nil || back.secretsExtra["vault"] == nil {
		t.Fatalf("round-trip 不一致: %+v (main=%+v sec=%+v)", back, back.extra, back.secretsExtra)
	}
}

// TestRenameEnvPreservesUnknownFields 改名时未知字段要随两个文件一起平移（M-1）。
func TestRenameEnvPreservesUnknownFields(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	seedEnvWithExtras(t, c)

	if err := c.RenameEnv("dev", "prod", ""); err != nil {
		t.Fatalf("RenameEnv: %v", err)
	}
	mainP, secP := c.envPaths("prod")
	assertContains(t, mainP, "future:")
	assertContains(t, secP, "vault:")
}
