package collection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newExampleResponse() ExampleResponse {
	return ExampleResponse{
		Status: 200, Proto: "HTTP/1.1", TimeMS: 12, Size: 21,
		ContentType: "application/json",
		Headers:     []KV{{Name: "Content-Type", Value: "application/json", Enabled: true}},
		Body:        "{\n  \"ok\": true\n}",
	}
}

func TestSaveResponseExampleLandsUnderExamples(t *testing.T) {
	c := openTemp(t)
	if err := c.CreateFolder("", "用户"); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	r, err := c.CreateRequest("用户", "列表", "GET")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	ex, err := c.SaveResponseExample(r.UID, "成功", ExampleRequest{Method: "GET", URL: "{{host}}/users"}, newExampleResponse())
	if err != nil {
		t.Fatalf("SaveResponseExample: %v", err)
	}
	if want := "examples/用户/列表/成功.yml"; ex.Path != want {
		t.Fatalf("示例路径: got %q want %q", ex.Path, want)
	}
	if ex.UID == "" || ex.CreatedAt == 0 || ex.Name != "成功" || ex.RequestUID != r.UID {
		t.Fatalf("示例元信息异常: %+v", ex)
	}
	data, err := os.ReadFile(filepath.Join(c.Dir, filepath.FromSlash(ex.Path)))
	if err != nil {
		t.Fatalf("读取示例文件: %v", err)
	}
	for _, want := range []string{"type: response-example", "status: 200", "ok"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("示例文件缺少 %q:\n%s", want, data)
		}
	}

	// 示例目录不参与扫描：树里只有请求，不会被当成集合条目
	tree, err := c.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if len(tree) != 1 || len(tree[0].Children) != 1 || tree[0].Children[0].Type != "request" {
		t.Fatalf("示例污染了集合树: %+v", tree)
	}
}

func TestListResponseExamplesRoundTripAndDedup(t *testing.T) {
	c := openTemp(t)
	r, err := c.CreateRequest("", "回显", "POST")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	req := ExampleRequest{
		Method: "POST", URL: "{{host}}/echo",
		Headers: []KV{{Name: "Content-Type", Value: "application/json", Enabled: true}},
		Body:    Body{Type: "json", Raw: `{"a":1}`},
	}
	if _, err := c.SaveResponseExample(r.UID, "成功", req, newExampleResponse()); err != nil {
		t.Fatalf("第一次保存: %v", err)
	}
	dup, err := c.SaveResponseExample(r.UID, "成功", req, newExampleResponse())
	if err != nil {
		t.Fatalf("第二次保存: %v", err)
	}
	if dup.Name != "成功 (1)" {
		t.Fatalf("同名示例未去重: %q", dup.Name)
	}

	got, err := c.ListResponseExamples(r.UID)
	if err != nil {
		t.Fatalf("ListResponseExamples: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("示例数量: %d", len(got))
	}
	// 新 → 旧：后保存的排在前面
	if got[0].UID != dup.UID {
		t.Fatalf("排序应为新 → 旧: %+v", got)
	}
	last := got[1]
	if last.Request.Body.Type != "json" || last.Request.Body.Raw != `{"a":1}` || last.Request.Method != "POST" {
		t.Fatalf("请求快照 round-trip 失败: %+v", last.Request)
	}
	if last.Response.Body != newExampleResponse().Body || last.Response.Status != 200 {
		t.Fatalf("响应快照 round-trip 失败: %+v", last.Response)
	}
	if last.Response.Headers[0].Value != "application/json" {
		t.Fatalf("响应头 round-trip 失败: %+v", last.Response.Headers)
	}
}

func TestResponseExamplesEmptyAndGuards(t *testing.T) {
	c := openTemp(t)
	r, _ := c.CreateRequest("", "空示例", "GET")
	if list, err := c.ListResponseExamples(r.UID); err != nil || len(list) != 0 {
		t.Fatalf("未保存过示例应返回空列表: %v %+v", err, list)
	}
	if _, err := c.SaveResponseExample(r.UID, "", ExampleRequest{}, newExampleResponse()); err == nil {
		t.Fatalf("空名称应被拒绝")
	}
	if _, err := c.SaveResponseExample("不存在的uid", "x", ExampleRequest{}, newExampleResponse()); err == nil {
		t.Fatalf("未知请求应报错")
	}
	if err := c.DeleteResponseExample(r.UID, "不存在的uid"); err == nil {
		t.Fatalf("未知示例应报错")
	}
}

func TestDeleteResponseExampleMovesToTrash(t *testing.T) {
	c := openTemp(t)
	r, _ := c.CreateRequest("", "删除示例", "GET")
	ex, err := c.SaveResponseExample(r.UID, "待删", ExampleRequest{Method: "GET"}, newExampleResponse())
	if err != nil {
		t.Fatalf("保存: %v", err)
	}
	if err := c.DeleteResponseExample(r.UID, ex.UID); err != nil {
		t.Fatalf("删除: %v", err)
	}
	list, err := c.ListResponseExamples(r.UID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("删除后仍能列出: %+v", list)
	}
	entries, _ := os.ReadDir(filepath.Join(c.Dir, ".trash"))
	if len(entries) != 1 {
		t.Fatalf(".trash 应有 1 个文件: %d", len(entries))
	}
}
