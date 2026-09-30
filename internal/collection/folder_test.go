package collection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func newTestCollection(t *testing.T) *Collection {
	t.Helper()
	return &Collection{Dir: t.TempDir()}
}

func TestCreateFolderNestedAndGuards(t *testing.T) {
	c := newTestCollection(t)
	if err := c.CreateFolder("", "用户"); err != nil {
		t.Fatalf("建根分组: %v", err)
	}
	uid, ok := c.folderUID("用户")
	if !ok || uid == "" {
		t.Fatalf("分组缺少 uid")
	}
	if err := c.CreateFolder("用户", "管理"); err != nil {
		t.Fatalf("建子分组: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "用户", "管理", "folder.yml")); err != nil {
		t.Fatalf("子分组未落盘: %v", err)
	}
	if err := c.CreateFolder("../外部", "越界"); err == nil {
		t.Fatalf("越界父级应被拒绝")
	}
	if err := c.CreateFolder("不存在的分组", "x"); err == nil {
		t.Fatalf("父级不存在应被拒绝")
	}
	if err := c.CreateFolder("", "非法/名称"); err == nil {
		t.Fatalf("非法名称应被拒绝")
	}
}

func TestRenameFolderKeepsDirAndUID(t *testing.T) {
	c := newTestCollection(t)
	if err := c.CreateFolder("", "用户"); err != nil {
		t.Fatalf("建分组: %v", err)
	}
	uid, _ := c.folderUID("用户")
	if err := c.RenameFolder(uid, "用户中心"); err != nil {
		t.Fatalf("重命名: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "用户", "folder.yml")); err != nil {
		t.Fatalf("目录名不应变化: %v", err)
	}
	got, err := c.Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	found := false
	for _, n := range got.Tree {
		if n.Type == "folder" && n.UID == uid {
			found = n.Name == "用户中心"
		}
	}
	if !found {
		t.Fatalf("分组显示名未更新: %+v", got.Tree)
	}
	if again, _ := c.folderUID("用户"); again != uid {
		t.Fatalf("uid 不应变化: %s → %s", uid, again)
	}
}

func TestMoveRequestAndFolder(t *testing.T) {
	c := newTestCollection(t)
	if err := c.CreateFolder("", "A"); err != nil {
		t.Fatalf("建 A: %v", err)
	}
	if err := c.CreateFolder("", "B"); err != nil {
		t.Fatalf("建 B: %v", err)
	}
	if err := c.CreateFolder("A", "子"); err != nil {
		t.Fatalf("建子: %v", err)
	}
	r, err := c.CreateRequest("A", "ping", "GET")
	if err != nil {
		t.Fatalf("建请求: %v", err)
	}

	// 移动请求到 B：uid 不变、路径变
	if err := c.MoveRequest(r.UID, "B"); err != nil {
		t.Fatalf("移动请求: %v", err)
	}
	got, err := c.ReadRequest(r.UID)
	if err != nil {
		t.Fatalf("读请求: %v", err)
	}
	if got.UID != r.UID {
		t.Fatalf("uid 不应变化: %s → %s", r.UID, got.UID)
	}
	if !strings.HasPrefix(got.Path, "B/") {
		t.Fatalf("路径未更新: %s", got.Path)
	}

	// 移动分组 A 到 B 下
	aUID, _ := c.folderUID("A")
	if err := c.MoveFolder(aUID, "B"); err != nil {
		t.Fatalf("移动分组: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "B", "A", "子", "folder.yml")); err != nil {
		t.Fatalf("分组未移动: %v", err)
	}
	// 不能移入自己/后代
	if err := c.MoveFolder(aUID, "B/A/子"); err == nil {
		t.Fatalf("移入后代应被拒绝")
	}
}

func TestDeleteFolderRefusesNonEmpty(t *testing.T) {
	c := newTestCollection(t)
	if err := c.CreateFolder("", "空组"); err != nil {
		t.Fatalf("建分组: %v", err)
	}
	if err := c.CreateFolder("", "非空组"); err != nil {
		t.Fatalf("建分组: %v", err)
	}
	if _, err := c.CreateRequest("非空组", "接口", "GET"); err != nil {
		t.Fatalf("建请求: %v", err)
	}

	emptyUID, _ := c.folderUID("空组")
	if err := c.DeleteFolder(emptyUID); err != nil {
		t.Fatalf("删空分组: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "空组")); !os.IsNotExist(err) {
		t.Fatalf("空分组应被移走")
	}
	if _, err := os.Stat(filepath.Join(c.Dir, ".trash")); err != nil {
		t.Fatalf("应移入 .trash: %v", err)
	}

	busyUID, _ := c.folderUID("非空组")
	if err := c.DeleteFolder(busyUID); err == nil || !strings.Contains(err.Error(), "非空") {
		t.Fatalf("非空分组应拒绝删除，实际: %v", err)
	}
	if err := c.DeleteFolder("不存在的uid"); err == nil {
		t.Fatalf("未知 uid 应报错")
	}
}

func TestRenameRequestKeepsPathAndUnknownFields(t *testing.T) {
	c := newTestCollection(t)
	r, err := c.CreateRequest("", "旧名", "GET")
	if err != nil {
		t.Fatalf("建请求: %v", err)
	}
	// 手工塞入未知字段，验证重命名不会丢
	path := filepath.Join(c.Dir, filepath.FromSlash(r.Path))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读请求文件: %v", err)
	}
	withExtra := string(data) + "script:\n  pre-request: bru.setVar('a', 1)\n"
	if err := os.WriteFile(path, []byte(withExtra), 0o644); err != nil {
		t.Fatalf("写入: %v", err)
	}

	if err := c.RenameRequest(r.UID, "新名"); err != nil {
		t.Fatalf("重命名: %v", err)
	}
	got, err := c.ReadRequest(r.UID)
	if err != nil {
		t.Fatalf("读回: %v", err)
	}
	if got.Name != "新名" || got.Path != r.Path || got.UID != r.UID {
		t.Fatalf("名称/路径/uid 异常: %+v", got)
	}
	out, _ := os.ReadFile(path)
	if !strings.Contains(string(out), "bru.setVar") {
		t.Fatalf("未知字段丢失:\n%s", string(out))
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("输出不是合法 YAML: %v", err)
	}
}

func TestRenameRejectsBadName(t *testing.T) {
	c := newTestCollection(t)
	r, _ := c.CreateRequest("", "请求", "GET")
	if err := c.RenameRequest(r.UID, "带/斜杠"); err == nil {
		t.Fatalf("非法名称应被拒绝")
	}
	if err := c.RenameFolder("any", ""); err == nil {
		t.Fatalf("空名称应被拒绝")
	}
}
