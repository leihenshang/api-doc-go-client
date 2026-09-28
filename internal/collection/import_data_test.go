package collection

import (
	"strings"
	"testing"
)

const postmanSample = `{
  "info": { "name": "演示集合" },
  "item": [
    {
      "name": "用户",
      "item": [
        {
          "name": "用户-列表",
          "request": {
            "method": "get",
            "url": "{{host}}/api/user/list?page=1",
            "header": [
              { "key": "Authorization", "value": "Bearer {{token}}" },
              { "key": "X-Skip", "value": "1", "disabled": true }
            ],
            "body": { "mode": "raw", "raw": "{\"name\":\"alice\"}" },
            "description": "查询用户"
          }
        }
      ]
    },
    { "name": "登录", "request": { "method": "POST", "url": "{{host}}/api/login" } },
    { "name": "缺地址", "request": { "method": "GET", "url": "" } }
  ]
}`

const openAPISample = `{
  "openapi": "3.0.0",
  "info": { "title": "demo", "version": "1.0" },
  "paths": {
    "/api/user/list": {
      "get": {
        "summary": "用户-列表",
        "tags": ["用户"],
        "parameters": [
          { "name": "page", "in": "query", "example": 1 },
          { "name": "id", "in": "path" }
        ]
      }
    },
    "/api/login": { "post": { "operationId": "login" } }
  }
}`

// findRequest 在树里按名称找请求节点。
func findRequest(nodes []*Node, name string) *Node {
	for _, n := range nodes {
		if n.Type == "request" && n.Name == name {
			return n
		}
		if found := findRequest(n.Children, name); found != nil {
			return found
		}
	}
	return nil
}

func TestImportPostman(t *testing.T) {
	c := &Collection{Dir: t.TempDir()}
	sum, err := c.ImportPostman("", []byte(postmanSample))
	if err != nil {
		t.Fatalf("ImportPostman: %v", err)
	}
	if sum.Imported != 2 || sum.Skipped != 0 || len(sum.Failures) != 1 {
		t.Fatalf("导入结果不符: %+v", sum)
	}
	if !strings.Contains(sum.Failures[0], "缺少 method 或 url") {
		t.Fatalf("失败明细不符: %+v", sum.Failures)
	}

	tree, err := c.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	// 目录进分组，请求落在对应分组下。
	found := false
	for _, n := range tree {
		if n.Type == "folder" && n.Name == "用户" {
			found = findRequest(n.Children, "用户-列表") != nil
		}
	}
	if !found {
		t.Fatalf("目录/请求结构不符: %+v", tree)
	}

	node := findRequest(tree, "用户-列表")
	r, err := c.ReadRequest(node.UID)
	if err != nil {
		t.Fatalf("ReadRequest: %v", err)
	}
	if r.Method != "GET" || r.URL != "{{host}}/api/user/list?page=1" {
		t.Fatalf("方法与地址不符: %+v", r)
	}
	if len(r.Headers) != 1 || r.Headers[0].Name != "Authorization" || !r.Headers[0].Enabled {
		t.Fatalf("请求头映射不符: %+v", r.Headers)
	}
	if len(r.Params) != 1 || r.Params[0].Name != "page" || r.Params[0].Value != "1" {
		t.Fatalf("查询参数映射不符: %+v", r.Params)
	}
	if r.Body.Type != "json" || r.Body.Raw != `{"name":"alice"}` {
		t.Fatalf("请求体映射不符: %+v", r.Body)
	}
	if r.Docs != "查询用户" {
		t.Fatalf("说明映射不符: %q", r.Docs)
	}
	if r.Seq == 0 || r.Path == "" {
		t.Fatalf("落盘字段缺失: %+v", r)
	}

	// 重复导入：同方法同地址的请求跳过。
	again, err := c.ImportPostman("", []byte(postmanSample))
	if err != nil {
		t.Fatalf("ImportPostman(重复): %v", err)
	}
	if again.Imported != 0 || again.Skipped != 2 {
		t.Fatalf("重复导入应全部跳过: %+v", again)
	}
}

func TestImportPostmanErrors(t *testing.T) {
	c := &Collection{Dir: t.TempDir()}
	if _, err := c.ImportPostman("", []byte("{oops")); err == nil {
		t.Fatal("非法 JSON 应报错")
	}
	if _, err := c.ImportPostman("", []byte(`{"info":{"name":"空"}}`)); err == nil {
		t.Fatal("item 为空应报错")
	}
}

func TestImportOpenAPI(t *testing.T) {
	c := &Collection{Dir: t.TempDir()}
	sum, err := c.ImportOpenAPI("", []byte(openAPISample))
	if err != nil {
		t.Fatalf("ImportOpenAPI: %v", err)
	}
	if sum.Imported != 2 || sum.Skipped != 0 || len(sum.Failures) != 0 {
		t.Fatalf("导入结果不符: %+v", sum)
	}

	tree, err := c.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	node := findRequest(tree, "用户-列表")
	if node == nil {
		t.Fatalf("tag 未生成分组或请求缺失: %+v", tree)
	}
	r, err := c.ReadRequest(node.UID)
	if err != nil {
		t.Fatalf("ReadRequest: %v", err)
	}
	// 相对路径补 {{host}}，query 参数进参数表，path 参数不进。
	if r.URL != "{{host}}/api/user/list" || r.Method != "GET" {
		t.Fatalf("地址/方法不符: %+v", r)
	}
	if len(r.Params) != 1 || r.Params[0].Name != "page" || r.Params[0].Value != "1" {
		t.Fatalf("参数映射不符: %+v", r.Params)
	}
	// 无 summary 时用 operationId 命名。
	if findRequest(tree, "login") == nil {
		t.Fatalf("operationId 命名未生效: %+v", tree)
	}

	again, err := c.ImportOpenAPI("", []byte(openAPISample))
	if err != nil {
		t.Fatalf("ImportOpenAPI(重复): %v", err)
	}
	if again.Imported != 0 || again.Skipped != 2 {
		t.Fatalf("重复导入应全部跳过: %+v", again)
	}
}

func TestImportOpenAPIErrors(t *testing.T) {
	c := &Collection{Dir: t.TempDir()}
	if _, err := c.ImportOpenAPI("", []byte("{oops")); err == nil {
		t.Fatal("非法 JSON 应报错")
	}
	if _, err := c.ImportOpenAPI("", []byte(`{"openapi":"3.0.0","paths":{}}`)); err == nil {
		t.Fatal("paths 为空应报错")
	}
}
