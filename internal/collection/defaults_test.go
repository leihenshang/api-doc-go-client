package collection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	share "github.com/leihenshang/api-doc-go-share/collection"
)

// TestMergeHeaders 集合级请求头与请求头的合并语义。
func TestMergeHeaders(t *testing.T) {
	base := []KV{
		{Name: "X-Tenant", Value: "acme", Enabled: true},
		{Name: "x-legacy", Value: "off", Enabled: false},
		{Name: "", Value: "noname", Enabled: true},
	}
	req := []KV{
		{Name: "x-tenant", Value: "other", Enabled: true},
		{Name: "X-Legacy", Value: "on", Enabled: true},
		{Name: "X-Only-Request", Value: "1", Enabled: true},
	}
	got := mergeHeaders(base, req)
	if len(got) != 3 {
		t.Fatalf("合并后应有 3 行，实际 %d: %+v", len(got), got)
	}
	// 同名（大小写不敏感）由请求覆盖
	if got[0].Name != "x-tenant" || got[0].Value != "other" {
		t.Fatalf("同名请求头应覆盖集合级: %+v", got[0])
	}
	// 集合级 enabled=false 不加入；请求可补上同名行
	if got[1].Name != "X-Legacy" || got[1].Value != "on" {
		t.Fatalf("请求应能补上集合级禁用的同名头: %+v", got[1])
	}
	if got[2].Name != "X-Only-Request" {
		t.Fatalf("请求独有头应保留: %+v", got[2])
	}
}

// TestMergeDefaultsAuth 认证继承语义：inherit/未设置取集合默认，none 与显式类型以请求为准。
func TestMergeDefaultsAuth(t *testing.T) {
	base := &share.Defaults{Auth: &share.Auth{Type: "bearer", Token: "{{token}}"}}

	cases := []struct {
		name string
		auth *Auth
		want string
	}{
		{"nil 继承", nil, "bearer"},
		{"空类型继承", &Auth{Type: ""}, "bearer"},
		{"inherit 继承", &Auth{Type: "inherit"}, "bearer"},
		{"none 不继承", &Auth{Type: "none"}, "none"},
		{"basic 覆盖", &Auth{Type: "basic", Username: "u", Password: "p"}, "basic"},
	}
	for _, tc := range cases {
		req := &Request{Headers: nil, Auth: tc.auth}
		got := MergeDefaults(req, base)
		if got.Auth == nil || got.Auth.Type != tc.want {
			t.Fatalf("%s: auth = %+v, want type %q", tc.name, got.Auth, tc.want)
		}
	}
	// 不修改入参（编辑器草稿不能被发送过程污染）
	original := &Request{Auth: nil}
	_ = MergeDefaults(original, base)
	if original.Auth != nil {
		t.Fatalf("MergeDefaults 不应修改请求入参: %+v", original.Auth)
	}
}

// TestSetDefaultsPersists 公共参数落盘与清除：写入 defaults 段、读回一致、清空后段消失。
func TestSetDefaultsPersists(t *testing.T) {
	dir := t.TempDir()
	c := &Collection{Dir: dir, UID: "uid-1", Name: "demo"}
	d := &share.Defaults{
		Headers: []KV{
			{Name: "X-Tenant", Value: "{{tenant}}", Enabled: true},
			{Name: "", Value: "", Enabled: true}, // 空行应被清理
		},
		Auth: &Auth{Type: "bearer", Token: "{{token}}"},
	}
	if err := c.SetDefaults(d); err != nil {
		t.Fatalf("SetDefaults: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "opencollection.yml"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if !strings.Contains(string(data), "defaults:") || !strings.Contains(string(data), "X-Tenant") {
		t.Fatalf("清单应写出 defaults 段：\n%s", data)
	}
	back := c.Defaults()
	if back == nil || len(back.Headers) != 1 || back.Auth == nil || back.Auth.Token != "{{token}}" {
		t.Fatalf("Defaults 读回不符: %+v", back)
	}
	// 返回副本：改副本不影响集合
	back.Headers[0].Value = "changed"
	if c.Defaults().Headers[0].Value == "changed" {
		t.Fatal("Defaults 应返回副本")
	}

	if err := c.SetDefaults(nil); err != nil {
		t.Fatalf("clear defaults: %v", err)
	}
	data, err = os.ReadFile(filepath.Join(dir, "opencollection.yml"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if strings.Contains(string(data), "defaults:") {
		t.Fatalf("清空后不应保留 defaults 段：\n%s", data)
	}
}
