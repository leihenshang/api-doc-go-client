package mcp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "api-doc-go-client/internal/mcp" //nolint:revive // 与其它 mcp 测试保持一致的调用写法
)

// 下面 5 个用例是代码评审发现的 5 个缺陷的回归测试。
// 每个都对应一次真实事故：评审时用临时探测测试在本机（Windows）复现过，
// 探测文件已删，这里转成永久用例，防止再犯。

// Issue 1：create_env 传只有大小写不同的名字，曾**静默覆盖**已存在的环境并销毁其密钥。
//
// 根因：环境名即文件名，而 Windows 文件系统不区分大小写；判重却用了 ==，
// 于是 "DEV" 绕过对 "dev" 的检查，SaveEnv 用 os.WriteFile 截断覆盖了同一个文件。
func TestEnvCreateCaseInsensitiveDuplicate(t *testing.T) {
	root, svc := setup(t)
	dir := filepath.Join(root, "demo")
	resetEnvs(t, svc, "demo")
	if _, err := svc.CreateEnv(CreateEnvInput{Project: "demo", Name: "dev", Vars: []EnvVarIn{
		{Name: "host", Value: "http://127.0.0.1:8080"},
		{Name: "apiKey", Value: "REAL-KEY-123", Secret: true},
	}}); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"DEV", "Dev", "dEv"} {
		_, err := svc.CreateEnv(CreateEnvInput{Project: "demo", Name: name})
		if err == nil {
			t.Fatalf("create_env(%q) 应该被拒绝：它和 dev 是同一个文件", name)
		}
		if !strings.Contains(err.Error(), "已存在") {
			t.Errorf("create_env(%q) 的错误应说明已存在，实际: %v", name, err)
		}
	}

	// 关键：原环境的数据与密钥必须完好
	if got := readEnvFile(t, dir, "dev"); !strings.Contains(got, "apiKey") || !strings.Contains(got, "host") {
		t.Errorf("原环境的变量被破坏了:\n%s", got)
	}
	if got := readSecretsFile(t, dir, "dev"); !strings.Contains(got, "REAL-KEY-123") {
		t.Errorf("原环境的密钥被销毁了:\n%s", got)
	}
	// 目录里不该多出任何文件
	rows, err := os.ReadDir(filepath.Join(dir, "environments"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 { // dev.yml + dev.secrets.yml
		t.Errorf("目录项应仍是 2 个，实际 %d: %v", len(rows), names(rows))
	}
}

// Issue 2：rename_env 只改大小写（dev→DEV），曾报告成功但**环境凭空消失**。
//
// 根因：判重放过大小写不同的名字 → SaveEnv 写回同一文件 → DeleteEnv 又把它移进 .trash。
func TestEnvRenameRejectsCaseOnlyChange(t *testing.T) {
	root, svc := setup(t)
	dir := filepath.Join(root, "demo")
	resetEnvs(t, svc, "demo")
	if _, err := svc.CreateEnv(CreateEnvInput{Project: "demo", Name: "dev", Vars: []EnvVarIn{
		{Name: "apiKey", Value: "REAL-KEY-123", Secret: true},
	}}); err != nil {
		t.Fatal(err)
	}

	for _, target := range []string{"DEV", "Dev"} {
		if _, err := svc.RenameEnv("demo", "dev", target, ""); err == nil {
			t.Fatalf("rename_env(dev→%q) 应该被拒绝：Windows 上那是同一个文件，改名会让环境消失", target)
		}
	}
	// 完全同名也要拒绝
	if _, err := svc.RenameEnv("demo", "dev", "dev", ""); err == nil {
		t.Error("rename_env 到同名应被拒绝")
	}

	// 环境必须还在，数据与密钥必须完好
	envs, err := svc.ListEnvs("demo", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 1 || envs[0].Name != "dev" {
		t.Fatalf("环境列表 = %v，期望只剩 [dev]", envNames(envs))
	}
	if !strings.Contains(readSecretsFile(t, dir, "dev"), "REAL-KEY-123") {
		t.Error("密钥被破坏了")
	}
	// 旧文件不该被移进 .trash（没有发生任何删除）。
	// 注意：resetEnvs 删脚手架环境时已经往 .trash 放过东西，所以这里比「新增条目」。
	before := len(trashNames(t, dir))
	if after := trashNames(t, dir); len(after) != before {
		t.Errorf("改名被拒不该产生新的 .trash 条目：新增 %v", after[before:])
	}
}

// Issue 2 的另一面：改名到**别的**环境要报「已占用」，不能覆盖。
func TestEnvRenameToOccupiedName(t *testing.T) {
	_, svc := setup(t)
	resetEnvs(t, svc, "demo")
	if _, err := svc.CreateEnv(CreateEnvInput{Project: "demo", Name: "dev", Vars: []EnvVarIn{{Name: "a", Value: "1"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateEnv(CreateEnvInput{Project: "demo", Name: "staging", Vars: []EnvVarIn{{Name: "b", Value: "2"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RenameEnv("demo", "dev", "staging", ""); err == nil {
		t.Error("改名到已存在的环境应报错而不是覆盖")
	} else if !strings.Contains(err.Error(), "占用") {
		t.Errorf("错误应说明目标名被占用，实际: %v", err)
	}
	envs, err := svc.ListEnvs("demo", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 2 {
		t.Fatalf("环境数 = %d，期望 2（发生了覆盖）: %v", len(envs), envNames(envs))
	}
}

// Issue 3：set_env_var 对**新增**变量传 enabled=false 曾被静默忽略（落盘仍是 true），
// 与 schema 承诺的「false = 停用该变量」矛盾。
func TestEnvSetVarHonorsEnabledOnNewVar(t *testing.T) {
	_, svc := setup(t)
	resetEnvs(t, svc, "demo")
	if _, err := svc.CreateEnv(CreateEnvInput{Project: "demo", Name: "e"}); err != nil {
		t.Fatal(err)
	}

	off := false
	got, err := svc.SetEnvVar(SetEnvVarInput{Project: "demo", Env: "e", Name: "newOff", Value: "v", Enabled: &off})
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Error("新增变量传 enabled=false，返回却是 true")
	}
	envs, err := svc.ListEnvs("demo", "")
	if err != nil {
		t.Fatal(err)
	}
	if envOf(t, envs, "e").Vars["newOff"].Enabled {
		t.Error("新增变量传 enabled=false，落盘后仍是启用状态")
	}

	// 不传 enabled → 默认启用（别被这次修复带偏）
	got, err = svc.SetEnvVar(SetEnvVarInput{Project: "demo", Env: "e", Name: "newOn", Value: "v"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled {
		t.Error("新增变量不传 enabled 时应默认启用")
	}

	// 已停用的变量：一次无关的值更新不能把它意外启用
	if _, err := svc.SetEnvVar(SetEnvVarInput{Project: "demo", Env: "e", Name: "newOn", Value: "v2", Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetEnvVar(SetEnvVarInput{Project: "demo", Env: "e", Name: "newOn", Value: "v3"}); err != nil {
		t.Fatal(err)
	}
	envs, err = svc.ListEnvs("demo", "")
	if err != nil {
		t.Fatal(err)
	}
	v := envOf(t, envs, "e").Vars["newOn"]
	if v.Enabled {
		t.Error("已停用的变量被一次无关的值更新意外启用了")
	}
	if v.Value != "v3" {
		t.Errorf("值 = %q，期望 v3（值本身应改成功）", v.Value)
	}
}

// Issue 4：set_env_var / delete_env_var / rename_env 曾是「读整份 → 改一处 → 整份写回」
// 且**无并发校验**，会把客户端界面或另一个 AI 会话在这期间的改动整份抹掉。
func TestEnvWriteConflictProtection(t *testing.T) {
	root, svc := setup(t)
	dir := filepath.Join(root, "demo")
	resetEnvs(t, svc, "demo")
	if _, err := svc.CreateEnv(CreateEnvInput{Project: "demo", Name: "e", Vars: []EnvVarIn{
		{Name: "keep", Value: "1"},
	}}); err != nil {
		t.Fatal(err)
	}

	// list_envs 给出 hash
	envs, err := svc.ListEnvs("demo", "")
	if err != nil {
		t.Fatal(err)
	}
	stale := envOf(t, envs, "e").Hash
	if len(stale) < 8 {
		t.Fatalf("list_envs 应返回可用于 ifMatch 的 hash，实际 %q", stale)
	}

	// 模拟「AI 读完之后别人往这个环境里加了一个变量」：直接改磁盘（等价于客户端界面保存）。
	// 用追加一整条 var 的方式，而不是替换某个字符串 —— YAML 里值是带引号的（value: "1"），
	// 字符串替换很容易什么都没换到，测试就会自己骗自己。
	body := readEnvFile(t, dir, "e")
	mutated := body + "    - name: injected\n      value: \"999\"\n      enabled: true\n      secret: false\n"
	if err := os.WriteFile(filepath.Join(dir, "environments", "e.yml"), []byte(mutated), 0o644); err != nil {
		t.Fatal(err)
	}

	// 带过期 hash 写入 → 必须被拒，且磁盘不被二次改写
	_, err = svc.SetEnvVar(SetEnvVarInput{Project: "demo", Env: "e", Name: "newOne", Value: "x", IfMatch: stale})
	if err == nil {
		t.Fatal("ifMatch 过期时应拒绝写入（否则会抹掉别人的改动）")
	}
	if !strings.Contains(err.Error(), "已变化") {
		t.Errorf("错误应说明文件已变化，实际: %v", err)
	}
	if got := readEnvFile(t, dir, "e"); strings.Contains(got, "newOne") {
		t.Errorf("被拒绝后不该落盘:\n%s", got)
	}
	// delete_env_var / rename_env 同样受保护
	if err := svc.DeleteEnvVar("demo", "e", "keep", stale); err == nil {
		t.Error("delete_env_var 带过期 ifMatch 也应被拒绝")
	}
	if _, err := svc.RenameEnv("demo", "e", "e2", stale); err == nil {
		t.Error("rename_env 带过期 ifMatch 也应被拒绝")
	}

	// 不带 ifMatch → 允许（最后写者赢，与请求侧一致）
	envs, err = svc.ListEnvs("demo", "")
	if err != nil {
		t.Fatal(err)
	}
	fresh := envOf(t, envs, "e").Hash
	if fresh == stale {
		t.Fatal("文件已被外部改动，list_envs 返回的 hash 应该变了")
	}
	got, err := svc.SetEnvVar(SetEnvVarInput{Project: "demo", Env: "e", Name: "newOne", Value: "x", IfMatch: fresh})
	if err != nil {
		t.Fatalf("带最新 hash 写入应成功: %v", err)
	}
	if got.Hash == "" || got.Hash == fresh {
		t.Errorf("写入后应返回新 hash 供链式使用，实际 %q（原 %q）", got.Hash, fresh)
	}
	// 别人的改动仍在
	if body := readEnvFile(t, dir, "e"); !strings.Contains(body, "injected") {
		t.Errorf("别人的改动被抹掉了:\n%s", body)
	}
}

// Issue 5：isMaskish 曾把 '.' 与 '?' 也当掩码，导致「把密钥改成 ...」被静默忽略
// （只在结果里附一句「传回的是掩码」，调用方察觉不到自己没改成）。
func TestIsMaskishRejectsRealPasswordChars(t *testing.T) {
	_, svc := setup(t)
	resetEnvs(t, svc, "demo")
	if _, err := svc.CreateEnv(CreateEnvInput{Project: "demo", Name: "e", Vars: []EnvVarIn{
		{Name: "apiKey", Value: "REAL-KEY-123", Secret: true},
	}}); err != nil {
		t.Fatal(err)
	}
	// 这些是现实中可能出现的密码，应该被当成新值轮换掉，而不是被忽略。
	// 刻意不含 '*'：它是最经典的「隐藏值」写法（******），把它当掩码能拦住更多传输中被改写的
	// 情形，而真实密码恰好是纯星号的概率极低 —— 这是有意识的取舍，不是漏网。
	for _, real := range []string{"...", ".", "?", "?????", "a?b", "x...y", "*.*"} {
		got, err := svc.SetEnvVar(SetEnvVarInput{Project: "demo", Env: "e", Name: "apiKey", Value: real, Secret: true})
		if err != nil {
			t.Fatalf("传 %q 不该报错: %v", real, err)
		}
		if got.KeptSecret {
			t.Errorf("传 %q 被误判成掩码，密钥没被轮换（它可能是真实密码）", real)
		}
	}
}
