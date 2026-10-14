package collection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// item 20：删除 → 回收站可列出（带原路径索引）→ 恢复到原位并可读回。
func TestTrashListAndRestore(t *testing.T) {
	c := newTestCollection(t)
	r, err := c.CreateRequest("", "回收我", "GET")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteRequest(r.UID); err != nil {
		t.Fatal(err)
	}

	list, err := c.ListTrash()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("回收站应有 1 条: %+v", list)
	}
	e := list[0]
	if e.Kind != "request" || !strings.HasSuffix(e.Origin, ".yml") || e.DeletedAt == 0 {
		t.Fatalf("条目元数据不符: %+v", e)
	}

	rel, err := c.RestoreTrash(e.File, "")
	if err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if rel != e.Origin {
		t.Fatalf("应恢复到原位: %s vs %s", rel, e.Origin)
	}
	if _, err := c.ReadRequest(r.UID); err != nil {
		t.Fatalf("恢复后应能读回: %v", err)
	}
	list, _ = c.ListTrash()
	if len(list) != 0 {
		t.Fatalf("恢复后回收站应清空: %+v", list)
	}
}

// 旧条目（无索引）兜底列出；跨集合导入按相对路径落位且拒绝重名。
func TestTrashLegacyEntryAndCrossImport(t *testing.T) {
	c := newTestCollection(t)
	trash := filepath.Join(c.Dir, ".trash")
	if err := os.MkdirAll(trash, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(trash, "20260101-000000.old.yml")
	if err := os.WriteFile(src, []byte("x: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := c.ListTrash()
	if err != nil || len(list) != 1 {
		t.Fatalf("旧条目应被兜底列出: %+v %v", list, err)
	}
	if list[0].Name != "old.yml" || list[0].DeletedAt == 0 {
		t.Errorf("旧条目解析不符: %+v", list[0])
	}

	dst := newTestCollection(t)
	rel, err := dst.ImportFromAbs(src, "api/old.yml")
	if err != nil {
		t.Fatal(err)
	}
	if rel != "api/old.yml" {
		t.Fatalf("导入相对路径不符: %s", rel)
	}
	if _, err := os.Stat(filepath.Join(dst.Dir, "api", "old.yml")); err != nil {
		t.Fatalf("导入后文件未落位: %v", err)
	}
	if _, err := dst.ImportFromAbs(src, "api/old.yml"); err == nil {
		t.Error("目标重名应拒绝导入")
	}
	// 越界路径必须被拒绝
	if _, err := dst.ImportFromAbs(src, "../escape.yml"); err == nil {
		t.Error("越出集合根的路径应被拒绝")
	}
}
