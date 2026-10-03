package collection

import (
	"os"
	"path/filepath"
	"testing"
)

// 外部/手工建的目录（没有 folder.yml）在集合树里同样显示为分组，
// 所以它必须能作为拖动目标：MoveRequest 要收下并补出 folder.yml。
func TestMoveRequestIntoDirWithoutFolderYML(t *testing.T) {
	c := newTestCollection(t)
	r, err := c.CreateRequest("", "ping", "GET")
	if err != nil {
		t.Fatalf("建请求: %v", err)
	}
	// 手工造一个只有目录、没有 folder.yml 的分组
	if err := os.MkdirAll(filepath.Join(c.Dir, "api", "user"), 0o755); err != nil {
		t.Fatalf("建目录: %v", err)
	}

	if err := c.MoveRequest(r.UID, "api/user"); err != nil {
		t.Fatalf("移动到无 folder.yml 的目录: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "api", "user", "ping.yml")); err != nil {
		t.Fatalf("请求未落盘到目标分组: %v", err)
	}
	// 目标分组应被补齐分组身份（否则下次重命名/移动它又会失败）
	if _, err := os.Stat(filepath.Join(c.Dir, "api", "user", "folder.yml")); err != nil {
		t.Fatalf("未补齐目标分组的 folder.yml: %v", err)
	}

	// 不存在的目录仍要拒绝（不能凭一个路径就凭空建分组）
	if err := c.MoveRequest(r.UID, "api/nope"); err == nil {
		t.Fatalf("不存在的目标分组应被拒绝")
	}
}

// 没有 folder.yml 的分组要有一个稳定 uid（"dir:<路径>"），否则界面上的移动/重命名会因 uid 为空而失败。
func TestMoveFolderWithFallbackUID(t *testing.T) {
	c := newTestCollection(t)
	if err := os.MkdirAll(filepath.Join(c.Dir, "api"), 0o755); err != nil {
		t.Fatalf("建目录: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(c.Dir, "admin"), 0o755); err != nil {
		t.Fatalf("建目录: %v", err)
	}
	// 手工写请求文件（不能用 CreateRequest —— 它会给分组补 folder.yml，就造不出「无 manifest 的目录」了）
	body := "info:\n    name: health\n    type: http\n    seq: 1\nmeta:\n    uid: 8b0d1b0e-1111-4222-8333-444455556666\nhttp:\n    method: GET\n    url: 'http://x/health'\n"
	if err := os.WriteFile(filepath.Join(c.Dir, "admin", "health.yml"), []byte(body), 0o644); err != nil {
		t.Fatalf("写请求文件: %v", err)
	}

	// 树里分组节点用的就是兜底 uid
	tree, err := c.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	var uid string
	for _, n := range tree {
		if n.Type == "folder" && n.Path == "admin" {
			uid = n.UID
		}
	}
	if uid != folderFallbackPrefix+"admin" {
		t.Fatalf("无 folder.yml 的分组 uid = %q，期望 %q", uid, folderFallbackPrefix+"admin")
	}

	// 用它移动分组：admin → api
	if err := c.MoveFolder(uid, "api"); err != nil {
		t.Fatalf("移动分组（兜底 uid）: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "api", "admin", "health.yml")); err != nil {
		t.Fatalf("分组未移动到位: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "admin")); !os.IsNotExist(err) {
		t.Fatalf("原分组目录应已不在: %v", err)
	}

	// 目标不能是自己或后代（拖动时前端也会拦，后端必须兜住）
	if err := c.MoveFolder(folderFallbackPrefix+"api", "api/admin"); err == nil {
		t.Fatalf("移入自己的子分组应被拒绝")
	}
}
