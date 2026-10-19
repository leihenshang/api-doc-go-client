package collection

// P2-1：墓碑改为「按回执逐条清」（RemovePendingDeletes）。
//
// 缺陷原状：推送链路只在「删除类操作全部成功」（allOk）时调用 ClearPendingDeletes 整文件删除，
// 于是同批只要有一条墓碑被服务端永久拒绝，整个列表就永远清不掉；下一轮会把同批已成功的
// 删除也换新 op_id 再发一遍，服务端按 op_id 的幂等去重失效 → 已成功的删除被无限重推。
//
// 本文件锁 RemovePendingDeletes 的四面：
//   ① 只摘指定的 (uid,type)，其余原样保留且顺序不变；
//   ② 全部摘光时文件消失（与 ClearPendingDeletes 一致，不留 "[]" 空壳）；
//   ③ 摘不存在的条目时列表一个字都不变（也不白写盘）；
//   ④ 同一 uid 的 api/doc 两条互不牵连（类型必须参与匹配）。

import (
	"os"
	"testing"
)

// p21Open 打开一个临时集合（复用 P1-10 的 helper，测试结束关闭）。
func p21Open(t *testing.T) *Collection {
	t.Helper()
	return p110Open(t)
}

// p21Seed 写入一份待删列表并断言写成功。
func p21Seed(t *testing.T, c *Collection, list []PendingDelete) {
	t.Helper()
	if err := c.SavePendingDeletes(list); err != nil {
		t.Fatalf("SavePendingDeletes: %v", err)
	}
}

// p21AssertList 断言待删列表逐条等于 want（顺序敏感）。
func p21AssertList(t *testing.T, c *Collection, want []PendingDelete) {
	t.Helper()
	got := c.LoadPendingDeletes()
	if len(got) != len(want) {
		t.Fatalf("待删列表应有 %d 条，got %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 条应为 %+v，got %+v（顺序与内容都必须一致）", i, want[i], got[i])
		}
	}
}

// p21AssertFileGone 断言 .sync-deletes.json 已不存在。
func p21AssertFileGone(t *testing.T, c *Collection) {
	t.Helper()
	if _, err := os.Stat(p110PendingPath(c)); !os.IsNotExist(err) {
		t.Fatalf("待删列表文件应被删除，stat err=%v", err)
	}
}

// TestP21RemovePendingDeletesKeepsOrder 只摘掉成功的那条 (uid,type)，其余原样保留且顺序不变。
func TestP21RemovePendingDeletesKeepsOrder(t *testing.T) {
	c := p21Open(t)
	seed := []PendingDelete{
		{UID: "uid-a", Type: PendingDeleteDoc},
		{UID: "uid-b", Type: PendingDeleteAPI},
		{UID: "uid-c", Type: PendingDeleteDoc},
		{UID: "uid-d", Type: PendingDeleteAPI},
	}
	p21Seed(t, c, seed)

	// 摘中间一条：剩下的必须保持 uid-a / uid-c / uid-d 的原顺序，不能被重排。
	if err := c.RemovePendingDeletes([]PendingDelete{{UID: "uid-b", Type: PendingDeleteAPI}}); err != nil {
		t.Fatalf("RemovePendingDeletes: %v", err)
	}
	p21AssertList(t, c, []PendingDelete{
		{UID: "uid-a", Type: PendingDeleteDoc},
		{UID: "uid-c", Type: PendingDeleteDoc},
		{UID: "uid-d", Type: PendingDeleteAPI},
	})

	// 一次摘两条（含首尾），仍要保序。
	if err := c.RemovePendingDeletes([]PendingDelete{
		{UID: "uid-a", Type: PendingDeleteDoc},
		{UID: "uid-d", Type: PendingDeleteAPI},
	}); err != nil {
		t.Fatalf("RemovePendingDeletes(两条): %v", err)
	}
	p21AssertList(t, c, []PendingDelete{{UID: "uid-c", Type: PendingDeleteDoc}})
}

// TestP21RemovePendingDeletesAllRemovedDeletesFile 全部摘光时文件消失（不留 "[]" 空壳）。
func TestP21RemovePendingDeletesAllRemovedDeletesFile(t *testing.T) {
	c := p21Open(t)
	p21Seed(t, c, []PendingDelete{
		{UID: "uid-a", Type: PendingDeleteAPI},
		{UID: "uid-b", Type: PendingDeleteDoc},
	})
	if _, err := os.Stat(p110PendingPath(c)); err != nil {
		t.Fatalf("前置条件：种子文件应存在，stat err=%v", err)
	}

	if err := c.RemovePendingDeletes([]PendingDelete{
		{UID: "uid-a", Type: PendingDeleteAPI},
		{UID: "uid-b", Type: PendingDeleteDoc},
	}); err != nil {
		t.Fatalf("RemovePendingDeletes: %v", err)
	}
	if list := c.LoadPendingDeletes(); len(list) != 0 {
		t.Fatalf("全部摘除后列表应为空，got %+v", list)
	}
	p21AssertFileGone(t, c)
}

// TestP21RemovePendingDeletesIgnoresUnknown 摘不存在的条目：列表一个字都不变。
func TestP21RemovePendingDeletesIgnoresUnknown(t *testing.T) {
	c := p21Open(t)
	seed := []PendingDelete{
		{UID: "uid-a", Type: PendingDeleteAPI},
		{UID: "uid-b", Type: PendingDeleteDoc},
	}
	p21Seed(t, c, seed)

	// uid-x 不在列表里；uid-a 类型不对（列出的是 doc，文件里是 api）；空 uid 直接忽略。
	if err := c.RemovePendingDeletes([]PendingDelete{
		{UID: "uid-x", Type: PendingDeleteAPI},
		{UID: "uid-a", Type: PendingDeleteDoc},
		{UID: "", Type: PendingDeleteAPI},
	}); err != nil {
		t.Fatalf("RemovePendingDeletes: %v", err)
	}
	p21AssertList(t, c, seed)

	// done 为空 / nil：直接返回，同样不动文件。
	if err := c.RemovePendingDeletes(nil); err != nil {
		t.Fatalf("RemovePendingDeletes(nil): %v", err)
	}
	if err := c.RemovePendingDeletes([]PendingDelete{}); err != nil {
		t.Fatalf("RemovePendingDeletes(空切片): %v", err)
	}
	p21AssertList(t, c, seed)
}

// TestP21RemovePendingDeletesMatchesByType 同一 uid 的 api / doc 是两条独立墓碑，互不牵连。
func TestP21RemovePendingDeletesMatchesByType(t *testing.T) {
	c := p21Open(t)
	p21Seed(t, c, []PendingDelete{
		{UID: "same-uid", Type: PendingDeleteAPI},
		{UID: "same-uid", Type: PendingDeleteDoc},
	})

	// 只摘 api 那条：doc 那条必须留下（否则一次成功会把另一类删除也弄丢）。
	if err := c.RemovePendingDeletes([]PendingDelete{{UID: "same-uid", Type: PendingDeleteAPI}}); err != nil {
		t.Fatalf("RemovePendingDeletes: %v", err)
	}
	p21AssertList(t, c, []PendingDelete{{UID: "same-uid", Type: PendingDeleteDoc}})

	// 再摘 doc 那条：列表空了，文件也应消失。
	if err := c.RemovePendingDeletes([]PendingDelete{{UID: "same-uid", Type: PendingDeleteDoc}}); err != nil {
		t.Fatalf("RemovePendingDeletes(doc): %v", err)
	}
	p21AssertFileGone(t, c)
}

// TestP21RemovePendingDeletesTypeEmptyDefaultsToAPI 文件里缺 type 时按 api 口径匹配
// （与 PendingDelete.UnmarshalJSON 的兼容口径一致）。
func TestP21RemovePendingDeletesTypeEmptyDefaultsToAPI(t *testing.T) {
	c := p21Open(t)
	p21Seed(t, c, []PendingDelete{{UID: "uid-a", Type: PendingDeleteAPI}})

	// 传进来的条目没写 type → 视作 api，应能摘掉文件里那条 api 墓碑。
	if err := c.RemovePendingDeletes([]PendingDelete{{UID: "uid-a"}}); err != nil {
		t.Fatalf("RemovePendingDeletes: %v", err)
	}
	p21AssertFileGone(t, c)
}
