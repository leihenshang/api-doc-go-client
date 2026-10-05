package mcp_test

import (
	"path/filepath"
	"strings"
	"testing"

	. "api-doc-go-client/internal/mcp"
)

// 新建敏感变量时传掩码必须报错：新变量没有「旧密钥」可保留，
// 存下去的就是字面掩码 —— 之后 AI 再也拿不到真值，且看起来「已配置」。
func TestSetEnvVarRejectsMaskForNewSecret(t *testing.T) {
	root, svc := setup(t)
	projectDir := filepath.Join(root, "demo")
	resetEnvs(t, svc, "demo")
	if _, err := svc.CreateEnv(CreateEnvInput{Project: "demo", Name: "dev"}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.SetEnvVar(SetEnvVarInput{Project: "demo", Env: "dev", Name: "token", Value: mask, Secret: true})
	if err == nil {
		t.Fatal("新敏感变量传掩码应被拒绝")
	}
	if !strings.Contains(err.Error(), "真实密钥") {
		t.Fatalf("错误信息应说明需要真实密钥: %v", err)
	}
	if sec := readSecretsFile(t, projectDir, "dev"); strings.Contains(sec, mask) {
		t.Fatalf("掩码被当值写进了 secrets 文件:\n%s", sec)
	}

	// 真实值应当正常落盘；之后再把掩码传回表示「不动」
	if _, err := svc.SetEnvVar(SetEnvVarInput{Project: "demo", Env: "dev", Name: "token", Value: "real-value", Secret: true}); err != nil {
		t.Fatalf("真实密钥应可写入: %v", err)
	}
	if sec := readSecretsFile(t, projectDir, "dev"); !strings.Contains(sec, "real-value") {
		t.Fatalf("真实密钥未落盘:\n%s", sec)
	}
	got, err := svc.SetEnvVar(SetEnvVarInput{Project: "demo", Env: "dev", Name: "token", Value: mask, Secret: true})
	if err != nil {
		t.Fatalf("已存在的敏感变量回填掩码应视为保持原值: %v", err)
	}
	if !got.KeptSecret {
		t.Fatalf("应标记 KeptSecret: %+v", got)
	}
}
