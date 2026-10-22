package collection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFieldMappingsFileRoundTrip 字段字典单文件：增删改 + 排序 + 空表删文件。
func TestFieldMappingsFileRoundTrip(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// 文件不存在时返回空列表而不是报错（新集合 / 未同步过）。
	if items, err := c.ListFieldMappings(); err != nil || len(items) != 0 {
		t.Fatalf("空集合应返回空列表，got %+v (err=%v)", items, err)
	}

	// 新增两条（写入顺序与字段序无关，落盘应按 Field 排序）。
	if _, err := c.UpsertFieldMapping(FieldMapping{Field: "data.name", Type: "string"}); err != nil {
		t.Fatalf("新增 name: %v", err)
	}
	if _, err := c.UpsertFieldMapping(FieldMapping{Field: "data.id", Type: "integer", Description: "主键"}); err != nil {
		t.Fatalf("新增 id: %v", err)
	}
	items, err := c.ListFieldMappings()
	if err != nil {
		t.Fatalf("ListFieldMappings: %v", err)
	}
	if len(items) != 2 || items[0].Field != "data.id" || items[1].Field != "data.name" {
		t.Fatalf("应按字段路径排序落盘，got %+v", items)
	}
	for _, it := range items {
		if it.UID == "" {
			t.Fatalf("新增条目必须分配 uid，got %+v", it)
		}
	}

	// 同 uid 更新就地覆盖；同名字段换 uid 必须拒绝（项目内 field 唯一）。
	updated, err := c.UpsertFieldMapping(FieldMapping{UID: items[0].UID, Field: "data.id", Type: "integer", Description: "主键ID"})
	if err != nil || updated.Description != "主键ID" {
		t.Fatalf("同 uid 应更新，got %+v (err=%v)", updated, err)
	}
	if _, err := c.UpsertFieldMapping(FieldMapping{Field: "data.id", Type: "string"}); err == nil {
		t.Fatal("同名字段应拒绝新增")
	}

	// 校验：字段/类型必填，超长说明拒绝。
	if _, err := c.UpsertFieldMapping(FieldMapping{Field: "", Type: "string"}); err == nil {
		t.Fatal("空字段路径应被拒绝")
	}
	if _, err := c.UpsertFieldMapping(FieldMapping{Field: "x", Type: ""}); err == nil {
		t.Fatal("空类型应被拒绝")
	}
	if _, err := c.UpsertFieldMapping(FieldMapping{Field: "x", Type: "string",
		Description: strings.Repeat("长", fieldMappingDescMax+1)}); err == nil {
		t.Fatal("超长说明应被拒绝")
	}

	// 删除：命中移除，未命中报 errNotFound。
	items, _ = c.ListFieldMappings()
	if err := c.DeleteFieldMapping(items[0].UID); err != nil {
		t.Fatalf("DeleteFieldMapping: %v", err)
	}
	if err := c.DeleteFieldMapping("not-exist"); err == nil {
		t.Fatal("删除不存在的条目应报错")
	}
	items, _ = c.ListFieldMappings()
	if len(items) != 1 || items[0].Field != "data.name" {
		t.Fatalf("删除后应只剩 data.name，got %+v", items)
	}

	// 删空后文件应移除（避免仓库里留一个空壳）。
	if err := c.DeleteFieldMapping(items[0].UID); err != nil {
		t.Fatalf("DeleteFieldMapping: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, fieldMappingsFile)); !os.IsNotExist(err) {
		t.Fatalf("空表应删除 %s，stat err=%v", fieldMappingsFile, err)
	}
}

// TestFieldMappingHashStable 摘要只随内容变化：uid/字段/类型/说明任一变动都应换值。
func TestFieldMappingHashStable(t *testing.T) {
	base := FieldMapping{UID: "u1", Field: "data.id", Type: "integer", Description: "主键"}
	h := FieldMappingHash(base)
	if h == "" || h != FieldMappingHash(base) {
		t.Fatal("相同内容摘要必须稳定")
	}
	for _, changed := range []FieldMapping{
		{UID: "u2", Field: base.Field, Type: base.Type, Description: base.Description},
		{UID: base.UID, Field: "data.name", Type: base.Type, Description: base.Description},
		{UID: base.UID, Field: base.Field, Type: "string", Description: base.Description},
		{UID: base.UID, Field: base.Field, Type: base.Type, Description: "新说明"},
	} {
		if FieldMappingHash(changed) == h {
			t.Fatalf("内容变化后摘要必须变化，got %+v", changed)
		}
	}
}
