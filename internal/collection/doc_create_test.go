package collection

// CreateDocEx 的冲突策略（MCP create_doc.onConflict）：
// error（默认）必须可被调用方识别；suffix 保留历史行为；overwrite 复用 uid/path；
// uid 幂等创建（同 uid 再建按策略处理）。

import (
	"errors"
	"strings"
	"testing"
)

func TestCreateDocConflictModes(t *testing.T) {
	c := newTestCollection(t)

	first, err := c.CreateDocEx("同名", "", "docs", DocConflictError)
	if err != nil {
		t.Fatalf("首次创建失败: %v", err)
	}

	// ① 默认 error：报 ErrDocConflict，并带出已有 uid/path（调用方据此改用 update/uid）
	_, err = c.CreateDocEx("同名", "", "docs", "")
	if !errors.Is(err, ErrDocConflict) {
		t.Fatalf("应返回 ErrDocConflict，实际 %v", err)
	}
	if !strings.Contains(err.Error(), first.UID) || !strings.Contains(err.Error(), first.Path) {
		t.Errorf("冲突错误应带已有 uid/path：%v", err)
	}

	// ② suffix：保留历史行为（-2.md），name 不变、path 变
	second, err := c.CreateDocEx("同名", "", "docs", DocConflictSuffix)
	if err != nil {
		t.Fatalf("suffix 创建失败: %v", err)
	}
	if second.Path == first.Path || !strings.HasSuffix(second.Path, "-2.md") || second.Name != "同名" {
		t.Errorf("suffix 应生成 -2.md 且 name 不变：%+v", second)
	}

	// ③ overwrite：复用已有条目的 uid/path（正文由调用方写入）
	same, err := c.CreateDocEx("同名", "", "docs", DocConflictOverwrite)
	if err != nil {
		t.Fatalf("overwrite 失败: %v", err)
	}
	if same.UID != first.UID || same.Path != first.Path {
		t.Errorf("overwrite 应复用首篇的 uid/path：%+v vs %+v", same, first)
	}

	// ④ uid 幂等：同 uid 再建 → error；overwrite 复用
	fixed, err := c.CreateDocEx("固定", "uid-fixed-0001", "docs", DocConflictError)
	if err != nil {
		t.Fatalf("uid 创建失败: %v", err)
	}
	if fixed.UID != "uid-fixed-0001" {
		t.Fatalf("应使用指定 uid：%+v", fixed)
	}
	if _, err := c.CreateDocEx("固定", "uid-fixed-0001", "docs", DocConflictError); !errors.Is(err, ErrDocConflict) {
		t.Errorf("同 uid 应冲突：%v", err)
	}
	if again, err := c.CreateDocEx("固定", "uid-fixed-0001", "docs", DocConflictOverwrite); err != nil || again.UID != fixed.UID {
		t.Errorf("同 uid overwrite 应复用：%+v %v", again, err)
	}

	// ⑤ 未知策略 → 直接报错（不落盘）
	if _, err := c.CreateDocEx("x", "", "docs", "merge"); err == nil {
		t.Errorf("未知 onConflict 应报错")
	}
}

func TestCreateDocSuffixStillDefaultForUI(t *testing.T) {
	c := newTestCollection(t)
	if _, err := c.CreateDoc("UI 新建", "", "docs"); err != nil {
		t.Fatal(err)
	}
	// 前端「新建文档」保持历史行为：重名自动加 -2，不打扰用户
	d, err := c.CreateDoc("UI 新建", "", "docs")
	if err != nil || !strings.HasSuffix(d.Path, "-2.md") {
		t.Errorf("CreateDoc 应保留 suffix 行为：%+v %v", d, err)
	}
}
