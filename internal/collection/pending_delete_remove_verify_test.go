package collection

// QA 独立验证（P2-1）：RemovePendingDeletes 的匹配口径。
//
// 刻意不复用工程师 pending_delete_remove_test.go 的 p21* helper，独立构造数据，
// 只锁本次改动真正承诺的两条行为：
//   ① (uid,type) 必须同时参与匹配 —— 同一 uid 的 api/doc 是两条独立墓碑；
//   ② 移除后的剩余列表顺序不变、格式仍是新格式；空列表时不留下空壳文件。

import (
	"os"
	"strings"
	"testing"
)

// qa21Open 新建临时集合。
func qa21Open(t *testing.T) *Collection {
	t.Helper()
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// qa21File 待删列表文件路径。
func qa21File(c *Collection) string {
	return c.Dir + string(os.PathSeparator) + pendingDeletesFile
}

// TestQA21RemoveDocLeavesApiOfSameUID 同一 uid 的 api / doc 两条墓碑：摘 doc 不得动 api。
func TestQA21RemoveDocLeavesApiOfSameUID(t *testing.T) {
	c := qa21Open(t)
	if err := c.SavePendingDeletes([]PendingDelete{
		{UID: "same", Type: PendingDeleteAPI},
		{UID: "same", Type: PendingDeleteDoc},
	}); err != nil {
		t.Fatalf("SavePendingDeletes: %v", err)
	}

	if err := c.RemovePendingDeletes([]PendingDelete{{UID: "same", Type: PendingDeleteDoc}}); err != nil {
		t.Fatalf("RemovePendingDeletes: %v", err)
	}

	got := c.LoadPendingDeletes()
	if len(got) != 1 || got[0].UID != "same" || got[0].Type != PendingDeleteAPI {
		t.Fatalf("摘 doc 后应只剩 {same,api}，got %+v", got)
	}
	// 文件仍在（还剩一条），且不是被整文件删掉的粗暴路径。
	if _, err := os.Stat(qa21File(c)); err != nil {
		t.Fatalf("还有剩余条目时文件不应消失，stat err=%v", err)
	}

	// 再摘 api：列表清空 → 文件消失。
	if err := c.RemovePendingDeletes([]PendingDelete{{UID: "same", Type: PendingDeleteAPI}}); err != nil {
		t.Fatalf("RemovePendingDeletes(api): %v", err)
	}
	if _, err := os.Stat(qa21File(c)); !os.IsNotExist(err) {
		t.Fatalf("全部摘除后文件应消失，stat err=%v", err)
	}
}

// TestQA21RemoveKeepsOrderAndFormat 摘中间若干条：剩余保持原顺序，文件仍是对象数组（新格式）。
func TestQA21RemoveKeepsOrderAndFormat(t *testing.T) {
	c := qa21Open(t)
	seed := []PendingDelete{
		{UID: "k1", Type: PendingDeleteAPI},
		{UID: "k2", Type: PendingDeleteDoc},
		{UID: "k3", Type: PendingDeleteAPI},
		{UID: "k4", Type: PendingDeleteDoc},
	}
	if err := c.SavePendingDeletes(seed); err != nil {
		t.Fatalf("SavePendingDeletes: %v", err)
	}

	if err := c.RemovePendingDeletes([]PendingDelete{
		{UID: "k2", Type: PendingDeleteDoc},
		{UID: "k3", Type: PendingDeleteAPI},
	}); err != nil {
		t.Fatalf("RemovePendingDeletes: %v", err)
	}

	got := c.LoadPendingDeletes()
	if len(got) != 2 {
		t.Fatalf("应剩 2 条，got %+v", got)
	}
	if got[0].UID != "k1" || got[1].UID != "k4" {
		t.Fatalf("剩余条目必须保持原标题顺序 k1,k4，got %+v", got)
	}

	raw, err := os.ReadFile(qa21File(c))
	if err != nil {
		t.Fatalf("读待删列表: %v", err)
	}
	// 新格式：对象数组（含 "uid"/"type" 字段），不能退化成旧格式字符串数组。
	if !strings.Contains(string(raw), `"uid"`) || !strings.Contains(string(raw), `"type"`) {
		t.Fatalf("重写后应保持新格式（对象数组），got %s", raw)
	}
}

// TestQA21RemoveRepeatedAndEmptyListIsNoop 重复摘同一条 / 文件本不存在：都不应报错、不应造出文件。
func TestQA21RemoveRepeatedAndEmptyListIsNoop(t *testing.T) {
	c := qa21Open(t)

	// 文件不存在时摘任意条：无副作用，且不得凭空创建待删列表文件。
	if err := c.RemovePendingDeletes([]PendingDelete{{UID: "ghost", Type: PendingDeleteAPI}}); err != nil {
		t.Fatalf("RemovePendingDeletes(文件不存在): %v", err)
	}
	if _, err := os.Stat(qa21File(c)); !os.IsNotExist(err) {
		t.Fatalf("文件本不存在时不应被创建，stat err=%v", err)
	}

	if err := c.SavePendingDeletes([]PendingDelete{{UID: "a", Type: PendingDeleteAPI}}); err != nil {
		t.Fatalf("SavePendingDeletes: %v", err)
	}
	done := []PendingDelete{{UID: "a", Type: PendingDeleteAPI}}
	if err := c.RemovePendingDeletes(done); err != nil {
		t.Fatalf("RemovePendingDeletes(第一次): %v", err)
	}
	// 第二次同样入参（幂等：条目已经不在了）：不得报错，也不得复活文件。
	if err := c.RemovePendingDeletes(done); err != nil {
		t.Fatalf("RemovePendingDeletes(第二次): %v", err)
	}
	if list := c.LoadPendingDeletes(); len(list) != 0 {
		t.Fatalf("重复摘除后列表应为空，got %+v", list)
	}
	if _, err := os.Stat(qa21File(c)); !os.IsNotExist(err) {
		t.Fatalf("列表空后不应留下文件，stat err=%v", err)
	}
}
