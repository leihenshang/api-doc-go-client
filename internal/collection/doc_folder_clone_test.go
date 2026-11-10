package collection

import (
	"strings"
	"testing"
)

// TestCloneDocSameFolderAndNaming 文档复制：同目录、正文带过去、序号递增。
func TestCloneDocSameFolderAndNaming(t *testing.T) {
	c, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(c.StopWatch)
	if err := c.CreateFolder("", "shop"); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	src, err := c.CreateDoc("guide", "", "shop")
	if err != nil {
		t.Fatalf("CreateDoc: %v", err)
	}
	src.Content = "# 指南\n\n正文内容"
	if err := c.SaveDoc(src); err != nil {
		t.Fatalf("SaveDoc: %v", err)
	}

	cp, err := c.CloneDoc(src.UID)
	if err != nil {
		t.Fatalf("CloneDoc: %v", err)
	}
	if cp.Name != "guide 副本1" {
		t.Fatalf("副本名应为 guide 副本1，实际 %q", cp.Name)
	}
	if folderOfPath(cp.Path) != "shop" {
		t.Fatalf("副本应落在 shop/，实际 %s", cp.Path)
	}
	if cp.UID == src.UID || cp.Path == src.Path {
		t.Fatalf("副本必须换 uid 与路径：uid=%s path=%s", cp.UID, cp.Path)
	}
	got, err := c.ReadDoc(cp.UID)
	if err != nil {
		t.Fatalf("ReadDoc(副本): %v", err)
	}
	if got.Content != src.Content {
		t.Fatalf("正文未复制：%q", got.Content)
	}

	cp2, err := c.CloneDoc(src.UID)
	if err != nil {
		t.Fatalf("第二次 CloneDoc: %v", err)
	}
	if cp2.Name != "guide 副本2" {
		t.Fatalf("第二个副本名应为 guide 副本2，实际 %q", cp2.Name)
	}
}

// TestCloneFolderDeepCopy 分组复制：子分组 / 请求 / 文档全部带过来，uid 全新、名字不变。
func TestCloneFolderDeepCopy(t *testing.T) {
	c, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(c.StopWatch)
	if err := c.CreateFolder("", "shop"); err != nil {
		t.Fatalf("CreateFolder(shop): %v", err)
	}
	if err := c.CreateFolder("shop", "sub"); err != nil {
		t.Fatalf("CreateFolder(sub): %v", err)
	}
	shopUID, ok := c.folderUID("shop")
	if !ok {
		t.Fatal("shop 缺少 uid")
	}
	req, err := c.CreateRequest("shop", "ping", "GET")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	req.URL = "{{host}}/ping"
	if err := c.SaveRequest(req); err != nil {
		t.Fatalf("SaveRequest: %v", err)
	}
	subReq, err := c.CreateRequest("shop/sub", "deep", "POST")
	if err != nil {
		t.Fatalf("CreateRequest(sub): %v", err)
	}
	doc, err := c.CreateDoc("guide", "", "shop")
	if err != nil {
		t.Fatalf("CreateDoc: %v", err)
	}
	doc.Content = "# 指南"
	if err := c.SaveDoc(doc); err != nil {
		t.Fatalf("SaveDoc: %v", err)
	}

	name, err := c.CloneFolder(shopUID)
	if err != nil {
		t.Fatalf("CloneFolder: %v", err)
	}
	if name != "shop 副本1" {
		t.Fatalf("副本分组名应为 shop 副本1，实际 %q", name)
	}

	res, err := c.scan()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	dstDir := "shop 副本1"
	if newUID, ok := res.uidByDir[dstDir]; !ok || newUID == shopUID {
		t.Fatalf("副本分组应换新 uid，实际 %q", newUID)
	}
	if _, ok := res.uidByDir[dstDir+"/sub"]; !ok {
		t.Fatalf("子分组未复制：%v", res.uidByDir)
	}

	// 请求：两份同名，副本在副本目录下、uid 不同、内容一致
	var clonedReq *Request
	for _, r := range res.reqs {
		if strings.HasPrefix(r.Path, dstDir+"/") && r.Name == "ping" {
			clonedReq = r
		}
	}
	if clonedReq == nil {
		t.Fatal("副本里找不到 ping")
	}
	if clonedReq.UID == req.UID {
		t.Fatal("副本请求沿用了源 uid")
	}
	if clonedReq.URL != req.URL {
		t.Fatalf("副本 URL 未复制：%q", clonedReq.URL)
	}

	// 子分组里的请求也复制了（uid 全新）
	var deep *Request
	for _, r := range res.reqs {
		if strings.HasPrefix(r.Path, dstDir+"/sub/") && r.Name == "deep" {
			deep = r
		}
	}
	if deep == nil || deep.UID == subReq.UID {
		t.Fatalf("子分组请求未正确复制：%+v", deep)
	}

	// 文档：名字保持 guide，路径在副本目录下
	var clonedDoc *DocEntry
	for _, d := range res.docs {
		if strings.HasPrefix(d.Path, dstDir+"/") && d.Name == "guide" {
			clonedDoc = d
		}
	}
	if clonedDoc == nil || clonedDoc.UID == doc.UID {
		t.Fatalf("文档未正确复制：%+v", clonedDoc)
	}
	if clonedDoc.Content != doc.Content {
		t.Fatalf("文档正文未复制：%q", clonedDoc.Content)
	}

	// 源分组原样保留
	if _, ok := res.uidByDir["shop"]; !ok {
		t.Fatal("源分组被破坏")
	}
	if got, err := c.ReadRequest(req.UID); err != nil || got.Name != "ping" {
		t.Fatalf("源请求被破坏：%+v %v", got, err)
	}
}

// TestCloneFolderUnknownUID 未知 uid / 根分组（没有 uid）→ 明确报错，不建半成品目录。
func TestCloneFolderUnknownUID(t *testing.T) {
	c, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.StopWatch)
	if _, err := c.CloneFolder("no-such-uid"); err == nil {
		t.Fatal("未知 uid 应该报错")
	}
}
