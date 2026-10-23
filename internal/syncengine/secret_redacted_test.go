package syncengine

// H-11：服务端对无明文权限的调用方只回 redacted 标志（不下发明文 secret）。
// 客户端必须保留本地已有值（不能把空值/掩码写进 secrets 文件）；
// 本地没有该变量时也不能凭空创建空 secret，否则下次有写权限时会把空值推回服务端。

import (
	"testing"

	"api-doc-go-client/internal/collection"
)

// TestApplyRedactedSecretKeepsLocalValue 校验 redacted 载荷不覆盖本地值、不写脏基线。
func TestApplyRedactedSecretKeepsLocalValue(t *testing.T) {
	c, err := collection.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.SaveEnv(collection.Env{UID: "env-h11", Name: "dev", Vars: []collection.Var{
		{UID: "v-h11", Name: "token", Value: "local-secret", Enabled: true, Secret: true},
	}}); err != nil {
		t.Fatalf("SaveEnv: %v", err)
	}

	e := &Engine{Coll: c, Bind: BindInfo{}}
	enabled := true
	if err := e.applyVariable("v-h11", Payload{
		Name: "token", EnvUID: "env-h11", Secret: true, Enabled: &enabled, Redacted: true,
	}); err != nil {
		t.Fatalf("applyVariable: %v", err)
	}

	envs, err := c.ListEnvs()
	if err != nil {
		t.Fatalf("ListEnvs: %v", err)
	}
	if len(envs) != 1 || len(envs[0].Vars) != 1 || envs[0].Vars[0].Value != "local-secret" {
		t.Fatalf("本地 secret 值不应被 redacted 载荷覆盖，got %+v", envs)
	}
	// 基线不能写：否则本地未推送的改动会被当成「已同步」。
	if _, ok := e.Bind.VarHashes["v-h11"]; ok {
		t.Fatalf("redacted 载荷不应写入推送基线，got %+v", e.Bind.VarHashes)
	}

	// 本地没有该变量：跳过，不凭空创建空 secret。
	if err := e.applyVariable("v-new", Payload{
		Name: "newToken", EnvUID: "env-h11", Secret: true, Enabled: &enabled, Redacted: true,
	}); err != nil {
		t.Fatalf("applyVariable(new): %v", err)
	}
	envs, err = c.ListEnvs()
	if err != nil {
		t.Fatalf("ListEnvs: %v", err)
	}
	for _, v := range envs[0].Vars {
		if v.UID == "v-new" || v.Name == "newToken" {
			t.Fatalf("redacted 且本地不存在的变量不应凭空创建，got %+v", envs[0].Vars)
		}
	}
}
