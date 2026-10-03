package collection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 保存冲突检测（P0）：客户端带「读到文件时的内容哈希」来保存，
// 磁盘已被外部改动（别的编辑器 / 另一个客户端窗口 / 同步）时必须拒写，不能静默覆盖。
func TestSaveRequestConflict(t *testing.T) {
	c := newTestCollection(t)
	r, err := c.CreateRequest("", "ping", "GET")
	if err != nil {
		t.Fatalf("建请求: %v", err)
	}
	full := filepath.Join(c.Dir, r.Path)

	// 首次保存（未带 ExpectHash → 不做冲突检测，写入成功）
	if err := c.SaveRequest(r); err != nil {
		t.Fatalf("首次保存: %v", err)
	}
	base := fileHash(full)
	if base == "" {
		t.Fatalf("写盘后应能取到内容哈希")
	}

	// 外部改动：直接改磁盘上的文件（绕过本客户端）
	if err := os.WriteFile(full, []byte("info:\n    name: ping\n    type: http\nmeta:\n    uid: external\nhttp:\n    method: GET\n"), 0o644); err != nil {
		t.Fatalf("模拟外部改动: %v", err)
	}

	// 带旧 hash 保存 → 必须拒写，错误以 [conflict] 开头
	r.URL = "{{host}}/local"
	r.ExpectHash = base
	err = c.SaveRequest(r)
	if err == nil {
		t.Fatalf("外部改过之后保存应被拒绝")
	}
	if !strings.Contains(err.Error(), conflictMarker) {
		t.Fatalf("错误应带冲突标记，得到: %v", err)
	}
	onDisk, _ := os.ReadFile(full)
	if strings.Contains(string(onDisk), "{{host}}/local") {
		t.Fatalf("拒写后磁盘不应被覆盖")
	}

	// 带当前（外部）hash 保存 → 允许写入（视为已合并）
	r.ExpectHash = fileHash(full)
	if err := c.SaveRequest(r); err != nil {
		t.Fatalf("带正确 hash 保存应成功: %v", err)
	}

	// 写盘后索引里的 hash 应同步为新值（前端下一次比对才不会假冲突）
	if c.idx != nil {
		t.Skip("本用例未启用索引")
	}
}

// 正常连续保存（不带 ExpectHash）不受影响；写盘后索引 hash 会跟着更新（若索引已启用）。
func TestSaveRequestUpdatesIndexHash(t *testing.T) {
	c := newTestCollection(t)
	r, err := c.CreateRequest("", "ping", "GET")
	if err != nil {
		t.Fatalf("建请求: %v", err)
	}
	if err := c.SaveRequest(r); err != nil {
		t.Fatalf("保存: %v", err)
	}
	if c.idx == nil {
		return // 未启用索引的环境跳过
	}
	n, ok, err := c.idx.Get(r.UID)
	if err != nil || !ok {
		t.Fatalf("索引里应有该请求: ok=%v err=%v", ok, err)
	}
	full := filepath.Join(c.Dir, r.Path)
	if n.Hash != fileHash(full) {
		t.Fatalf("索引 hash 应与磁盘一致")
	}
	r.URL = "{{host}}/v2"
	if err := c.SaveRequest(r); err != nil {
		t.Fatalf("二次保存: %v", err)
	}
	n2, _, _ := c.idx.Get(r.UID)
	if n2.Hash == n.Hash {
		t.Fatalf("再次保存后索引 hash 应更新")
	}
}

func TestHashMatchesPrefix(t *testing.T) {
	full := "fa837cf3dfc9abcdef0123456789abcdef0123456789abcdef0123456789"
	cases := []struct {
		name, actual, expect string
		want                 bool
	}{
		{"全等", full, full, true},
		{"12 位前缀（界面/MCP 展示的就是它）", full, full[:12], true},
		{"8 位前缀（下限）", full, full[:8], true},
		{"太短的前缀视为不可信", full, full[:4], false},
		{"前缀不匹配", full, "0123456789abcdef", false},
		{"空期望=不检查", full, "", true},
		{"比实际更长", full, full + "00", false},
	}
	for _, c := range cases {
		if got := hashMatches(c.actual, c.expect); got != c.want {
			t.Errorf("%s: hashMatches(%.12s…, %.12s…)=%v want %v", c.name, c.actual, c.expect, got, c.want)
		}
	}
}
