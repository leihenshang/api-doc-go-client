package collection

// 模块/集合级维护（MCP 容器工具）：路径→uid、改集合显示名、空模块清理。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFolderUIDByPathAndTraversal(t *testing.T) {
	c := newTestCollection(t)
	if err := c.CreateFolder("", "api"); err != nil {
		t.Fatal(err)
	}
	if err := c.CreateFolder("api", "user"); err != nil {
		t.Fatal(err)
	}
	uid, ok := c.FolderUIDByPath("api")
	if !ok || uid == "" {
		t.Fatalf("api 应能找到 uid：%q %v", uid, ok)
	}
	if _, ok := c.FolderUIDByPath("api/user"); !ok {
		t.Errorf("嵌套模块应能找到")
	}
	for _, bad := range []string{"", ".", "..", "../etc", "api/../../x"} {
		if _, ok := c.FolderUIDByPath(bad); ok {
			t.Errorf("%q 不应命中", bad)
		}
	}
}

func TestRenameCollection(t *testing.T) {
	c := newTestCollection(t)
	if err := os.WriteFile(filepath.Join(c.Dir, "opencollection.yml"),
		[]byte("opencollection: 1.0.0\ninfo:\n    name: 旧名\nmeta:\n    uid: uid-1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.RenameCollection("新 名"); err != nil {
		t.Fatalf("改名失败: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(c.Dir, "opencollection.yml"))
	if !strings.Contains(string(data), "新 名") || !strings.Contains(string(data), "uid-1") {
		t.Errorf("清单应更新 name 且保留 uid：\n%s", data)
	}
	if err := c.RenameCollection("   "); err == nil {
		t.Errorf("空名字应拒绝")
	}
}

func TestPruneEmptyFolders(t *testing.T) {
	c := newTestCollection(t)
	// 空的三级模块：全空的祖先链应一起回收
	if err := c.CreateFolder("", "空"); err != nil {
		t.Fatal(err)
	}
	if err := c.CreateFolder("空", "子"); err != nil {
		t.Fatal(err)
	}
	if err := c.CreateFolder("空", "子"); err != nil {
		// CreateFolder 只有 (parent,name)；这句其实是再建一个同名“子” → 忽略错误
	}
	// 非空模块：有请求的不能删
	if err := c.CreateFolder("", "有货"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateRequest("有货", "接口", "GET"); err != nil {
		t.Fatal(err)
	}
	// 保留目录：assets（即使只有 folder.yml 也不清）
	reserved := filepath.Join(c.Dir, "assets")
	if err := os.MkdirAll(reserved, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reserved, "folder.yml"), []byte("info:\n    name: assets\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := c.PruneEmptyFolders()
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if n != 2 { // 子 + 空（自底向上两层）
		t.Errorf("应删 2 个空模块，实际 %d", n)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "空")); !os.IsNotExist(err) {
		t.Errorf("空模块链应被删除")
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "有货", "folder.yml")); err != nil {
		t.Errorf("有内容的模块不能删：%v", err)
	}
	if _, err := os.Stat(filepath.Join(reserved, "folder.yml")); err != nil {
		t.Errorf("assets 不应被清理：%v", err)
	}
}
