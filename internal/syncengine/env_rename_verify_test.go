package syncengine

// QA 独立验证（P1-7）：服务端把环境改名时留一份本地快照。
//
// 与工程师 env_rename_snapshot_test.go 互补，本文件只覆盖他没测/测不到的面：
//   ① Report.Conflicts 的**数字**（不是"有文件"就算过）+ 副本内容用既有 ConflictDetail 读；
//   ② secrets 不扩散：按字符串扫整个 .conflicts/ 目录，明文一个都不能出现；
//   ③ 两个不同环境同一轮各改名一次 → 各 1 份（共 2 份），不能被 snapshotOnce 的去重键串掉；
//   ④ 主文件已被删掉（索引里曾经有）→ 不崩、不产生空副本；
//   ⑤ 改名后的环境内容（含 secret 值）必须完整平移到新名字下。

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"api-doc-go-client/internal/collection"
)

// qa17SecretValue 敏感变量的明文，用来在整个 .conflicts/ 里搜。
const qa17SecretValue = "sk-qa17-SECRET-9f8e7d6c"

// qa17SeedEnv 落一个本地环境（含变量；secret 值会被 collection 拆到 .secrets.yml）。
func qa17SeedEnv(t *testing.T, c *collection.Collection, env collection.Env) {
	t.Helper()
	if err := c.SaveEnv(env); err != nil {
		t.Fatalf("SaveEnv(%s): %v", env.Name, err)
	}
}

// qa17Env 按 uid 取本地环境。
func qa17Env(t *testing.T, c *collection.Collection, uid string) collection.Env {
	t.Helper()
	envs, err := c.ListEnvs()
	if err != nil {
		t.Fatalf("ListEnvs: %v", err)
	}
	for _, env := range envs {
		if env.UID == uid {
			return env
		}
	}
	t.Fatalf("本地没有 uid=%s 的环境：%+v", uid, envs)
	return collection.Env{}
}

// qa17HasEnvNamed 本地是否存在该名字的环境文件。
func qa17HasEnvNamed(t *testing.T, c *collection.Collection, name string) bool {
	t.Helper()
	envs, err := c.ListEnvs()
	if err != nil {
		t.Fatalf("ListEnvs: %v", err)
	}
	for _, env := range envs {
		if env.Name == name {
			return true
		}
	}
	return false
}

// qa17VarValue 取环境里某个变量名的值（secret 也会被 ListEnvs 合并成明文）。
func qa17VarValue(t *testing.T, c *collection.Collection, envUID, varName string) string {
	t.Helper()
	env := qa17Env(t, c, envUID)
	for _, v := range env.Vars {
		if v.Name == varName {
			return v.Value
		}
	}
	t.Fatalf("环境 %s 里没有变量 %s：%+v", envUID, varName, env.Vars)
	return ""
}

// qa17Conflicts 当前 .conflicts/ 下的条目。
func qa17Conflicts(t *testing.T, c *collection.Collection) []collection.ConflictItem {
	t.Helper()
	list, err := c.ListConflicts()
	if err != nil {
		t.Fatalf("ListConflicts: %v", err)
	}
	return list
}

// qa17ConflictTexts 把 .conflicts/ 下**所有**文件（含非 .yml）的全文拼起来，用于搜明文。
func qa17ConflictTexts(t *testing.T, c *collection.Collection) map[string]string {
	t.Helper()
	out := map[string]string{}
	root := filepath.Join(c.Dir, ".conflicts")
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(root, p)
		out[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("遍历 .conflicts: %v", err)
	}
	return out
}

// qa17Baseline 首轮同步（远端为空）：把本地环境/变量推上去并固化摘要，之后改名才算"覆盖"。
func qa17Baseline(t *testing.T, f *fakeServer, c *collection.Collection, e *Engine) {
	t.Helper()
	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("基线同步: %v", err)
	}
	if rep.Conflicts != 0 {
		t.Fatalf("基线不该产生冲突副本，got %+v", rep)
	}
	if list := qa17Conflicts(t, c); len(list) != 0 {
		t.Fatalf("基线不该产生冲突副本: %+v", list)
	}
}

// qa17Rename 模拟服务端把某环境改名：推进游标并给出 changes 项。
func qa17Rename(t *testing.T, f *fakeServer, uid, newName string, rev int64) {
	t.Helper()
	f.state = State{Cursor: rev}
	f.changes = Changes{Cursor: rev, Items: []Item{{
		Type: "variable_env", UID: uid, Op: "upsert", ItemRev: rev,
		Payload: payloadOf(t, Payload{Name: newName}),
	}}}
}

// TestQA17RenameSnapshotCountedAndReadable 改名后：恰 1 份 env 副本、Conflicts 计 1、
// 副本能通过既有 ConflictDetail 读到旧内容，且本地改名+内容平移都成立。
func TestQA17RenameSnapshotCountedAndReadable(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 1}
	f.snapshot = Snapshot{Cursor: 1}
	c, e := newBoundEngine(t, cli)

	qa17SeedEnv(t, c, collection.Env{UID: "qa17-envA", Name: "old", Vars: []collection.Var{
		{UID: "qa17-vHost", Name: "host", Value: "http://local-dev", Enabled: true},
		{UID: "qa17-vTok", Name: "token", Value: qa17SecretValue, Enabled: true, Secret: true},
	}})
	qa17Baseline(t, f, c, e)

	qa17Rename(t, f, "qa17-envA", "new", 2)
	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("改名轮同步: %v", err)
	}
	// ① 计数必须精确：多计会让状态栏噪声，少计会让用户看不到"被吸收了"。
	if rep.Conflicts != 1 {
		t.Fatalf("远端改名应恰好计 1 个冲突，got %+v", rep)
	}
	// 附加：改名轮不产生推送（别名不回推，否则服务端会看到自己刚发的名字又变回来）。
	if rep.Pushed != 0 {
		t.Fatalf("改名轮不该回推任何 op，got %+v", rep)
	}

	// ② 本地已改名，且旧名字消失；变量与 secret 值完整平移。
	if env := qa17Env(t, c, "qa17-envA"); env.Name != "new" {
		t.Fatalf("本地环境应为新名字 new，got %+v", env)
	}
	if qa17HasEnvNamed(t, c, "old") {
		t.Fatalf("旧名字 old 不该还在")
	}
	if got := qa17VarValue(t, c, "qa17-envA", "host"); got != "http://local-dev" {
		t.Fatalf("普通变量值应平移，got %q", got)
	}
	if got := qa17VarValue(t, c, "qa17-envA", "token"); got != qa17SecretValue {
		t.Fatalf("secret 值应随 RenameEnv 平移，got %q", got)
	}

	// ③ 副本内容：kind/name/uid/path 与旧值。
	list := qa17Conflicts(t, c)
	if len(list) != 1 {
		t.Fatalf("应恰好 1 份副本，got %d: %+v", len(list), list)
	}
	item := list[0]
	if item.Kind != collection.ConflictKindEnv || item.Name != "old" || item.OfUID != "qa17-envA" {
		t.Fatalf("副本元信息应为 env/old/qa17-envA，got %+v", item)
	}
	raw, err := os.ReadFile(filepath.Join(c.Dir, ".conflicts", item.File))
	if err != nil {
		t.Fatalf("读副本文件: %v", err)
	}
	if !strings.Contains(string(raw), "path: environments/old.yml") {
		t.Fatalf("副本应记住原路径 environments/old.yml：\n%s", raw)
	}
	// 副本必须是**改名之前**的样子（info.name 仍是旧名），否则留底就没有证明力。
	if !strings.Contains(string(raw), "name: old") {
		t.Fatalf("副本应保留改名前的 info.name: old：\n%s", raw)
	}
	// ④ 副本走既有读取链路可读（不自己解析 yml）。
	det, err := c.ConflictDetail(item.File)
	if err != nil {
		t.Fatalf("ConflictDetail: %v", err)
	}
	if det.Mode != collection.ConflictModeSnapshot {
		t.Fatalf("环境副本应按整文件快照渲染，got mode=%q", det.Mode)
	}
	if !strings.Contains(det.Preview, "http://local-dev") {
		t.Fatalf("预览应能读到旧文件内容，got %q", det.Preview)
	}
	if !strings.Contains(det.RemotePreview, "old") || !strings.Contains(det.RemotePreview, "new") {
		t.Fatalf("远端摘要应说明 old → new，got %q", det.RemotePreview)
	}
}

// TestQA17RenameDoesNotLeakSecretsIntoConflicts 改名副本不得把 .secrets.yml 的明文带进 .conflicts/。
func TestQA17RenameDoesNotLeakSecretsIntoConflicts(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 1}
	f.snapshot = Snapshot{Cursor: 1}
	c, e := newBoundEngine(t, cli)

	qa17SeedEnv(t, c, collection.Env{UID: "qa17-envS", Name: "staging", Vars: []collection.Var{
		{UID: "qa17-vTok", Name: "token", Value: qa17SecretValue, Enabled: true, Secret: true},
		{UID: "qa17-vHost", Name: "host", Value: "http://staging", Enabled: true},
	}})
	// 前置条件：本地 secrets 文件确实存在且是明文（否则本用例是空跑）。
	secPath := filepath.Join(c.Dir, "environments", "staging.secrets.yml")
	secRaw, err := os.ReadFile(secPath)
	if err != nil {
		t.Fatalf("前置条件：应存在环境 secrets 文件，err=%v", err)
	}
	if !strings.Contains(string(secRaw), qa17SecretValue) {
		t.Fatalf("前置条件：secrets 文件里应有明文密钥：\n%s", secRaw)
	}
	qa17Baseline(t, f, c, e)

	qa17Rename(t, f, "qa17-envS", "staging2", 2)
	if _, err := e.RunOne(); err != nil {
		t.Fatalf("改名轮同步: %v", err)
	}

	// 密钥值本身没丢：新名字下仍在。
	if got := qa17VarValue(t, c, "qa17-envS", "token"); got != qa17SecretValue {
		t.Fatalf("改名后 secret 值应保留在新环境里，got %q", got)
	}
	if _, err := os.Stat(secPath); !os.IsNotExist(err) {
		t.Fatalf("旧名字的 secrets 文件应已被搬走，stat err=%v", err)
	}

	// 关键断言：.conflicts/ 整个目录（含隐藏文件）不得出现密钥明文。
	texts := qa17ConflictTexts(t, c)
	if len(texts) == 0 {
		t.Fatal("没有副本可扫，用例空跑（改名前应已留底）")
	}
	for name, body := range texts {
		if strings.Contains(body, qa17SecretValue) {
			t.Fatalf(".conflicts/%s 里出现了密钥明文（敏感值不得复制进副本）", name)
		}
	}
	// 反向确认扫的是真东西：副本里应当有非敏感的 host 名/旧名字。
	joined := ""
	for _, body := range texts {
		joined += body
	}
	if !strings.Contains(joined, "host") || !strings.Contains(joined, "staging") {
		t.Fatalf("副本内容不含预期字样，扫描可能是空跑：%v", keysOf(texts))
	}
}

// keysOf 仅用于失败信息里展示扫描到的文件名。
func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestQA17TwoEnvsRenamedSameRoundTwoSnapshots 同一轮两个不同环境各改一次名 → 各 1 份（共 2 份）。
func TestQA17TwoEnvsRenamedSameRoundTwoSnapshots(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 1}
	f.snapshot = Snapshot{Cursor: 1}
	c, e := newBoundEngine(t, cli)

	qa17SeedEnv(t, c, collection.Env{UID: "qa17-env1", Name: "alpha", Vars: []collection.Var{
		{UID: "qa17-v1", Name: "a", Value: "1", Enabled: true},
	}})
	qa17SeedEnv(t, c, collection.Env{UID: "qa17-env2", Name: "beta", Vars: []collection.Var{
		{UID: "qa17-v2", Name: "b", Value: "2", Enabled: true},
	}})
	qa17Baseline(t, f, c, e)

	f.state = State{Cursor: 2}
	f.changes = Changes{Cursor: 2, Items: []Item{
		{Type: "variable_env", UID: "qa17-env1", Op: "upsert", ItemRev: 2,
			Payload: payloadOf(t, Payload{Name: "alpha2"})},
		{Type: "variable_env", UID: "qa17-env2", Op: "upsert", ItemRev: 3,
			Payload: payloadOf(t, Payload{Name: "beta2"})},
	}}
	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("同步: %v", err)
	}
	if rep.Conflicts != 2 {
		t.Fatalf("两个环境各改一次名应计 2 个冲突，got %+v", rep)
	}
	list := qa17Conflicts(t, c)
	if len(list) != 2 {
		t.Fatalf("应留 2 份副本（每环境 1 份），got %d: %+v", len(list), list)
	}
	names := []string{list[0].Name, list[1].Name}
	sort.Strings(names)
	if names[0] != "alpha" || names[1] != "beta" {
		t.Fatalf("两份副本应分别记旧名 alpha/beta，got %v", names)
	}
	for _, it := range list {
		if it.Kind != collection.ConflictKindEnv {
			t.Fatalf("副本类型应为 env，got %+v", it)
		}
	}
	// 本地两个环境都改了名，变量各自保留。
	if got := qa17Env(t, c, "qa17-env1"); got.Name != "alpha2" || qa17VarValue(t, c, "qa17-env1", "a") != "1" {
		t.Fatalf("env1 应改名为 alpha2 且保留变量，got %+v", got)
	}
	if got := qa17Env(t, c, "qa17-env2"); got.Name != "beta2" || qa17VarValue(t, c, "qa17-env2", "b") != "2" {
		t.Fatalf("env2 应改名为 beta2 且保留变量，got %+v", got)
	}
}

// TestQA17SameNameNoSnapshot 服务端名字没变（no-op）：0 份、无副作用。
func TestQA17SameNameNoSnapshot(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 1}
	f.snapshot = Snapshot{Cursor: 1}
	c, e := newBoundEngine(t, cli)

	qa17SeedEnv(t, c, collection.Env{UID: "qa17-envN", Name: "same", Vars: []collection.Var{
		{UID: "qa17-vn", Name: "k", Value: "v", Enabled: true},
	}})
	qa17Baseline(t, f, c, e)

	qa17Rename(t, f, "qa17-envN", "same", 2)
	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("同步: %v", err)
	}
	if rep.Conflicts != 0 {
		t.Fatalf("同名是 no-op，不该计冲突，got %+v", rep)
	}
	if list := qa17Conflicts(t, c); len(list) != 0 {
		t.Fatalf("同名不该留副本: %+v", list)
	}
	if got := qa17VarValue(t, c, "qa17-envN", "k"); got != "v" {
		t.Fatalf("同名轮不该动环境内容，got %q", got)
	}
}

// TestQA17LocalMissingEnvNoSnapshot 本地没有该环境（服务端新建）：0 份，环境正常创建。
func TestQA17LocalMissingEnvNoSnapshot(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 1}
	f.snapshot = Snapshot{Cursor: 1}
	c, e := newBoundEngine(t, cli)
	qa17Baseline(t, f, c, e)

	qa17Rename(t, f, "qa17-envNew", "brand-new", 2)
	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("同步: %v", err)
	}
	if rep.Conflicts != 0 {
		t.Fatalf("新建环境不存在覆盖，不该计冲突，got %+v", rep)
	}
	if list := qa17Conflicts(t, c); len(list) != 0 {
		t.Fatalf("新建环境不该留副本: %+v", list)
	}
	if env := qa17Env(t, c, "qa17-envNew"); env.Name != "brand-new" {
		t.Fatalf("环境应被正常创建为 brand-new，got %+v", env)
	}
}

// TestQA17MissingEnvFileNoEmptySnapshot 主文件被删掉（本地只剩其他痕迹）：
// 不崩、不产生空副本，按"新建"处理。
func TestQA17MissingEnvFileNoEmptySnapshot(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 1}
	f.snapshot = Snapshot{Cursor: 1}
	c, e := newBoundEngine(t, cli)

	qa17SeedEnv(t, c, collection.Env{UID: "qa17-envD", Name: "vanished", Vars: []collection.Var{
		{UID: "qa17-vd", Name: "k", Value: "v", Enabled: true},
	}})
	qa17Baseline(t, f, c, e)

	// 主文件被外部删掉（secrets 文件留着，模拟"清理了一半"）。
	mainPath := filepath.Join(c.Dir, "environments", "vanished.yml")
	if err := os.Remove(mainPath); err != nil {
		t.Fatalf("删主文件: %v", err)
	}

	qa17Rename(t, f, "qa17-envD", "renamed", 2)
	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("同步不该因主文件缺失而失败: %v", err)
	}
	if rep.Conflicts != 0 {
		t.Fatalf("没有可留底的文件，不该计冲突，got %+v", rep)
	}
	if list := qa17Conflicts(t, c); len(list) != 0 {
		t.Fatalf("主文件缺失时不该产生（空）副本: %+v", list)
	}
	// 环境按新建落地（旧主文件已不存在，无从 Rename）。
	if env := qa17Env(t, c, "qa17-envD"); env.Name != "renamed" {
		t.Fatalf("应按新建落地为 renamed，got %+v", env)
	}
}
