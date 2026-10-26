package syncengine

import (
	"errors"
	"strings"
	"testing"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/secrets"
)

func newDeleteFailureEngine(t *testing.T, env collection.Env) (*collection.Collection, *Engine) {
	t.Helper()
	c, err := collection.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	// Open 创建的默认环境也叫 dev；同名覆盖后，测试中只有这里指定的环境与变量。
	if err := c.SaveEnv(env); err != nil {
		t.Fatalf("SaveEnv: %v", err)
	}
	e := &Engine{
		Coll:   c,
		Cipher: secrets.NewCipher(""),
		Bind: BindInfo{
			EnvHashes: map[string]string{env.UID: envHashOf(env.Name)},
			VarHashes: map[string]string{},
		},
	}
	for _, v := range env.Vars {
		e.Bind.VarHashes[v.UID] = varHashOf(v)
	}
	return c, e
}

func assertNoCollectedVarOps(t *testing.T, e *Engine) {
	t.Helper()
	b := newPushBatch(4)
	e.collectVarOps(b)
	if len(b.ops) != 0 {
		t.Fatalf("内容与基线均未变化时不应产生环境/变量操作，got %+v", b.ops)
	}
}

func TestApplyDeleteVariableSaveFailureKeepsBaseline(t *testing.T) {
	deleted := collection.Var{UID: "var-delete", Name: "deleted", Value: "old", Enabled: true}
	kept := collection.Var{UID: "var-keep", Name: "kept", Value: "same", Enabled: true}
	env := collection.Env{UID: "env-var-delete", Name: "dev", Vars: []collection.Var{deleted, kept}}
	_, e := newDeleteFailureEngine(t, env)

	injected := errors.New("injected SaveEnv failure")
	saveCalls := 0
	e.saveEnvForDelete = func(got collection.Env) error {
		saveCalls++
		for _, v := range got.Vars {
			if v.UID == deleted.UID {
				t.Errorf("写回环境中不应再包含待删变量：%+v", got.Vars)
			}
		}
		return injected
	}
	rep := &Report{}
	e.applyDelete(Item{Type: "variable", UID: deleted.UID, Op: "delete"}, rep)

	if saveCalls != 1 {
		t.Fatalf("删除变量应尝试保存一次，got %d", saveCalls)
	}
	if len(rep.Errors) != 1 || !strings.Contains(rep.Errors[0], injected.Error()) {
		t.Fatalf("SaveEnv 失败必须进入 Report.Errors，got %+v", rep.Errors)
	}
	if got := e.Bind.VarHashes[deleted.UID]; got != varHashOf(deleted) {
		t.Fatalf("删除失败必须保留变量基线：want %q got %q", varHashOf(deleted), got)
	}
	if got := e.Bind.VarHashes[kept.UID]; got != varHashOf(kept) {
		t.Fatalf("其他变量基线不应受影响：want %q got %q", varHashOf(kept), got)
	}
	if current, _, err := e.envByUIDVar(deleted.UID); err != nil || current.Name == "" {
		t.Fatalf("注入失败不应改动磁盘，待删变量应仍在本地：env=%+v err=%v", current, err)
	}
	// 旧实现会清掉摘要，残留变量因而在这里被当成新条目 upsert，复活服务端删除。
	assertNoCollectedVarOps(t, e)

	// 故障恢复后可幂等重试：成功落盘才清基线，其他变量保持不变。
	e.saveEnvForDelete = nil
	retryRep := &Report{}
	e.applyDelete(Item{Type: "variable", UID: deleted.UID, Op: "delete"}, retryRep)
	if len(retryRep.Errors) != 0 {
		t.Fatalf("恢复后重试不应报错，got %+v", retryRep.Errors)
	}
	if _, ok := e.Bind.VarHashes[deleted.UID]; ok {
		t.Fatal("删除成功后应清除变量基线")
	}
	if current, _, err := e.envByUIDVar(deleted.UID); err != nil || current.Name != "" {
		t.Fatalf("恢复后重试应删除本地变量：env=%+v err=%v", current, err)
	}
	current, _, err := e.envByUID(env.UID)
	if err != nil || len(current.Vars) != 1 || current.Vars[0].UID != kept.UID {
		t.Fatalf("重试只应删除目标变量，got env=%+v err=%v", current, err)
	}
	assertNoCollectedVarOps(t, e)
}

func TestApplyDeleteEnvFailureKeepsBaselinesAndContinues(t *testing.T) {
	firstVar := collection.Var{UID: "var-env-1", Name: "first", Value: "one", Enabled: true}
	secondVar := collection.Var{UID: "var-env-2", Name: "second", Value: "two", Enabled: true}
	env := collection.Env{UID: "env-delete", Name: "dev", Vars: []collection.Var{firstVar, secondVar}}
	c, e := newDeleteFailureEngine(t, env)
	otherVar := collection.Var{UID: "var-other", Name: "other", Value: "ok", Enabled: true}
	other := collection.Env{UID: "env-other", Name: "other", Vars: []collection.Var{otherVar}}
	if err := c.SaveEnv(other); err != nil {
		t.Fatalf("SaveEnv(other): %v", err)
	}
	e.Bind.EnvHashes[other.UID] = envHashOf(other.Name)
	e.Bind.VarHashes[otherVar.UID] = varHashOf(otherVar)

	injected := errors.New("injected DeleteEnv failure")
	e.deleteEnvForDelete = func(name string) error {
		if name == env.Name {
			return injected
		}
		return c.DeleteEnv(name)
	}
	rep := &Report{}
	// 第一条失败不得阻塞同批第二条环境删除。
	if err := e.applyItems([]Item{
		{Type: "variable_env", UID: env.UID, Op: "delete"},
		{Type: "variable_env", UID: other.UID, Op: "delete"},
	}, rep); err != nil {
		t.Fatalf("applyItems: %v", err)
	}
	if len(rep.Errors) != 1 || !strings.Contains(rep.Errors[0], injected.Error()) {
		t.Fatalf("DeleteEnv 失败必须进入 Report.Errors，got %+v", rep.Errors)
	}
	if got := e.Bind.EnvHashes[env.UID]; got != envHashOf(env.Name) {
		t.Fatalf("环境删除失败必须保留环境基线：want %q got %q", envHashOf(env.Name), got)
	}
	for _, v := range env.Vars {
		if got := e.Bind.VarHashes[v.UID]; got != varHashOf(v) {
			t.Fatalf("环境删除失败必须保留其变量基线 %s：want %q got %q", v.UID, varHashOf(v), got)
		}
	}
	if current, _, err := e.envByUID(env.UID); err != nil || current.Name == "" {
		t.Fatalf("注入失败不应改动磁盘，环境应仍在本地：env=%+v err=%v", current, err)
	}
	if _, ok := e.Bind.EnvHashes[other.UID]; ok {
		t.Fatal("同批其他环境删除成功后应清除环境基线")
	}
	if _, ok := e.Bind.VarHashes[otherVar.UID]; ok {
		t.Fatal("同批其他环境删除成功后应清除其变量基线")
	}
	if current, _, err := e.envByUID(other.UID); err != nil || current.Name != "" {
		t.Fatalf("第一条失败不得阻塞第二条删除：env=%+v err=%v", current, err)
	}
	// 旧实现清空失败环境及其变量摘要，下一轮会把整套残留重新 upsert。
	assertNoCollectedVarOps(t, e)

	e.deleteEnvForDelete = nil
	retryRep := &Report{}
	e.applyDelete(Item{Type: "variable_env", UID: env.UID, Op: "delete"}, retryRep)
	if len(retryRep.Errors) != 0 {
		t.Fatalf("恢复后重试不应报错，got %+v", retryRep.Errors)
	}
	if _, ok := e.Bind.EnvHashes[env.UID]; ok {
		t.Fatal("环境删除成功后应清除环境基线")
	}
	for _, v := range env.Vars {
		if _, ok := e.Bind.VarHashes[v.UID]; ok {
			t.Fatalf("环境删除成功后应清除变量基线 %s", v.UID)
		}
	}
	if current, _, err := e.envByUID(env.UID); err != nil || current.Name != "" {
		t.Fatalf("恢复后重试应删除本地环境：env=%+v err=%v", current, err)
	}
	assertNoCollectedVarOps(t, e)
}
