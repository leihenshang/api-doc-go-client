package index

import (
	"path/filepath"
	"testing"
)

func TestRebuildSearchDelete(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "idx.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	nodes := []Node{
		{UID: "f1", Type: "folder", Path: "用户", Title: "用户管理"},
		{UID: "r1", Type: "request", Path: "用户/list.yml", Title: "用户-列表", Method: "GET", URL: "{{host}}/api/user/list"},
		{UID: "r2", Type: "request", Path: "user/create.yml", Title: "创建用户", Method: "POST", URL: "{{host}}/api/user"},
	}
	if err := db.Rebuild(nodes); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if n, _ := db.Count(); n != 3 {
		t.Fatalf("Count = %d", n)
	}

	// 标题搜索
	got, err := db.Search("用户", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) < 2 {
		t.Fatalf("搜索「用户」应命中 ≥2，实际 %d: %+v", len(got), got)
	}

	// 方法搜索
	got, _ = db.Search("POST", 0)
	if len(got) != 1 || got[0].UID != "r2" {
		t.Fatalf("方法搜索: %+v", got)
	}

	// limit
	got, _ = db.Search("用户", 1)
	if len(got) != 1 {
		t.Fatalf("limit 未生效: %d", len(got))
	}

	// Upsert + Get
	if err := db.Upsert(Node{UID: "r1", Type: "request", Path: "用户/list.yml", Title: "列表改名", Method: "GET"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	n, ok, err := db.Get("r1")
	if err != nil || !ok || n.Title != "列表改名" {
		t.Fatalf("Get: %+v ok=%v err=%v", n, ok, err)
	}

	// Delete + 重建可恢复
	if err := db.Delete("r2"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if n, _ := db.Count(); n != 2 {
		t.Fatalf("删除后 Count = %d", n)
	}
	if err := db.Rebuild(nodes); err != nil {
		t.Fatalf("二次 Rebuild: %v", err)
	}
	if n, _ := db.Count(); n != 3 {
		t.Fatalf("重建后 Count = %d", n)
	}
}
