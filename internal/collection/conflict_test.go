package collection

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"api-doc-go-client/internal/index"
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

// 构造一个带服务器快照的冲突副本（模拟 push 冲突：原文件 = 本地版本）。
func seedConflictCopy(t *testing.T, c *Collection) (uid, file string) {
	t.Helper()
	// 注入临时索引库：单测不依赖用户配置目录（ensureIndex 默认落在 AppData）
	idx, err := index.Open(filepath.Join(c.Dir, ".test-index.sqlite"))
	if err != nil {
		t.Fatalf("开测试索引: %v", err)
	}
	c.idx = idx
	t.Cleanup(func() { _ = idx.Close() })

	r, err := c.CreateRequest("", "users", "GET")
	if err != nil {
		t.Fatalf("建请求: %v", err)
	}
	r.URL = "{{host}}/users"
	if err := c.SaveRequest(r); err != nil {
		t.Fatalf("保存本地版本: %v", err)
	}
	serverPayload := mustJSON(t, map[string]any{
		"name":   "用户列表",
		"method": "GET",
		"url":    "{{host}}/members",
		"extra":  string(mustJSON(t, map[string]any{"desc": "服务端的说明"})),
	})
	full, err := c.SaveConflictCopy(r, 7, serverPayload)
	if err != nil {
		t.Fatalf("存冲突副本: %v", err)
	}
	return r.UID, filepath.Base(full)
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	return b
}

// ConflictDetail 应只挑出有差异的字段（name/url/docs），method 相同不出现。
func TestConflictDetailDiffs(t *testing.T) {
	c := newTestCollection(t)
	_, file := seedConflictCopy(t, c)

	d, err := c.ConflictDetail(file)
	if err != nil {
		t.Fatalf("ConflictDetail: %v", err)
	}
	if !d.HasPayload {
		t.Fatalf("应识别到服务器快照")
	}
	got := map[string]FieldDiff{}
	for _, df := range d.Diffs {
		got[df.Field] = df
	}
	if _, ok := got["method"]; ok {
		t.Fatalf("method 两边都是 GET，不应出现")
	}
	n, ok := got["name"]
	if !ok || n.Local != "users" || n.Server != "用户列表" {
		t.Fatalf("name 差异不对: %+v", got["name"])
	}
	u, ok := got["url"]
	if !ok || u.Local != "{{host}}/users" || u.Server != "{{host}}/members" {
		t.Fatalf("url 差异不对: %+v", got["url"])
	}
	ds, ok := got["docs"]
	if !ok || ds.Local != "" || ds.Server != "服务端的说明" {
		t.Fatalf("docs 差异不对: %+v", got["docs"])
	}
}

// 选本地：副本内容整份还原，base_rev 前移到 server_rev；副本消失。
func TestResolveConflictLocal(t *testing.T) {
	c := newTestCollection(t)
	uid, file := seedConflictCopy(t, c)

	if err := c.ResolveConflict(file, "local"); err != nil {
		t.Fatalf("ResolveConflict(local): %v", err)
	}
	got, err := c.ReadRequest(uid)
	if err != nil {
		t.Fatalf("读原请求: %v", err)
	}
	if got.Name != "users" || got.URL != "{{host}}/users" || got.Docs != "" {
		t.Fatalf("本地版本未还原: %+v", got)
	}
	if got.BaseRev != 7 {
		t.Fatalf("base_rev 应前移到 7，得到 %d", got.BaseRev)
	}
	if list, _ := c.ListConflicts(); len(list) != 0 {
		t.Fatalf("副本应已移除，剩 %d 份", len(list))
	}
}

// 选服务器：标量字段按快照改写并固化为已同步（不再 dirty）。
func TestResolveConflictRemote(t *testing.T) {
	c := newTestCollection(t)
	uid, file := seedConflictCopy(t, c)

	if err := c.ResolveConflict(file, "remote"); err != nil {
		t.Fatalf("ResolveConflict(remote): %v", err)
	}
	got, err := c.ReadRequest(uid)
	if err != nil {
		t.Fatalf("读原请求: %v", err)
	}
	if got.Name != "用户列表" || got.URL != "{{host}}/members" || got.Docs != "服务端的说明" {
		t.Fatalf("服务器版本未应用: %+v", got)
	}
	if got.BaseRev != 7 {
		t.Fatalf("base_rev 应为 7，得到 %d", got.BaseRev)
	}
	// MarkSynced 固化：索引 hash 与磁盘一致，下一轮不 dirty
	if c.NodeHash(uid) != c.FileHashOf(got.Path) {
		t.Fatalf("应已固化为已同步")
	}
}

// 旧版本副本（无服务器快照）：只能保留本地，不能选服务器。
func TestResolveConflictLegacyNoPayload(t *testing.T) {
	c := newTestCollection(t)
	r, err := c.CreateRequest("", "users", "GET")
	if err != nil {
		t.Fatalf("建请求: %v", err)
	}
	if err := c.SaveRequest(r); err != nil {
		t.Fatalf("保存: %v", err)
	}
	full, err := c.SaveConflictCopy(r, 9, nil)
	if err != nil {
		t.Fatalf("存副本: %v", err)
	}
	file := filepath.Base(full)

	d, err := c.ConflictDetail(file)
	if err != nil {
		t.Fatalf("ConflictDetail: %v", err)
	}
	if d.HasPayload || len(d.Diffs) != 0 {
		t.Fatalf("旧副本应无快照无差异: %+v", d)
	}
	if err := c.ResolveConflict(file, "remote"); err == nil {
		t.Fatalf("缺少服务器快照时采用服务器应报错")
	}
	if err := c.ResolveConflict(file, "local"); err != nil {
		t.Fatalf("保留本地应成功: %v", err)
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
