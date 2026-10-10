package collection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openTemp(t *testing.T) *Collection {
	t.Helper()
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return c
}

func TestOpenCreatesManifestAndGitignore(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if c.UID == "" || c.Name == "" {
		t.Fatalf("清单未初始化: %+v", c)
	}
	for _, f := range []string{"opencollection.yml", ".gitignore", filepath.Join("environments", "dev.yml")} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("缺少 %s: %v", f, err)
		}
	}
	// 再次打开：uid 稳定
	c2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if c2.UID != c.UID {
		t.Fatalf("uid 不稳定: %s vs %s", c.UID, c2.UID)
	}
}

func TestRequestCRUDAndTree(t *testing.T) {
	c := openTemp(t)
	if err := c.CreateFolder("", "用户"); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	r1, err := c.CreateRequest("", "根接口", "GET")
	if err != nil {
		t.Fatalf("CreateRequest root: %v", err)
	}
	r2, err := c.CreateRequest("用户", "用户-列表", "POST")
	if err != nil {
		t.Fatalf("CreateRequest folder: %v", err)
	}
	if r1.UID == "" || r2.UID == "" || r1.UID == r2.UID {
		t.Fatalf("uid 异常")
	}
	if !strings.Contains(r2.Path, "用户/") {
		t.Fatalf("路径应位于分组目录: %q", r2.Path)
	}

	// 树：分组在前
	tree, err := c.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if len(tree) != 2 || tree[0].Type != "folder" || tree[1].Type != "request" {
		t.Fatalf("树结构异常: %+v", tree)
	}
	if len(tree[0].Children) != 1 || tree[0].Children[0].UID != r2.UID {
		t.Fatalf("分组子项异常: %+v", tree[0].Children)
	}

	// 读回
	got, err := c.ReadRequest(r2.UID)
	if err != nil {
		t.Fatalf("ReadRequest: %v", err)
	}
	if got.Name != "用户-列表" || got.Method != "POST" || got.BaseRev != 0 {
		t.Fatalf("读回不一致: %+v", got)
	}

	// 保存（改名 + 修改 URL）
	got.Name = "list-users"
	got.URL = "{{host}}/users"
	if err := c.SaveRequest(got); err != nil {
		t.Fatalf("SaveRequest: %v", err)
	}
	again, _ := c.ReadRequest(r2.UID)
	if again.Name != "list-users" || again.URL != "{{host}}/users" {
		t.Fatalf("保存未生效: %+v", again)
	}
	data, _ := os.ReadFile(filepath.Join(c.Dir, filepath.FromSlash(again.Path)))
	// meta.uid / info.type 必须写回文件（对齐设计文档 §3.3）
	for _, want := range []string{"uid:", "type: http", "base_rev:"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("文件缺少 %s:\n%s", want, data)
		}
	}

	// 删除 → .trash/
	if err := c.DeleteRequest(r1.UID); err != nil {
		t.Fatalf("DeleteRequest: %v", err)
	}
	if _, err := c.ReadRequest(r1.UID); err == nil {
		t.Fatalf("删除后仍可读")
	}
	entries, _ := os.ReadDir(filepath.Join(c.Dir, ".trash"))
	if len(entries) != 1 {
		t.Fatalf(".trash 应有 1 个文件: %d", len(entries))
	}
}

func TestSaveRequestRejectsPathEscape(t *testing.T) {
	c := openTemp(t)
	err := c.SaveRequest(&Request{UID: "x", Path: "../evil.yml"})
	if err == nil {
		t.Fatalf("应拒绝越界路径")
	}
}

func TestEnvSecretsSplit(t *testing.T) {
	c := openTemp(t)
	env := Env{Name: "prod", Vars: []Var{
		{Name: "host", Value: "https://api.example.com", Enabled: true},
		{Name: "token", Value: "s3cret", Enabled: true, Secret: true},
	}}
	if err := c.SaveEnv(env); err != nil {
		t.Fatalf("SaveEnv: %v", err)
	}
	// 主文件不含明文 secret；secrets 文件含
	mainData, _ := os.ReadFile(filepath.Join(c.Dir, "environments", "prod.yml"))
	if strings.Contains(string(mainData), "s3cret") {
		t.Fatalf("主环境文件不应包含 secret 明文:\n%s", mainData)
	}
	secData, _ := os.ReadFile(filepath.Join(c.Dir, "environments", "prod.secrets.yml"))
	if !strings.Contains(string(secData), "s3cret") {
		t.Fatalf("secrets 文件应包含明文:\n%s", secData)
	}
	// 读回：secret 值已合并
	envs, err := c.ListEnvs()
	if err != nil {
		t.Fatalf("ListEnvs: %v", err)
	}
	if len(envs) != 2 { // dev（Open 时自动创建）+ prod
		t.Fatalf("环境数量: %+v", envs)
	}
	var got *Env
	for i := range envs {
		if envs[i].Name == "prod" {
			got = &envs[i]
		}
	}
	if got == nil || len(got.Vars) != 2 || got.Vars[1].Value != "s3cret" {
		t.Fatalf("secret 合并失败: %+v", got)
	}
	// 删除 → .trash/
	if err := c.DeleteEnv("prod"); err != nil {
		t.Fatalf("DeleteEnv: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "environments", "prod.yml")); !os.IsNotExist(err) {
		t.Fatalf("环境文件应已移除")
	}
}

// TestEnvNameAcceptsChinese 回归：中文 / 数字 / 括号等名称必须能建、能改名。
//
// 这条测试是按「客户端实际链接的共享包」校验的 —— 现象曾出现在客户端明明放开了字符集、
// 界面却仍报「只能包含字母、数字、- 与 _」，根因是 go.mod 里锁的还是旧版共享包，
// 只改本地 share 源码不生效。所以这里从 Collection 的真实入口走一遍，防止版本回退再次漏网。
func TestEnvNameAcceptsChinese(t *testing.T) {
	c := openTemp(t)
	cases := []string{"测试环境1", "测试环境", "预发(v2)", "预发（v2）", "dev.v1", "生产环境-华东"}
	for _, name := range cases {
		if err := c.SaveEnv(Env{Name: name}); err != nil {
			t.Fatalf("SaveEnv(%q) 应被接受: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(c.Dir, "environments", name+".yml")); err != nil {
			t.Fatalf("环境文件未落盘 %q: %v", name, err)
		}
	}
	// 改中文名：旧文件消失、新文件出现
	if err := c.RenameEnv("测试环境1", "测试环境2", ""); err != nil {
		t.Fatalf("RenameEnv 中文名: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "environments", "测试环境1.yml")); !os.IsNotExist(err) {
		t.Fatalf("改名后旧文件应移除")
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "environments", "测试环境2.yml")); err != nil {
		t.Fatalf("改名后新文件应存在: %v", err)
	}
	// 仍然拒绝的：路径分隔符、空格、Windows 非法字符、前导点
	for _, bad := range []string{"a/b", "a b", "a:b", ".hidden", ".."} {
		if err := c.SaveEnv(Env{Name: bad}); err == nil {
			t.Fatalf("SaveEnv(%q) 应被拒绝", bad)
		}
	}
}
