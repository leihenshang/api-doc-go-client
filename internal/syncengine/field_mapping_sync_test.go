package syncengine

// 字段字典（field_mapping）同步回归：
//   ① 服务端快照/增量下发 → 落单文件 field_mappings.yml，并记条目摘要基线；
//   ② 本地脏检测按「条目摘要」而非文件 hash：改一条只推一条；
//   ③ 手工编辑缺 uid 的条目会被自愈补号（否则永远推不出去）；
//   ④ 本地删除 → delete op，服务端删除 → 本地条目移除且清基线；
//   ⑤ 同名字段换 uid 时以服务端为准，旧 uid 基线一并清掉。

import (
	"encoding/json"
	"testing"

	"api-doc-go-client/internal/collection"
)

// TestFieldMappingSnapshotApplyAndDelete 快照落盘 + 增量删除。
func TestFieldMappingSnapshotApplyAndDelete(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 10}
	f.snapshot = Snapshot{
		Cursor: 10,
		Items: []Item{
			{Type: "field_mapping", UID: "fm1", Op: "upsert", ItemRev: 1,
				Payload: payloadOf(t, Payload{Field: "data.list[].id", FieldType: "integer", Description: "主键"})},
			{Type: "field_mapping", UID: "fm2", Op: "upsert", ItemRev: 2,
				Payload: payloadOf(t, Payload{Field: "data.name", FieldType: "string"})},
		},
	}
	c, e := newBoundEngine(t, cli)

	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	if rep.Pulled != 2 || len(rep.Errors) != 0 {
		t.Fatalf("2 条字段字典应全部应用成功，got %+v", rep)
	}
	items, err := c.ListFieldMappings()
	if err != nil {
		t.Fatalf("ListFieldMappings: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("应落盘 2 条字段字典，got %+v", items)
	}
	for _, it := range items {
		if it.UID == "fm1" && (it.Type != "integer" || it.Description != "主键") {
			t.Fatalf("fm1 内容不符: %+v", it)
		}
	}
	// 基线必须记下来，否则下一轮会把刚拉下来的内容当本地脏数据推回去。
	if e.Bind.FieldMappingHashes["fm1"] == "" || e.Bind.FieldMappingHashes["fm2"] == "" {
		t.Fatalf("条目摘要基线未记录: %+v", e.Bind.FieldMappingHashes)
	}

	// 第二轮：服务端删除 fm1，本地必须移除并清基线。
	f.state = State{Cursor: 11}
	e.Bind.Cursor = 10
	f.changes = Changes{Cursor: 11, Items: []Item{
		{Type: "field_mapping", UID: "fm1", Op: "delete", Deleted: true, ItemRev: 3},
	}}
	if _, err := e.RunOne(); err != nil {
		t.Fatalf("RunOne(delete): %v", err)
	}
	items, err = c.ListFieldMappings()
	if err != nil {
		t.Fatalf("ListFieldMappings: %v", err)
	}
	if len(items) != 1 || items[0].UID != "fm2" {
		t.Fatalf("fm1 应已删除、只留 fm2，got %+v", items)
	}
	if _, ok := e.Bind.FieldMappingHashes["fm1"]; ok {
		t.Fatalf("删除后应清掉 fm1 基线，got %+v", e.Bind.FieldMappingHashes)
	}
}

// TestFieldMappingCollectOpsUidHealAndDirty 本地脏检测与 uid 自愈。
func TestFieldMappingCollectOpsUidHealAndDirty(t *testing.T) {
	c, err := collection.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := c.SaveFieldMappings([]collection.FieldMapping{
		{UID: "fm-known", Field: "data.id", Type: "integer"},
		{Field: "data.name", Type: "string"}, // 手工编辑新增：没有 uid
	}); err != nil {
		t.Fatalf("SaveFieldMappings: %v", err)
	}
	e := &Engine{Coll: c, Bind: BindInfo{}}

	b := newPushBatch(8)
	e.collectFieldMappingOps(b)
	if len(b.ops) != 2 {
		t.Fatalf("两条都应推送 upsert，got %+v", b.ops)
	}
	for _, op := range b.ops {
		if op.Type != "field_mapping" || op.Op != "upsert" || op.UID == "" {
			t.Fatalf("应推送带 uid 的 field_mapping upsert，got %+v", op)
		}
	}
	// 自愈结果必须落盘，否则下一轮又变成「缺 uid」。
	items, err := c.ListFieldMappings()
	if err != nil {
		t.Fatalf("ListFieldMappings: %v", err)
	}
	for _, it := range items {
		if it.UID == "" {
			t.Fatalf("缺 uid 的条目应被补号并落盘，got %+v", items)
		}
	}

	// 模拟推送全部成功：基线写入后本轮应无 op。
	for _, op := range b.ops {
		m := b.meta[op.OpID]
		e.applyPushed(m, OpResult{OpID: op.OpID, Status: "ok"})
	}
	b = newPushBatch(8)
	e.collectFieldMappingOps(b)
	if len(b.ops) != 0 {
		t.Fatalf("内容未变化不应再推送，got %+v", b.ops)
	}

	// 只改一条描述：应只推这一条（条目摘要脏检测的意义）。
	items, _ = c.ListFieldMappings()
	for i := range items {
		if items[i].Field == "data.id" {
			items[i].Description = "主键"
		}
	}
	if err := c.SaveFieldMappings(items); err != nil {
		t.Fatalf("SaveFieldMappings: %v", err)
	}
	b = newPushBatch(8)
	e.collectFieldMappingOps(b)
	if len(b.ops) != 1 {
		t.Fatalf("改一条应只推一条，got %+v", b.ops)
	}
	var p Payload
	if err := json.Unmarshal(b.ops[0].Payload, &p); err != nil || p.Field != "data.id" || p.Description != "主键" {
		t.Fatalf("载荷不符: %s (err=%v)", b.ops[0].Payload, err)
	}

	// 本地删除另一条：应出 delete op。
	target := ""
	for _, it := range items {
		if it.Field == "data.name" {
			target = it.UID
		}
	}
	if err := c.DeleteFieldMapping(target); err != nil {
		t.Fatalf("DeleteFieldMapping: %v", err)
	}
	b = newPushBatch(8)
	e.collectFieldMappingOps(b)
	var deletes int
	for _, op := range b.ops {
		if op.Op == "delete" && op.Type == "field_mapping" && op.UID == target {
			deletes++
		}
	}
	if deletes != 1 {
		t.Fatalf("本地删除应恰好产生 1 条 delete op，got %+v", b.ops)
	}
}

// TestFieldMappingApplyReplacesSameField 同名不同 uid：以服务端为准并清旧基线。
func TestFieldMappingApplyReplacesSameField(t *testing.T) {
	c, err := collection.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	local := collection.FieldMapping{UID: "local-uid", Field: "data.name", Type: "string", Description: "旧说明"}
	if err := c.SaveFieldMappings([]collection.FieldMapping{local}); err != nil {
		t.Fatalf("SaveFieldMappings: %v", err)
	}
	e := &Engine{Coll: c, Bind: BindInfo{
		FieldMappingHashes: map[string]string{"local-uid": collection.FieldMappingHash(local)},
	}}

	err = e.applyFieldMapping("server-uid", Payload{Field: "data.name", FieldType: "string", Description: "新说明"})
	if err != nil {
		t.Fatalf("applyFieldMapping: %v", err)
	}
	items, err := c.ListFieldMappings()
	if err != nil {
		t.Fatalf("ListFieldMappings: %v", err)
	}
	if len(items) != 1 || items[0].UID != "server-uid" || items[0].Description != "新说明" {
		t.Fatalf("同名字段应被服务端条目替换，got %+v", items)
	}
	if _, ok := e.Bind.FieldMappingHashes["local-uid"]; ok {
		t.Fatalf("旧 uid 基线应清掉（否则下轮会反推 delete），got %+v", e.Bind.FieldMappingHashes)
	}
	if e.Bind.FieldMappingHashes["server-uid"] != collection.FieldMappingHash(items[0]) {
		t.Fatalf("应记录服务端条目摘要，got %+v", e.Bind.FieldMappingHashes)
	}

	// 空字段载荷是坏数据：直接忽略，不得清掉本地条目。
	if err := e.applyFieldMapping("bad-uid", Payload{}); err != nil {
		t.Fatalf("空载荷应忽略而不是报错: %v", err)
	}
	items, _ = c.ListFieldMappings()
	if len(items) != 1 {
		t.Fatalf("空载荷不应改动本地，got %+v", items)
	}
}
