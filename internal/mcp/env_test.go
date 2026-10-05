package mcp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "api-doc-go-client/internal/mcp" //nolint:revive // 与其它 mcp 测试保持一致的调用写法
)

// mask 敏感值对外的掩码。与共享包 varx.MaskedValue 一致，这里独立写死一个，
// 顺便验证「掩码文案没有被改掉」（变了就该同步更新文档）。
const mask = "••••••"

// resetEnvs 清空项目里的环境，让测试从确定状态开始。
//
// 必须清：collection 首次打开集合时会自动建一个 dev 环境（含 host 变量，见
// collection.Open 的脚手架），不清理的话「首次创建 dev」必然撞名，测试之间也会互相干扰。
func resetEnvs(t *testing.T, svc *Service, project string) {
	t.Helper()
	envs, err := svc.ListEnvs(project, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range envs {
		if err := svc.DeleteEnv(project, e.Name); err != nil {
			t.Fatalf("清理环境 %s 失败: %v", e.Name, err)
		}
	}
	if left, err := svc.ListEnvs(project, ""); err != nil || len(left) != 0 {
		t.Fatalf("清理后仍有环境: %v (err=%v)", envNames(left), err)
	}
}

// trashNames 读集合 .trash 里的文件名（删除类操作要断言「进了回收站」）。
func trashNames(t *testing.T, projectDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(projectDir, ".trash"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("读 .trash 失败: %v", err)
	}
	return names(entries)
}

// readEnvFile 读集合里的某个环境文件（测试直接看落盘结果，不经 Service）。
func readEnvFile(t *testing.T, projectDir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(projectDir, "environments", name+".yml"))
	if err != nil {
		t.Fatalf("读取环境 %s 失败: %v", name, err)
	}
	return string(data)
}

func readSecretsFile(t *testing.T, projectDir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(projectDir, "environments", name+".secrets.yml"))
	if err != nil {
		t.Fatalf("读取环境 %s 的 secrets 失败: %v", name, err)
	}
	return string(data)
}

// envOf 从 ListEnvs 结果里取一个环境。
func envOf(t *testing.T, envs []EnvEntry, name string) EnvEntry {
	t.Helper()
	for _, e := range envs {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("环境列表里没有 %s（实际: %v）", name, envNames(envs))
	return EnvEntry{}
}

func envNames(envs []EnvEntry) []string {
	out := make([]string, 0, len(envs))
	for _, e := range envs {
		out = append(out, e.Name)
	}
	return out
}

func TestEnvCreateAndList(t *testing.T) {
	root, svc := setup(t)
	resetEnvs(t, svc, "demo")
	dir := filepath.Join(root, "demo")

	env, err := svc.CreateEnv(CreateEnvInput{
		Project: "demo",
		Name:    "dev",
		Vars: []EnvVarIn{
			{Name: "host", Value: "http://127.0.0.1:8080"},
			{Name: "token", Value: "super-secret", Secret: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if env.Name != "dev" || len(env.Vars) != 2 {
		t.Fatalf("返回的环境不对: %+v", env)
	}
	// 敏感变量的值对外必须是掩码
	if got := env.Vars["token"].Value; got != mask {
		t.Errorf("token 的对外值 = %q，期望掩码 %q", got, mask)
	}
	if got := env.Vars["host"].Value; got != "http://127.0.0.1:8080" {
		t.Errorf("host 的值被改动了: %q", got)
	}

	// 落盘：真密钥只进 .secrets.yml，主文件里不能有明文
	main := readEnvFile(t, dir, "dev")
	if strings.Contains(main, "super-secret") {
		t.Errorf("主环境文件里出现了明文密钥:\n%s", main)
	}
	if !strings.Contains(readSecretsFile(t, dir, "dev"), "super-secret") {
		t.Errorf("secrets 文件里没有密钥值:\n%s", readSecretsFile(t, dir, "dev"))
	}

	// list_envs 能读回来，且默认环境是排序后的第一个
	if _, err := svc.CreateEnv(CreateEnvInput{Project: "demo", Name: "alpha"}); err != nil {
		t.Fatal(err)
	}
	envs, err := svc.ListEnvs("demo", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := envNames(envs); len(got) != 2 || got[0] != "alpha" || got[1] != "dev" {
		t.Fatalf("环境列表 = %v，期望 [alpha dev]（按名字排序）", got)
	}
	if !envOf(t, envs, "alpha").Default {
		t.Error("排序第一个的环境应是默认环境")
	}
	if envOf(t, envs, "dev").Default {
		t.Error("dev 不该是默认环境")
	}
	if v := envOf(t, envs, "dev").Vars["token"]; v.Value != mask || !v.Secret {
		t.Errorf("list_envs 里的敏感变量 = %+v，期望掩码 + secret=true", v)
	}
}

// TestEnvCreateDefaultsEnabled：建环境时省略 enabled 的变量必须是启用的。
// 曾经用 bool 接这个字段，零值 false 让「AI 忘了传」=「建出来就停用」——
// 变量在列表里看得见，{{name}} 却解析不到，是极难排查的坑。
func TestEnvCreateDefaultsEnabled(t *testing.T) {
	_, svc := setup(t)
	resetEnvs(t, svc, "demo")
	if _, err := svc.CreateEnv(CreateEnvInput{Project: "demo", Name: "e", Vars: []EnvVarIn{
		{Name: "a", Value: "1"},                          // 不传 enabled
		{Name: "b", Value: "2", Enabled: boolPtr(false)}, // 显式停用
		{Name: "c", Value: "3", Enabled: boolPtr(true)},  // 显式启用
	}}); err != nil {
		t.Fatal(err)
	}
	envs, err := svc.ListEnvs("demo", "")
	if err != nil {
		t.Fatal(err)
	}
	vars := envOf(t, envs, "e").Vars
	if !vars["a"].Enabled {
		t.Error("省略 enabled 的变量应是启用状态")
	}
	if vars["b"].Enabled {
		t.Error("显式 enabled=false 的变量应是停用状态")
	}
	if !vars["c"].Enabled {
		t.Error("显式 enabled=true 的变量应是启用状态")
	}
}

func TestEnvListQueryFiltersByVarName(t *testing.T) {
	_, svc := setup(t)
	resetEnvs(t, svc, "demo")
	if _, err := svc.CreateEnv(CreateEnvInput{Project: "demo", Name: "dev", Vars: []EnvVarIn{
		{Name: "host", Value: "h"},
		{Name: "baseUrl", Value: "b"},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateEnv(CreateEnvInput{Project: "demo", Name: "prod"}); err != nil {
		t.Fatal(err)
	}
	// 按变量名命中：即使环境名不含关键词也要返回
	envs, err := svc.ListEnvs("demo", "baseurl")
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 1 || envs[0].Name != "dev" {
		t.Fatalf("按变量名过滤 = %v，期望只返回 dev", envNames(envs))
	}
	// 按环境名命中
	envs, err = svc.ListEnvs("demo", "pro")
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 1 || envs[0].Name != "prod" {
		t.Fatalf("按环境名过滤 = %v，期望只返回 prod", envNames(envs))
	}
	// 不命中 → 空
	envs, err = svc.ListEnvs("demo", "不存在的关键词")
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 0 {
		t.Fatalf("不命中时应返回空，得到 %v", envNames(envs))
	}
}

func TestEnvSetVarUpsertAndDisable(t *testing.T) {
	_, svc := setup(t)
	resetEnvs(t, svc, "demo")
	if _, err := svc.CreateEnv(CreateEnvInput{Project: "demo", Name: "dev", Vars: []EnvVarIn{
		{Name: "keep", Value: "原值"},
	}}); err != nil {
		t.Fatal(err)
	}
	// 新增
	got, err := svc.SetEnvVar(SetEnvVarInput{Project: "demo", Env: "dev", Name: "host", Value: "h1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != "h1" || !got.Enabled {
		t.Errorf("新增变量返回 = %+v", got)
	}
	// 更新（不传 enabled → 保持原状）
	got, err = svc.SetEnvVar(SetEnvVarInput{Project: "demo", Env: "dev", Name: "host", Value: "h2"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != "h2" {
		t.Errorf("更新后 = %q，期望 h2", got.Value)
	}
	// 停用
	off := false
	got, err = svc.SetEnvVar(SetEnvVarInput{Project: "demo", Env: "dev", Name: "host", Value: "h2", Enabled: &off})
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Error("enabled=false 后仍返回 enabled=true")
	}
	// 同一环境里其它变量没被动过
	envs, err := svc.ListEnvs("demo", "")
	if err != nil {
		t.Fatal(err)
	}
	e := envOf(t, envs, "dev")
	if len(e.Vars) != 2 || e.Vars["keep"].Value != "原值" {
		t.Errorf("其它变量被影响了: %+v", e.Vars)
	}
}

// TestEnvSetVarSecretMaskRoundTrip 是最关键的一条：AI 只能看到掩码，
// 它把 list 的输出原样回写时，绝不能把 "••••••" 存成真密钥。
func TestEnvSetVarSecretMaskRoundTrip(t *testing.T) {
	root, svc := setup(t)
	resetEnvs(t, svc, "demo")
	dir := filepath.Join(root, "demo")
	if _, err := svc.CreateEnv(CreateEnvInput{Project: "demo", Name: "dev", Vars: []EnvVarIn{
		{Name: "apiKey", Value: "real-key-123", Secret: true},
	}}); err != nil {
		t.Fatal(err)
	}

	// 场景 1：把掩码原样传回 → 密钥保持不变
	got, err := svc.SetEnvVar(SetEnvVarInput{Project: "demo", Env: "dev", Name: "apiKey", Value: mask, Secret: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != mask {
		t.Errorf("返回值 = %q，期望仍是掩码", got.Value)
	}
	if !strings.Contains(readSecretsFile(t, dir, "dev"), "real-key-123") {
		t.Errorf("回填掩码把真密钥冲掉了，secrets 文件:\n%s", readSecretsFile(t, dir, "dev"))
	}

	// 场景 2：显式给新值 → 真的换掉
	if _, err := svc.SetEnvVar(SetEnvVarInput{Project: "demo", Env: "dev", Name: "apiKey", Value: "rotated-456", Secret: true}); err != nil {
		t.Fatal(err)
	}
	sec := readSecretsFile(t, dir, "dev")
	if !strings.Contains(sec, "rotated-456") || strings.Contains(sec, "real-key-123") {
		t.Errorf("轮换密钥没生效，secrets 文件:\n%s", sec)
	}

	// 场景 3：被改写过的掩码（传输途中变了样）也要认出来，不能当成新密钥。
	// 字符集只有装饰字符：• ● · *（评审结论：'.' 与 '?' 是可能的真实密码字符，不算掩码）。
	for _, mangled := range []string{"••••••", "••••", "●●●●●●", "····", "******", " •••••• "} {
		got, err := svc.SetEnvVar(SetEnvVarInput{Project: "demo", Env: "dev", Name: "apiKey", Value: mangled, Secret: true})
		if err != nil {
			t.Fatalf("传 %q 应被当作掩码：%v", mangled, err)
		}
		if !got.KeptSecret {
			t.Errorf("传 %q 时 KeptSecret=false，期望识别为掩码并保持原密钥", mangled)
		}
	}
	if !strings.Contains(readSecretsFile(t, dir, "dev"), "rotated-456") {
		t.Errorf("被改写的掩码把真密钥冲掉了:\n%s", readSecretsFile(t, dir, "dev"))
	}

	// 场景 4：对已存在的敏感变量传空值要报错（不能静默清空密钥）
	if _, err := svc.SetEnvVar(SetEnvVarInput{Project: "demo", Env: "dev", Name: "apiKey", Value: "", Secret: true}); err == nil {
		t.Error("对敏感变量传空值应报错（静默清空密钥是最坏的帮倒忙）")
	}
	if !strings.Contains(readSecretsFile(t, dir, "dev"), "rotated-456") {
		t.Error("报错后密钥不该被动过")
	}
}

func TestEnvRenameCarriesSecrets(t *testing.T) {
	root, svc := setup(t)
	resetEnvs(t, svc, "demo")
	dir := filepath.Join(root, "demo")
	if _, err := svc.CreateEnv(CreateEnvInput{Project: "demo", Name: "dev", Vars: []EnvVarIn{
		{Name: "host", Value: "h"},
		{Name: "apiKey", Value: "real-key-123", Secret: true},
	}}); err != nil {
		t.Fatal(err)
	}
	env, err := svc.RenameEnv("demo", "dev", "staging", "")
	if err != nil {
		t.Fatal(err)
	}
	if env.Name != "staging" || len(env.Vars) != 2 {
		t.Fatalf("改名结果不对: %+v", env)
	}
	if env.Vars["apiKey"].Value != mask {
		t.Errorf("改名后敏感值 = %q，期望仍是掩码", env.Vars["apiKey"].Value)
	}
	// 敏感值真的搬过去了
	if !strings.Contains(readSecretsFile(t, dir, "staging"), "real-key-123") {
		t.Errorf("新环境的 secrets 文件里没有密钥:\n%s", readSecretsFile(t, dir, "staging"))
	}
	// 旧文件进了 .trash（可人工找回）
	var moved []string
	for _, name := range trashNames(t, dir) {
		if strings.Contains(name, "dev") {
			moved = append(moved, name)
		}
	}
	if len(moved) == 0 {
		t.Errorf(".trash 里没找到旧环境文件，现有: %v", trashNames(t, dir))
	}
	// 旧环境不该再出现在列表里
	envs, err := svc.ListEnvs("demo", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := envNames(envs); len(got) != 1 || got[0] != "staging" {
		t.Errorf("改名后环境列表 = %v，期望只剩 staging", got)
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestEnvDeleteGoesToTrash(t *testing.T) {
	root, svc := setup(t)
	resetEnvs(t, svc, "demo")
	dir := filepath.Join(root, "demo")
	if _, err := svc.CreateEnv(CreateEnvInput{Project: "demo", Name: "dev"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteEnv("demo", "dev"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "environments", "dev.yml")); !os.IsNotExist(err) {
		t.Error("环境主文件还在（应已移入 .trash）")
	}
	if got := trashNames(t, dir); len(got) == 0 {
		t.Error(".trash 里应有被删的环境文件")
	}
	envs, err := svc.ListEnvs("demo", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 0 {
		t.Errorf("删除后列表 = %v，期望空", envNames(envs))
	}
}

func TestEnvDeleteVar(t *testing.T) {
	root, svc := setup(t)
	resetEnvs(t, svc, "demo")
	dir := filepath.Join(root, "demo")
	if _, err := svc.CreateEnv(CreateEnvInput{Project: "demo", Name: "dev", Vars: []EnvVarIn{
		{Name: "a", Value: "1"},
		{Name: "apiKey", Value: "real-key-123", Secret: true},
		{Name: "b", Value: "2"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteEnvVar("demo", "dev", "apiKey", ""); err != nil {
		t.Fatal(err)
	}
	envs, err := svc.ListEnvs("demo", "")
	if err != nil {
		t.Fatal(err)
	}
	e := envOf(t, envs, "dev")
	if len(e.Vars) != 2 {
		t.Fatalf("删除后应剩 2 个变量，实际 %d: %+v", len(e.Vars), e.Vars)
	}
	if _, ok := e.Vars["apiKey"]; ok {
		t.Error("apiKey 还在")
	}
	// 删掉敏感变量后，它的密钥也不该留在 secrets 文件里
	if strings.Contains(readSecretsFile(t, dir, "dev"), "real-key-123") {
		t.Errorf("secrets 文件里还留着已删变量的密钥:\n%s", readSecretsFile(t, dir, "dev"))
	}
	// 不存在的变量要报错，不能静默成功
	if err := svc.DeleteEnvVar("demo", "dev", "nope", ""); err == nil {
		t.Error("删除不存在的变量应报错")
	}
}

func TestEnvNameAndVarNameValidation(t *testing.T) {
	_, svc := setup(t)
	resetEnvs(t, svc, "demo")
	if _, err := svc.CreateEnv(CreateEnvInput{Project: "demo", Name: "dev"}); err != nil {
		t.Fatal(err)
	}
	bad := []struct{ desc, env, variable string }{
		{"空环境名", "", "a"},
		{"环境名含路径分隔符", "../evil", "a"},
		{"环境名含点", ".", "a"},
		{"环境名含斜杠", "a/b", "a"},
		{"变量名为空", "dev", ""},
		{"变量名含斜杠", "dev", "a/b"},
		{"变量名含反斜杠", "dev", `a\b`},
		{"变量名是 ..", "dev", ".."},
		{"变量名以数字开头", "dev", "1a"},
		{"变量名含空格", "dev", "a b"},
	}
	for _, c := range bad {
		if _, err := svc.SetEnvVar(SetEnvVarInput{Project: "demo", Env: c.env, Name: c.variable, Value: "v"}); err == nil {
			t.Errorf("%s（env=%q name=%q）应被拒绝", c.desc, c.env, c.variable)
		}
	}
	// create_env 也要拦非法环境名与非法变量名
	if _, err := svc.CreateEnv(CreateEnvInput{Project: "demo", Name: "bad name"}); err == nil {
		t.Error("create_env 应拒绝含空格的环境名")
	}
	if _, err := svc.CreateEnv(CreateEnvInput{Project: "demo", Name: "ok2", Vars: []EnvVarIn{{Name: "1x", Value: "v"}}}); err == nil {
		t.Error("create_env 应拒绝以数字开头的变量名")
	}
	// 重名不覆盖
	if _, err := svc.CreateEnv(CreateEnvInput{Project: "demo", Name: "dev"}); err == nil {
		t.Error("重名环境应报错而不是覆盖")
	}
	// 不存在的环境 / 同名改名
	if _, err := svc.RenameEnv("demo", "nope", "x", ""); err == nil {
		t.Error("改名不存在的环境应报错")
	}
	if _, err := svc.RenameEnv("demo", "dev", "dev", ""); err == nil {
		t.Error("改成同名应报错")
	}
	if err := svc.DeleteEnv("demo", "nope"); err == nil {
		t.Error("删除不存在的环境应报错")
	}
}

// TestNewServerWithoutFactoryWorks 是回归测试：曾因 cmd/mcpserver 忘了传 NewApp 工厂，
// 独立二进制里所有「打开项目」的工具都报「未提供运行时工厂」。NewRegistry 必须自带兜底。
func TestNewServerWithoutFactoryWorks(t *testing.T) {
	root := t.TempDir()
	newProjectDir(t, root, "demo", "uid-demo")
	_, svc, err := NewServer(Config{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	// 走一条真正需要打开项目的路径
	envs, err := svc.ListEnvs("demo", "")
	if err != nil {
		t.Fatalf("未注入工厂时打开项目失败: %v", err)
	}
	// 新建集合自带 dev 环境，能读到就说明链路通了
	if len(envs) != 1 || envs[0].Name != "dev" {
		t.Fatalf("环境列表 = %v，期望脚手架生成的 [dev]", envNames(envs))
	}
}
