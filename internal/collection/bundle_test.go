package collection

// 集合整包导出/导入（item 26）：导出的文件必须能被另一个集合完整还原
// —— 目录（含空目录与嵌套）、请求（参数/头/体/说明/响应字段文档）、文档条目正文。

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBundleRoundTrip(t *testing.T) {
	src := newTestCollection(t)
	src.Name, src.UID = "shop", "uid-shop"

	// 目录：嵌套 + 空目录
	if err := src.CreateFolder("", "用户"); err != nil {
		t.Fatalf("建目录: %v", err)
	}
	if err := src.CreateFolder("用户", "查询"); err != nil {
		t.Fatalf("建子目录: %v", err)
	}
	if err := src.CreateFolder("", "空目录"); err != nil {
		t.Fatalf("建空目录: %v", err)
	}

	// 请求：把几个容易在转换中丢掉的字段都填上
	r, err := src.CreateRequest("用户/查询", "用户-列表", "GET")
	if err != nil {
		t.Fatalf("建请求: %v", err)
	}
	r.URL = "{{host}}/api/users"
	r.Params = []KV{{Name: "page", Value: "1", Enabled: true, Description: "页码"}}
	r.Headers = []KV{{Name: "X-Token", Value: "t", Enabled: true}}
	r.Docs = "## 说明\n\n列表接口"
	r.RespDoc = &ResponseDoc{
		Type:   "object",
		Params: []ResponseParam{{FieldName: "id", Type: "int64", Description: "主键"}},
		Sample: `{"id":1}`,
	}
	if err := src.SaveRequest(r); err != nil {
		t.Fatalf("保存请求: %v", err)
	}

	// 文档条目（正文里有中文与 Markdown 结构）
	d, err := src.CreateDoc("接入指南", "", "docs")
	if err != nil {
		t.Fatalf("建文档: %v", err)
	}
	d.Content = "# 接入指南\n\n先申请 token。\n\n| 参数 | 说明 |\n| --- | --- |\n| id | 主键 |"
	if err := src.SaveDoc(d); err != nil {
		t.Fatalf("保存文档: %v", err)
	}

	data, err := src.ExportBundle()
	if err != nil {
		t.Fatalf("ExportBundle: %v", err)
	}
	// 包内结构：格式标识、版本、集合名、四个目录（用户 / 用户-查询 / 空目录 / docs）、1 请求、1 文档
	var b CollectionBundle
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatalf("导出结果不是合法 JSON: %v", err)
	}
	if b.Format != BundleFormat || b.Version != BundleVersion {
		t.Fatalf("包头异常: format=%q version=%d", b.Format, b.Version)
	}
	if b.Collection.Name != "shop" {
		t.Fatalf("集合名未带出: %+v", b.Collection)
	}
	if len(b.Folders) != 4 || len(b.Requests) != 1 || len(b.Docs) != 1 {
		t.Fatalf("包内条目数异常: folders=%d requests=%d docs=%d", len(b.Folders), len(b.Requests), len(b.Docs))
	}
	if strings.Contains(string(data), "expectHash") {
		t.Errorf("运行期字段 expectHash 不应出现在导出文件里")
	}

	// 导入到另一个空集合：应得到 shop/… 的完整结构
	dst := newTestCollection(t)
	dst.Name = "target"
	sum, err := dst.ImportBundle("", data)
	if err != nil {
		t.Fatalf("ImportBundle: %v", err)
	}
	if sum.Imported != 2 || sum.Skipped != 0 {
		t.Fatalf("导入统计异常: %+v", sum)
	}

	tree, err := dst.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	shop := findNode(tree, "folder", "shop")
	if shop == nil {
		t.Fatalf("没建出集合分组 shop: %+v", tree)
	}
	if findNode(shop.Children, "folder", "空目录") == nil {
		t.Errorf("空目录没有保留（目录信息不完整）")
	}
	users := findNode(shop.Children, "folder", "用户")
	if users == nil {
		t.Fatalf("缺少目录 用户")
	}
	query := findNode(users.Children, "folder", "查询")
	if query == nil {
		t.Fatalf("缺少嵌套目录 用户/查询")
	}
	if findNode(query.Children, "request", "用户-列表") == nil {
		t.Fatalf("请求没有落在 用户/查询 下: %+v", query.Children)
	}
	if findNode(shop.Children, "folder", "docs") == nil {
		t.Fatalf("文档所在目录 docs 缺失")
	}

	// 请求内容必须逐项还原（这些字段 Postman/OpenAPI 都表达不了）
	res, err := dst.scan()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	var gotReq *Request
	for _, x := range res.reqs {
		if x.Name == "用户-列表" {
			gotReq = x
			break
		}
	}
	if gotReq == nil {
		t.Fatalf("导入后找不到请求")
	}
	if gotReq.URL != "{{host}}/api/users" || gotReq.Method != "GET" {
		t.Errorf("请求行异常: %s %s", gotReq.Method, gotReq.URL)
	}
	if len(gotReq.Params) != 1 || gotReq.Params[0].Description != "页码" {
		t.Errorf("参数说明丢失: %+v", gotReq.Params)
	}
	if len(gotReq.Headers) != 1 || gotReq.Headers[0].Name != "X-Token" {
		t.Errorf("请求头丢失: %+v", gotReq.Headers)
	}
	if !strings.Contains(gotReq.Docs, "列表接口") {
		t.Errorf("接口说明丢失: %q", gotReq.Docs)
	}
	if gotReq.RespDoc == nil || gotReq.RespDoc.Type != "object" ||
		len(gotReq.RespDoc.Params) != 1 || gotReq.RespDoc.Params[0].FieldName != "id" {
		t.Errorf("响应字段文档丢失: %+v", gotReq.RespDoc)
	}
	if !strings.HasPrefix(gotReq.Path, "shop/用户/查询/") {
		t.Errorf("请求路径不对: %q", gotReq.Path)
	}

	// 文档正文必须原样带回
	var gotDoc *DocEntry
	for _, x := range res.docs {
		if x.Name == "接入指南" {
			gotDoc = x
			break
		}
	}
	if gotDoc == nil {
		t.Fatalf("导入后找不到文档条目")
	}
	if !strings.Contains(gotDoc.Content, "先申请 token") || !strings.Contains(gotDoc.Content, "| id | 主键 |") {
		t.Errorf("文档正文丢失: %q", gotDoc.Content)
	}
	if !strings.HasPrefix(gotDoc.Path, "shop/docs/") {
		t.Errorf("文档路径不对: %q", gotDoc.Path)
	}
}

func TestBundleImportGuards(t *testing.T) {
	c := newTestCollection(t)
	c.Name = "target"

	// 不是本格式
	if _, err := c.ImportBundle("", []byte(`{"items":[]}`)); err == nil ||
		!strings.Contains(err.Error(), "不是 api-doc-go 集合文件") {
		t.Errorf("非本格式应报错，得到 %v", err)
	}
	// 版本过高：明确提示升级，而不是半截导入
	hi := `{"format":"api-doc-go/collection","version":99,"collection":{"name":"x"},
	       "requests":[{"name":"r","method":"GET","url":"http://x","path":"r.yml"}]}`
	if _, err := c.ImportBundle("", []byte(hi)); err == nil || !strings.Contains(err.Error(), "版本") {
		t.Errorf("高版本应拒绝，得到 %v", err)
	}
	// 空包
	if _, err := c.ImportBundle("", []byte(`{"format":"api-doc-go/collection","version":1}`)); err == nil ||
		!strings.Contains(err.Error(), "没有内容") {
		t.Errorf("空包应报错，得到 %v", err)
	}
	// 坏 JSON
	if _, err := c.ImportBundle("", []byte(`{`)); err == nil {
		t.Error("坏 JSON 应报错")
	}
	// 越界路径：不得写到集合目录之外
	evil := `{"format":"api-doc-go/collection","version":1,"collection":{"name":"x"},
	         "requests":[{"name":"r","method":"GET","url":"http://x","path":"../../escape/r.yml"}]}`
	sum, err := c.ImportBundle("", []byte(evil))
	if err == nil && sum.Imported > 0 {
		t.Error("越界路径不应被接受")
	}
}

// findNode 在树里按类型+名称找节点（只找一层）。
func findNode(nodes []*Node, typ, name string) *Node {
	for _, n := range nodes {
		if n.Type == typ && n.Name == name {
			return n
		}
	}
	return nil
}
