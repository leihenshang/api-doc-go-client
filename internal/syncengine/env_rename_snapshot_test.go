package syncengine

// P1-7：服务端把环境改名时，applyVarEnv 会直接 RenameEnv 覆盖本地名字，
// 此前没有任何留底——本地命名意图被静默吸收。这里锁定修复后的窄口子：
// 只有「本地有该环境」且「名字不一致」才在 .conflicts/ 留一份主文件快照。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"api-doc-go-client/internal/collection"
)

// envByUIDForTest 按 uid 取本地环境：集合打开时可能已内置一个 dev 环境，
// 所以断言不能假设「只有一个环境」。
func envByUIDForTest(t *testing.T, c *collection.Collection, uid string) collection.Env {
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

// envNameExistsForTest 本地是否存在该名字的环境。
func envNameExistsForTest(t *testing.T, c *collection.Collection, name string) bool {
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

// TestRunOneSnapshotsLocalEnvBeforeRemoteRename 改名：留一份 Kind=env 的主文件副本，
// 本地环境改叫服务端的新名字。
func TestRunOneSnapshotsLocalEnvBeforeRemoteRename(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 1}
	f.snapshot = Snapshot{Cursor: 1, Items: []Item{
		{Type: "variable_env", UID: "env1", Op: "upsert", ItemRev: 1,
			Payload: payloadOf(t, Payload{Name: "old"})},
		{Type: "variable", UID: "v1", Op: "upsert", ItemRev: 2,
			Payload: payloadOf(t, Payload{Name: "host", Value: "http://local-dev", EnvUID: "env1"})},
	}}
	c, e := newBoundEngine(t, cli)
	if _, err := e.RunOne(); err != nil {
		t.Fatalf("首轮同步: %v", err)
	}

	// 远端把同一个环境改了名
	f.state = State{Cursor: 2}
	f.changes = Changes{Cursor: 2, Items: []Item{{
		Type: "variable_env", UID: "env1", Op: "upsert", ItemRev: 3,
		Payload: payloadOf(t, Payload{Name: "new"}),
	}}}
	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("第二轮同步: %v", err)
	}
	if rep.Conflicts != 1 {
		t.Fatalf("远端改名覆盖本地命名应计 1 个冲突，got %+v", rep)
	}

	// 本地环境已改叫服务端的名字（内容由 RenameEnv 一起搬过去）
	env := envByUIDForTest(t, c, "env1")
	if env.Name != "new" {
		t.Fatalf("本地环境应为服务端的新名字，got %+v", env)
	}
	if envNameExistsForTest(t, c, "old") {
		t.Fatalf("旧名字 old 应已随改名消失")
	}
	found := false
	for _, v := range env.Vars {
		if v.UID == "v1" && v.Value == "http://local-dev" {
			found = true
		}
	}
	if !found {
		t.Fatalf("改名后变量应随文件一起搬家，got %+v", env.Vars)
	}

	// .conflicts/ 里有一份旧名字的主文件副本
	list, err := c.ListConflicts()
	if err != nil {
		t.Fatalf("ListConflicts: %v", err)
	}
	if len(list) != 1 || list[0].Kind != "env" {
		t.Fatalf("应留一份环境副本: %+v", list)
	}
	if list[0].Name != "old" || list[0].OfUID != "env1" {
		t.Fatalf("副本应记旧名字 old / uid env1，got %+v", list[0])
	}
	raw, err := os.ReadFile(filepath.Join(c.Dir, ".conflicts", list[0].File))
	if err != nil {
		t.Fatalf("读副本: %v", err)
	}
	if !strings.Contains(string(raw), "path: environments/old.yml") {
		t.Fatalf("副本应记住原文件路径 environments/old.yml:\n%s", raw)
	}
	det, err := c.ConflictDetail(list[0].File)
	if err != nil {
		t.Fatalf("ConflictDetail: %v", err)
	}
	if !strings.Contains(det.Preview, "http://local-dev") {
		t.Fatalf("副本里能读到本地变量值，got %q", det.Preview)
	}
}

// TestSnapshotOnceDedupesEnvCopiesWithinRound 同一轮内同一个环境文件被判定多次，
// 只留一份副本（snapshotOnce 按 kind+rel 去重）。改名路径本身是幂等的（第二轮已是同名），
// 所以这里直接驱动去重的那一层。
func TestSnapshotOnceDedupesEnvCopiesWithinRound(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 1}
	f.snapshot = Snapshot{Cursor: 1, Items: []Item{
		{Type: "variable_env", UID: "env4", Op: "upsert", ItemRev: 1,
			Payload: payloadOf(t, Payload{Name: "blue"})},
	}}
	c, e := newBoundEngine(t, cli)
	if _, err := e.RunOne(); err != nil {
		t.Fatalf("首轮同步: %v", err)
	}

	rep := &Report{}
	rel := "environments/blue.yml"
	e.snapshotOnce(rep, collection.ConflictKindEnv, "env4", "blue", rel, "远端摘要", []byte("info:\n  name: blue\n"))
	e.snapshotOnce(rep, collection.ConflictKindEnv, "env4", "blue", rel, "远端摘要", []byte("info:\n  name: blue\n"))
	if rep.Conflicts != 1 {
		t.Fatalf("同一轮只应留一份副本，got %+v", rep)
	}
	if list, _ := c.ListConflicts(); len(list) != 1 {
		t.Fatalf("同一轮只应留一份副本，got %+v", list)
	}
}

// TestRunOneNoSnapshotOnEmptyEnvName 服务端下发空名字：applyVarEnv 是 no-op，
// 本地名不变 = 没有发生覆盖，所以不能留副本、也不能计冲突（否则用户会看到
// 一条「把 old 改名为（空）」的假冲突）。
func TestRunOneNoSnapshotOnEmptyEnvName(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 1}
	f.snapshot = Snapshot{Cursor: 1, Items: []Item{
		{Type: "variable_env", UID: "env5", Op: "upsert", ItemRev: 1,
			Payload: payloadOf(t, Payload{Name: "keepme"})},
		{Type: "variable", UID: "v5", Op: "upsert", ItemRev: 2,
			Payload: payloadOf(t, Payload{Name: "host", Value: "http://a", EnvUID: "env5"})},
	}}
	c, e := newBoundEngine(t, cli)
	if _, err := e.RunOne(); err != nil {
		t.Fatalf("首轮同步: %v", err)
	}

	f.state = State{Cursor: 2}
	f.changes = Changes{Cursor: 2, Items: []Item{{
		Type: "variable_env", UID: "env5", Op: "upsert", ItemRev: 3,
		Payload: payloadOf(t, Payload{Name: ""}),
	}}}
	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("第二轮同步: %v", err)
	}
	if rep.Conflicts != 0 {
		t.Fatalf("空名字是 no-op，不该计冲突，got %+v", rep)
	}
	if list, _ := c.ListConflicts(); len(list) != 0 {
		t.Fatalf("空名字不该留副本: %+v", list)
	}
	if env := envByUIDForTest(t, c, "env5"); env.Name != "keepme" {
		t.Fatalf("空名字不该改动本地环境名，got %+v", env)
	}
}

// TestRunOneNoSnapshotWhenRenameFails 目标名被本地另一个环境占用 → RenameEnv 失败，
// 本地根本没被覆盖：不许留副本、不许计冲突，错误照原路径进 Report.Errors。
func TestRunOneNoSnapshotWhenRenameFails(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 1}
	f.snapshot = Snapshot{Cursor: 1}
	c, e := newBoundEngine(t, cli)

	// 本地两个环境：被改名的 old，以及占住目标名 taken 的另一个。
	if err := c.SaveEnv(collection.Env{UID: "envA", Name: "old", Vars: []collection.Var{
		{UID: "vA", Name: "k", Value: "v", Enabled: true},
	}}); err != nil {
		t.Fatalf("SaveEnv(old): %v", err)
	}
	if err := c.SaveEnv(collection.Env{UID: "envB", Name: "taken"}); err != nil {
		t.Fatalf("SaveEnv(taken): %v", err)
	}
	if _, err := e.RunOne(); err != nil { // 基线：把两个环境推上去
		t.Fatalf("基线同步: %v", err)
	}

	// 服务端把 envA 改名成「taken」——本地这个名字已被 envB 占着。
	f.state = State{Cursor: 2}
	f.changes = Changes{Cursor: 2, Items: []Item{{
		Type: "variable_env", UID: "envA", Op: "upsert", ItemRev: 2,
		Payload: payloadOf(t, Payload{Name: "taken"}),
	}}}
	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("同步不该整轮失败（单条错误进 Report.Errors）: %v", err)
	}
	if len(rep.Errors) == 0 {
		t.Fatalf("改名失败应上报错误，got %+v", rep)
	}
	joined := strings.Join(rep.Errors, " | ")
	if !strings.Contains(joined, "taken") {
		t.Fatalf("错误信息应指出目标名被占用，got %q", joined)
	}
	if rep.Conflicts != 0 {
		t.Fatalf("本地未被覆盖，不该计冲突，got %+v", rep)
	}
	if list, _ := c.ListConflicts(); len(list) != 0 {
		t.Fatalf("本地未被覆盖，不该留副本: %+v", list)
	}
	// 本地没被覆盖：envA 仍叫 old，envB 仍叫 taken。
	if env := envByUIDForTest(t, c, "envA"); env.Name != "old" {
		t.Fatalf("改名失败后本地应保持旧名字 old，got %+v", env)
	}
	if env := envByUIDForTest(t, c, "envB"); env.Name != "taken" {
		t.Fatalf("被占用的 taken 不该被顶掉，got %+v", env)
	}
}

// TestRunOneDoesNotSnapshotEnvOnSameName 同名：服务端名字没变，没有覆盖，不留副本。
func TestRunOneDoesNotSnapshotEnvOnSameName(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 1}
	f.snapshot = Snapshot{Cursor: 1, Items: []Item{
		{Type: "variable_env", UID: "env2", Op: "upsert", ItemRev: 1,
			Payload: payloadOf(t, Payload{Name: "stage"})},
		{Type: "variable", UID: "v2", Op: "upsert", ItemRev: 2,
			Payload: payloadOf(t, Payload{Name: "host", Value: "http://a", EnvUID: "env2"})},
	}}
	c, e := newBoundEngine(t, cli)
	if _, err := e.RunOne(); err != nil {
		t.Fatalf("首轮同步: %v", err)
	}

	f.state = State{Cursor: 2}
	f.changes = Changes{Cursor: 2, Items: []Item{{
		Type: "variable_env", UID: "env2", Op: "upsert", ItemRev: 3,
		Payload: payloadOf(t, Payload{Name: "stage"}),
	}}}
	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("第二轮同步: %v", err)
	}
	if rep.Conflicts != 0 {
		t.Fatalf("同名是 no-op，不该产生副本，got %+v", rep)
	}
	list, err := c.ListConflicts()
	if err != nil {
		t.Fatalf("ListConflicts: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("同名不该产生环境副本: %+v", list)
	}
	if env := envByUIDForTest(t, c, "env2"); env.Name != "stage" {
		t.Fatalf("同名不该改动环境名，got %+v", env)
	}
}

// TestRunOneDoesNotSnapshotEnvOnLocalCreate 本地没有该环境（服务端新建）：
// 不存在覆盖，不留副本，环境照常创建。
func TestRunOneDoesNotSnapshotEnvOnLocalCreate(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 1}
	f.snapshot = Snapshot{Cursor: 1, Items: []Item{}}
	c, e := newBoundEngine(t, cli)
	if _, err := e.RunOne(); err != nil {
		t.Fatalf("首轮同步: %v", err)
	}

	f.state = State{Cursor: 2}
	f.changes = Changes{Cursor: 2, Items: []Item{{
		Type: "variable_env", UID: "env3", Op: "upsert", ItemRev: 1,
		Payload: payloadOf(t, Payload{Name: "prod"}),
	}}}
	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("第二轮同步: %v", err)
	}
	if rep.Conflicts != 0 {
		t.Fatalf("本地没有该环境，不存在覆盖，不该产生副本，got %+v", rep)
	}
	if list, _ := c.ListConflicts(); len(list) != 0 {
		t.Fatalf("新建环境不该产生副本: %+v", list)
	}
	if env := envByUIDForTest(t, c, "env3"); env.Name != "prod" {
		t.Fatalf("环境应被正常创建，got %+v", env)
	}
}
