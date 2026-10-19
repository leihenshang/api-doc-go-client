package collection

// P1-10：待推送删除墓碑（.sync-deletes.json）带实体类型。
//
// 锁三面：
//   ① (uid, type) 去重：同一条目重复删只留一条，但同一 uid 的 api/doc 各自保留；
//   ② 旧格式（纯字符串数组 ["uid"]）反序列化后按 api 处理（升级不丢删除）；
//   ③ 新格式（对象数组）往返一致，且对象缺 type 时回落 api。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// p110Open 打开一个临时集合（真实入口），测试结束关闭。
func p110Open(t *testing.T) *Collection {
	t.Helper()
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// p110PendingPath 待删列表文件路径。
func p110PendingPath(c *Collection) string {
	return filepath.Join(c.Dir, pendingDeletesFile)
}

// TestRecordPendingDeleteDeduplicatesByUIDAndType (uid,type) 去重口径。
func TestRecordPendingDeleteDeduplicatesByUIDAndType(t *testing.T) {
	c := p110Open(t)

	c.recordPendingDelete("uid-a", PendingDeleteAPI)
	c.recordPendingDelete("uid-a", PendingDeleteAPI) // 同 (uid,type) 重复 → 不新增
	c.recordPendingDelete("uid-a", PendingDeleteDoc) // 同 uid 不同类型 → 各留一条
	c.recordPendingDelete("uid-b", PendingDeleteDoc)
	c.recordPendingDelete("", PendingDeleteAPI) // 空 uid 直接忽略

	got := c.LoadPendingDeletes()
	want := []PendingDelete{
		{UID: "uid-a", Type: PendingDeleteAPI},
		{UID: "uid-a", Type: PendingDeleteDoc},
		{UID: "uid-b", Type: PendingDeleteDoc},
	}
	if len(got) != len(want) {
		t.Fatalf("应有 %d 条墓碑，got %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 条墓碑不符：want %+v got %+v（全体 %+v）", i, want[i], got[i], got)
		}
	}

	// 空的类型也按 api 记（防御性回落，别写出 {uid, ""} 这种条目）
	c.recordPendingDelete("uid-c", "")
	if list := c.LoadPendingDeletes(); list[len(list)-1] != (PendingDelete{UID: "uid-c", Type: PendingDeleteAPI}) {
		t.Fatalf("type 为空应回落为 api，got %+v", list[len(list)-1])
	}
}

// TestLoadPendingDeletesLegacyStringFormat 旧格式 ["uid1","uid2"] 必须能读，且一律按 api。
func TestLoadPendingDeletesLegacyStringFormat(t *testing.T) {
	c := p110Open(t)
	legacy := "[\n  \"legacy-1\",\n  \"legacy-2\"\n]\n"
	if err := os.WriteFile(p110PendingPath(c), []byte(legacy), 0o644); err != nil {
		t.Fatalf("写旧格式文件: %v", err)
	}

	got := c.LoadPendingDeletes()
	if len(got) != 2 {
		t.Fatalf("旧格式应读出 2 条，got %d: %+v", len(got), got)
	}
	for i, want := range []string{"legacy-1", "legacy-2"} {
		if got[i].UID != want {
			t.Fatalf("第 %d 条 uid 应为 %q，got %+v", i, want, got[i])
		}
		if got[i].Type != PendingDeleteAPI {
			t.Fatalf("旧格式条目必须按 api 处理，got %+v", got[i])
		}
	}

	// 再写一次（新格式）必须能覆盖旧文件，不能出现新旧混读的残留
	if err := c.SavePendingDeletes(got); err != nil {
		t.Fatalf("SavePendingDeletes: %v", err)
	}
	if reread := c.LoadPendingDeletes(); len(reread) != 2 || reread[0] != got[0] || reread[1] != got[1] {
		t.Fatalf("旧格式读入再写出后应保持一致，got %+v", reread)
	}
}

// TestPendingDeletesNewFormatRoundTrip 新格式对象数组往返一致 + 文件里确实是对象不是字符串。
func TestPendingDeletesNewFormatRoundTrip(t *testing.T) {
	c := p110Open(t)
	want := []PendingDelete{
		{UID: "doc-1", Type: PendingDeleteDoc},
		{UID: "api-1", Type: PendingDeleteAPI},
		{UID: "doc-2", Type: PendingDeleteDoc},
	}
	if err := c.SavePendingDeletes(want); err != nil {
		t.Fatalf("SavePendingDeletes: %v", err)
	}

	raw, err := os.ReadFile(p110PendingPath(c))
	if err != nil {
		t.Fatalf("读 %s: %v", pendingDeletesFile, err)
	}
	// 落盘必须是 {uid,type} 对象：引擎与外部工具都按字段读，纯字符串会丢类型
	var probe []map[string]string
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("落盘内容不是对象数组：%v，内容 %s", err, raw)
	}
	if len(probe) != len(want) || probe[0]["type"] != PendingDeleteDoc {
		t.Fatalf("落盘结构不符：%s", raw)
	}

	got := c.LoadPendingDeletes()
	if len(got) != len(want) {
		t.Fatalf("往返后条数应为 %d，got %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 条往返不一致：want %+v got %+v", i, want[i], got[i])
		}
	}

	// 对象缺 type 字段（手写/外部工具生成）→ 回落 api，不能丢这条删除
	partial := "[\n  {\"uid\": \"only-uid\"}\n]\n"
	if err := os.WriteFile(p110PendingPath(c), []byte(partial), 0o644); err != nil {
		t.Fatalf("写缺字段文件: %v", err)
	}
	list := c.LoadPendingDeletes()
	if len(list) != 1 || list[0].UID != "only-uid" || list[0].Type != PendingDeleteAPI {
		t.Fatalf("缺 type 的对象应回落为 api，got %+v", list)
	}

	// 清空后文件应当不存在（沿用旧行为：空列表即无文件，避免空文件进版本库）
	if err := c.SavePendingDeletes(nil); err != nil {
		t.Fatalf("SavePendingDeletes(nil): %v", err)
	}
	if _, err := os.Stat(p110PendingPath(c)); !os.IsNotExist(err) {
		t.Fatalf("空列表应删除文件，err=%v", err)
	}
}
