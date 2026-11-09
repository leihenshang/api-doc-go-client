package collection

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestOpenMissingDirDoesNotCreate 回归：打开一个不存在的路径，绝不能把目录建出来。
//
// 背景：集合目录被改名 / 移动后，客户端启动时会拿 localStorage 里的旧绝对路径去 Open。
// 旧实现无条件 MkdirAll，于是旧名字的空目录被复活，用户以为集合「自己长回来了」。
func TestOpenMissingDirDoesNotCreate(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "renamed-away")

	c, err := Open(target)
	if c != nil {
		t.Fatalf("目录不存在却返回了集合：%+v", c)
	}
	if !errors.Is(err, ErrDirMissing) {
		t.Fatalf("期望 ErrDirMissing，实际 %v", err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("Open 不该创建目录，但 %s 已存在（stat err=%v）", target, statErr)
	}
}

// TestOpenMissingDirKeepsParentClean 父目录里不能留下任何痕迹（含临时文件）。
func TestOpenMissingDirKeepsParentClean(t *testing.T) {
	base := t.TempDir()
	if _, err := Open(filepath.Join(base, "gone", "nested")); !errors.Is(err, ErrDirMissing) {
		t.Fatalf("期望 ErrDirMissing，实际 %v", err)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("父目录应保持空，实际有 %d 项", len(entries))
	}
}

// TestInitStillCreatesDir Init 是「建集合」的入口，必须继续能创建目录并写清单。
func TestInitStillCreatesDir(t *testing.T) {
	target := filepath.Join(t.TempDir(), "brand-new")
	c, err := Init(target)
	if err != nil {
		t.Fatalf("Init 应创建目录：%v", err)
	}
	defer c.StopWatch()
	if _, err := os.Stat(filepath.Join(target, "opencollection.yml")); err != nil {
		t.Fatalf("Init 应写入清单：%v", err)
	}
	if c.Name != "brand-new" {
		t.Fatalf("集合名应取目录基名，实际 %q", c.Name)
	}
}

// TestOpenEmptyDirNeedsInitNotMissing 区分两种错误：目录在、缺清单 = ErrNeedsInit（可确认初始化）。
func TestOpenEmptyDirNeedsInitNotMissing(t *testing.T) {
	dir := t.TempDir()
	if _, err := Open(dir); !errors.Is(err, ErrNeedsInit) {
		t.Fatalf("空目录应返回 ErrNeedsInit，实际 %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "opencollection.yml")); !os.IsNotExist(err) {
		t.Fatalf("Open 不该写清单，stat err=%v", err)
	}
}
