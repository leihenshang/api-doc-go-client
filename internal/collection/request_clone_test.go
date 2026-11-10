package collection

import (
	"strings"
	"testing"
)

// newCloneFixture 建一个含 shop/ping 的集合（ping 带 URL / 头 / 文档，便于验证内容完整性）。
func newCloneFixture(t *testing.T) (*Collection, *Request) {
	t.Helper()
	c, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(c.StopWatch)
	if err := c.CreateFolder("", "shop"); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	src, err := c.CreateRequest("shop", "ping", "GET")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	src.URL = "{{host}}/ping?x=1"
	src.Docs = "健康检查"
	src.Summary = "探活"
	src.Headers = append(src.Headers, KV{Name: "X-Trace", Value: "1", Enabled: true})
	if err := c.SaveRequest(src); err != nil {
		t.Fatalf("SaveRequest: %v", err)
	}
	return c, src
}

// TestCloneRequestNameFolderAndIdentity 同层级 + 自动命名 + 身份全新。
func TestCloneRequestNameFolderAndIdentity(t *testing.T) {
	c, src := newCloneFixture(t)

	cp, err := c.CloneRequest(src.UID)
	if err != nil {
		t.Fatalf("CloneRequest: %v", err)
	}
	if cp.Name != "ping 副本1" {
		t.Fatalf("首个副本名应为 ping 副本1，实际 %q", cp.Name)
	}
	if folderOfPath(cp.Path) != folderOfPath(src.Path) {
		t.Fatalf("副本应落在同一分组：src=%s cp=%s", src.Path, cp.Path)
	}
	if cp.Path == src.Path || cp.UID == src.UID {
		t.Fatalf("副本必须换路径与 uid：path=%s uid=%s", cp.Path, cp.UID)
	}
	if cp.Seq == 0 {
		t.Fatalf("副本应分配 seq")
	}

	// 再复制一次：序号递增，且不影响第一个副本
	cp2, err := c.CloneRequest(src.UID)
	if err != nil {
		t.Fatalf("第二次 CloneRequest: %v", err)
	}
	if cp2.Name != "ping 副本2" {
		t.Fatalf("第二个副本名应为 ping 副本2，实际 %q", cp2.Name)
	}
	if cp2.Path == cp.Path {
		t.Fatalf("两个副本不能共用路径：%s", cp2.Path)
	}
}

// TestCloneRequestCopiesContent 参数 / 头 / 体 / 文档都随副本走，源请求不受影响。
func TestCloneRequestCopiesContent(t *testing.T) {
	c, src := newCloneFixture(t)

	cp, err := c.CloneRequest(src.UID)
	if err != nil {
		t.Fatalf("CloneRequest: %v", err)
	}
	got, err := c.ReadRequest(cp.UID)
	if err != nil {
		t.Fatalf("ReadRequest(副本): %v", err)
	}
	if got.Method != src.Method || got.URL != src.URL {
		t.Fatalf("方法/URL 未复制：%+v", got)
	}
	if got.Docs != src.Docs || got.Summary != src.Summary {
		t.Fatalf("文档/概述未复制：docs=%q summary=%q", got.Docs, got.Summary)
	}
	if len(got.Headers) != len(src.Headers) {
		t.Fatalf("头数量不一致：%d vs %d", len(got.Headers), len(src.Headers))
	}
	// 源请求保持原样
	again, err := c.ReadRequest(src.UID)
	if err != nil {
		t.Fatalf("ReadRequest(源): %v", err)
	}
	if again.Name != "ping" || again.Path != src.Path {
		t.Fatalf("源请求被改动：name=%q path=%q", again.Name, again.Path)
	}
}

// TestCloneRequestNameIsPerFolder 同名请求在别的分组不参与序号计算。
func TestCloneRequestNameIsPerFolder(t *testing.T) {
	c, src := newCloneFixture(t)
	if err := c.CreateFolder("", "other"); err != nil {
		t.Fatalf("CreateFolder(other): %v", err)
	}
	if _, err := c.CreateRequest("other", "ping 副本1", "GET"); err != nil {
		t.Fatalf("CreateRequest(other/ping 副本1): %v", err)
	}

	cp, err := c.CloneRequest(src.UID)
	if err != nil {
		t.Fatalf("CloneRequest: %v", err)
	}
	if cp.Name != "ping 副本1" {
		t.Fatalf("别的分组同名不该影响本层序号，实际 %q", cp.Name)
	}
	if !strings.HasPrefix(cp.Path, "shop/") {
		t.Fatalf("副本应落在 shop/ 下，实际 %s", cp.Path)
	}
}
