package collection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 文档 / 示例 / 环境的整文件快照：能被列出、能看预览、能原样还原。
func TestSnapshotConflictListDetailResolve(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	d, err := c.CreateDoc("设计说明", "", "docs")
	if err != nil {
		t.Fatalf("CreateDoc: %v", err)
	}
	local := "---\nuid: " + d.UID + "\nname: 设计说明\n---\n\n本地未推送的内容\n"
	file, err := c.SaveConflictSnapshot(ConflictKindDoc, d.UID, d.Name, d.Path, []byte(local), "远端正文", 0)
	if err != nil {
		t.Fatalf("SaveConflictSnapshot: %v", err)
	}

	list, err := c.ListConflicts()
	if err != nil {
		t.Fatalf("ListConflicts: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("快照副本应被列出且不被当成请求副本，got %+v", list)
	}
	if list[0].Kind != ConflictKindDoc || list[0].OfUID != d.UID {
		t.Fatalf("副本元信息不对: %+v", list[0])
	}

	det, err := c.ConflictDetail(file)
	if err != nil {
		t.Fatalf("ConflictDetail: %v", err)
	}
	if det.Mode != ConflictModeSnapshot || !det.HasPayload {
		t.Fatalf("快照详情应走 snapshot 模式且允许「以远端为准」: %+v", det)
	}
	if !strings.Contains(det.Preview, "本地未推送的内容") || det.RemotePreview != "远端正文" {
		t.Fatalf("预览内容不对: %+v", det)
	}

	// 模拟远端版本已经落地（pull 覆盖后的磁盘状态），再「以本地为准」应原样还原。
	full := filepath.Join(c.Dir, filepath.FromSlash(d.Path))
	if err := os.WriteFile(full, []byte("远端覆盖后的内容\n"), 0o644); err != nil {
		t.Fatalf("写远端版本: %v", err)
	}
	if err := c.ResolveConflict(file, "local"); err != nil {
		t.Fatalf("ResolveConflict(local): %v", err)
	}
	got, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("读还原结果: %v", err)
	}
	if string(got) != local {
		t.Fatalf("本地版本未原样还原:\nwant %q\ngot  %q", local, string(got))
	}
	if _, err := os.Stat(filepath.Join(c.Dir, ".conflicts", file)); !os.IsNotExist(err) {
		t.Fatalf("处理后副本应移入 .trash，仍存在: %v", err)
	}
}

// 「以远端为准」= 丢弃副本，不改动已落地的远端内容。
func TestSnapshotConflictResolveRemoteKeepsFile(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	d, err := c.CreateDoc("快速开始", "", "docs")
	if err != nil {
		t.Fatalf("CreateDoc: %v", err)
	}
	file, err := c.SaveConflictSnapshot(ConflictKindDoc, d.UID, d.Name, d.Path, []byte("本地旧内容"), "", 0)
	if err != nil {
		t.Fatalf("SaveConflictSnapshot: %v", err)
	}
	remote := "远端新内容\n"
	full := filepath.Join(c.Dir, filepath.FromSlash(d.Path))
	if err := os.WriteFile(full, []byte(remote), 0o644); err != nil {
		t.Fatalf("写远端版本: %v", err)
	}
	if err := c.ResolveConflict(file, "remote"); err != nil {
		t.Fatalf("ResolveConflict(remote): %v", err)
	}
	got, _ := os.ReadFile(full)
	if string(got) != remote {
		t.Fatalf("以远端为准不应改写文件，got %q", string(got))
	}
}

// 副本里记录的原文件路径必须限制在集合内（副本可被手工编辑）。
func TestSnapshotConflictRejectsEscapingPath(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := c.SaveConflictSnapshot(ConflictKindDoc, "u1", "x", "../../etc/passwd", []byte("x"), "", 0); err == nil {
		t.Fatal("越界路径应被拒绝")
	}
}
