package syncengine

// 拉取覆盖前的留底（P0）：文档 / 示例 / 环境在本地有未推送改动时，
// 远端更新不能把它们静默盖掉——必须先落一份整文件快照到 .conflicts/。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunOneSnapshotsDirtyDocBeforeOverwrite(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 1}
	f.snapshot = Snapshot{Cursor: 1, Items: []Item{{
		Type: "doc", UID: "d1", Op: "upsert", ItemRev: 1,
		Payload: payloadOf(t, Payload{Name: "设计说明", Content: "v1"}),
	}}}
	c, e := newBoundEngine(t, cli)
	if _, err := e.RunOne(); err != nil {
		t.Fatalf("首轮同步: %v", err)
	}

	// 本地编辑但尚未推送
	d, err := c.ReadDoc("d1")
	if err != nil {
		t.Fatalf("ReadDoc: %v", err)
	}
	d.Content = "本地未推送的修改"
	if err := c.SaveDoc(d); err != nil {
		t.Fatalf("SaveDoc: %v", err)
	}

	// 远端推进到 v2，下一轮 pull 会覆盖本地
	f.state = State{Cursor: 2}
	f.changes = Changes{Cursor: 2, Items: []Item{{
		Type: "doc", UID: "d1", Op: "upsert", ItemRev: 2,
		Payload: payloadOf(t, Payload{Name: "设计说明", Content: "v2"}),
	}}}
	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("第二轮同步: %v", err)
	}
	if rep.Conflicts != 1 {
		t.Fatalf("本地改动被覆盖应计 1 个冲突，got %+v", rep)
	}

	// 远端版本已落地
	d, err = c.ReadDoc("d1")
	if err != nil {
		t.Fatalf("ReadDoc(第二轮): %v", err)
	}
	if d.Content != "v2" {
		t.Fatalf("远端版本应已应用，got %q", d.Content)
	}

	// 本地改动在 .conflicts/ 里可找回
	list, err := c.ListConflicts()
	if err != nil {
		t.Fatalf("ListConflicts: %v", err)
	}
	if len(list) != 1 || list[0].Kind != "doc" || list[0].OfUID != "d1" {
		t.Fatalf("应留一份文档快照: %+v", list)
	}
	det, err := c.ConflictDetail(list[0].File)
	if err != nil {
		t.Fatalf("ConflictDetail: %v", err)
	}
	if !strings.Contains(det.Preview, "本地未推送的修改") {
		t.Fatalf("快照应包含本地改动，got %q", det.Preview)
	}

	if err := c.ResolveConflict(list[0].File, "local"); err != nil {
		t.Fatalf("还原本地版本: %v", err)
	}
	d, _ = c.ReadDoc("d1")
	if d.Content != "本地未推送的修改" {
		t.Fatalf("还原后应是本地内容，got %q", d.Content)
	}
}

// 文档 pull 后必须把 DocHashes 前移：否则 mirror 模式会每轮都把它当成本地改动
// （既产生伪冲突，也会在 auto 模式把刚拉下来的内容原样推回去）。
func TestRunOneDoesNotSnapshotCleanDocOnSecondPull(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 1}
	f.snapshot = Snapshot{Cursor: 1, Items: []Item{{
		Type: "doc", UID: "d2", Op: "upsert", ItemRev: 1,
		Payload: payloadOf(t, Payload{Name: "干净文档", Content: "same"}),
	}}}
	_, e := newBoundEngine(t, cli)
	if _, err := e.RunOne(); err != nil {
		t.Fatalf("首轮: %v", err)
	}
	f.state = State{Cursor: 2}
	f.changes = Changes{Cursor: 2, Items: []Item{{
		Type: "doc", UID: "d2", Op: "upsert", ItemRev: 2,
		Payload: payloadOf(t, Payload{Name: "干净文档", Content: "same"}),
	}}}
	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("第二轮: %v", err)
	}
	if rep.Conflicts != 0 {
		t.Fatalf("本地没改过，不该产生冲突，got %+v", rep)
	}
}

// 环境变量被远端覆盖前，所属环境文件应整份留底。
func TestRunOneSnapshotsDirtyVariableBeforeOverwrite(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 1}
	// 先拉一个环境（variable_env 先于 variable 应用）
	f.snapshot = Snapshot{Cursor: 1, Items: []Item{
		{Type: "variable_env", UID: "env1", Op: "upsert", ItemRev: 1,
			Payload: payloadOf(t, Payload{Name: "dev"})},
		{Type: "variable", UID: "v1", Op: "upsert", ItemRev: 2,
			Payload: payloadOf(t, Payload{Name: "host", Value: "http://a", EnvUID: "env1"})},
	}}
	c, e := newBoundEngine(t, cli)
	if _, err := e.RunOne(); err != nil {
		t.Fatalf("首轮: %v", err)
	}

	// 本地改这个变量的值（未推送）
	envs, err := c.ListEnvs()
	if err != nil || len(envs) == 0 {
		t.Fatalf("ListEnvs: %v %+v", err, envs)
	}
	envs[0].Vars[0].Value = "http://local"
	if err := c.SaveEnv(envs[0]); err != nil {
		t.Fatalf("SaveEnv: %v", err)
	}

	f.state = State{Cursor: 2}
	f.changes = Changes{Cursor: 2, Items: []Item{{
		Type: "variable", UID: "v1", Op: "upsert", ItemRev: 3,
		Payload: payloadOf(t, Payload{Name: "host", Value: "http://b", EnvUID: "env1"}),
	}}}
	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("第二轮: %v", err)
	}
	if rep.Conflicts != 1 {
		t.Fatalf("本地变量改动被覆盖应计冲突，got %+v", rep)
	}
	list, _ := c.ListConflicts()
	if len(list) != 1 || list[0].Kind != "env" {
		t.Fatalf("应留一份环境快照: %+v", list)
	}
	raw, err := os.ReadFile(filepath.Join(c.Dir, ".conflicts", list[0].File))
	if err != nil {
		t.Fatalf("读副本: %v", err)
	}
	if !strings.Contains(string(raw), "http://local") {
		t.Fatalf("快照应包含本地变量值:\n%s", raw)
	}
}
