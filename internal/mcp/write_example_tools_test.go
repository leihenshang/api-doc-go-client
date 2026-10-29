package mcp_test

// 响应示例三个写工具（save / update / delete）的端到端回归：
// 新增 → get_request_detail 能看到（name/status/body 字节）；
// 改名 → 磁盘文件名与 path 一起变（旧文件不残留）；
// 改 status/body → size 跟着变；删除 → 详情里消失且文件进 .trash。

import (
	. "api-doc-go-client/internal/mcp" //nolint:revive // 与其它测试一致：白盒式调用
	"os"
	"path/filepath"
	"strings"
	"testing"

	"api-doc-go-client/internal/collection"
)

// TestSaveResponseExampleTool save_response_example 落盘并能被详情读回。
func TestSaveResponseExampleTool(t *testing.T) {
	root, svc := setup(t)
	r, err := svc.CreateRequest(CreateRequestInput{
		Project: "demo", Folder: "api", Name: "ex-save", Method: "POST", URL: "http://127.0.0.1/ex",
	})
	if err != nil {
		t.Fatal(err)
	}

	ex, err := svc.SaveResponseExample(SaveResponseExampleInput{
		Project: "demo", UID: r.UID, Name: "外部日志样例", Status: 503,
		Headers: []collection.KV{{Name: "X-Trace", Value: "t-1", Enabled: true}},
		Body:    `{"code":503,"msg":"busy"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ex.UID == "" || ex.Name != "外部日志样例" || ex.Response.Status != 503 {
		t.Fatalf("示例返回值异常: %+v", ex)
	}
	if _, err := os.Stat(filepath.Join(root, "demo", ex.Path)); err != nil {
		t.Fatalf("示例文件未落盘: %v", err)
	}
	// contentType 空 → 按 body 首字符推断为 application/json；size 按字节数
	if ex.Response.ContentType != "application/json" {
		t.Errorf("contentType 应自动推断为 application/json，实际 %q", ex.Response.ContentType)
	}
	if ex.Response.Size != len(`{"code":503,"msg":"busy"}`) {
		t.Errorf("size 应为响应体字节数，实际 %d", ex.Response.Size)
	}
	// 请求快照来自落盘请求
	if ex.Request.Method != "POST" || ex.Request.URL != "http://127.0.0.1/ex" {
		t.Errorf("请求快照不符: %+v", ex.Request)
	}

	// 详情能看到（name / status / 字节数 / 响应体）
	d, err := svc.GetRequestDetail("demo", r.UID, DetailOptions{IncludeBody: true, MaxBodyBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Examples) != 1 {
		t.Fatalf("详情应带 1 条示例: %+v", d.Examples)
	}
	got := d.Examples[0]
	if got.UID != ex.UID || got.Name != "外部日志样例" || got.Status != 503 || got.BodyBytes != len(ex.Response.Body) {
		t.Errorf("详情里的示例信息不符: %+v", got)
	}
	if !strings.Contains(got.WithBody, `"busy"`) {
		t.Errorf("详情应带响应体原文: %q", got.WithBody)
	}

	// 默认参数：name 省略 = 「响应示例」、status 省略 = 200
	ex2, err := svc.SaveResponseExample(SaveResponseExampleInput{Project: "demo", UID: r.UID})
	if err != nil {
		t.Fatal(err)
	}
	if ex2.Name != "响应示例" || ex2.Response.Status != 200 {
		t.Errorf("默认 name/status 不符: %+v", ex2)
	}
	// 同名再加一条 → 自动加序号（不覆盖）
	ex3, err := svc.SaveResponseExample(SaveResponseExampleInput{Project: "demo", UID: r.UID})
	if err != nil {
		t.Fatal(err)
	}
	if ex3.Name != "响应示例 (1)" {
		t.Errorf("重名应自动加序号，实际 %q", ex3.Name)
	}
	// 缺少 uid 时应报错（AI 能读懂）
	if _, err := svc.SaveResponseExample(SaveResponseExampleInput{Project: "demo"}); err == nil {
		t.Error("缺 uid 应报错")
	}
}

// TestUpdateResponseExampleTool 改名同步改文件名、改 status/body 同步 size。
func TestUpdateResponseExampleTool(t *testing.T) {
	root, svc := setup(t)
	r, err := svc.CreateRequest(CreateRequestInput{
		Project: "demo", Name: "ex-update", Method: "GET", URL: "http://127.0.0.1/exu",
	})
	if err != nil {
		t.Fatal(err)
	}
	ex, err := svc.SaveResponseExample(SaveResponseExampleInput{
		Project: "demo", UID: r.UID, Name: "初始名", Status: 200, Body: `{"ok":true}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	oldPath := ex.Path
	oldAbs := filepath.Join(root, "demo", oldPath)
	if _, err := os.Stat(oldAbs); err != nil {
		t.Fatalf("旧文件应存在: %v", err)
	}

	// 改名 + 改状态码 + 改响应体
	up, err := svc.UpdateResponseExample(UpdateResponseExampleInput{
		Project: "demo", UID: r.UID, ExampleUID: ex.UID,
		Name: strPtr("外部系统超时"), Status: intPtr(504), Body: strPtr(`{"code":504}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if up.Name != "外部系统超时" || up.Response.Status != 504 || up.Response.Body != `{"code":504}` {
		t.Fatalf("更新未生效: %+v", up)
	}
	if up.Response.Size != len(`{"code":504}`) {
		t.Errorf("body 变了 size 应跟着变，实际 %d", up.Response.Size)
	}
	// 磁盘文件名与 path 一起变
	if up.Path == oldPath {
		t.Errorf("改名后 path 应变化，仍是 %s", up.Path)
	}
	if !strings.Contains(up.Path, "外部系统超时") {
		t.Errorf("path 应带上新名: %s", up.Path)
	}
	if _, err := os.Stat(filepath.Join(root, "demo", up.Path)); err != nil {
		t.Errorf("新文件应存在: %v", err)
	}
	if _, err := os.Stat(oldAbs); !os.IsNotExist(err) {
		t.Errorf("旧文件不应残留: %v", err)
	}

	// 不传的字段不动（只改 content_type）
	ct := "text/plain"
	up2, err := svc.UpdateResponseExample(UpdateResponseExampleInput{
		Project: "demo", UID: r.UID, ExampleUID: ex.UID, ContentType: &ct,
	})
	if err != nil {
		t.Fatal(err)
	}
	if up2.Response.Status != 504 || up2.Response.Body != `{"code":504}` || up2.Name != "外部系统超时" {
		t.Fatalf("只改 contentType 不应动其它字段: %+v", up2)
	}
	if up2.Response.ContentType != "text/plain" {
		t.Errorf("contentType 未生效: %q", up2.Response.ContentType)
	}

	// 找不到示例：报错（不静默成功）
	if _, err := svc.UpdateResponseExample(UpdateResponseExampleInput{
		Project: "demo", UID: r.UID, ExampleUID: "no-such-uid", Name: strPtr("x"),
	}); err == nil {
		t.Error("示例 uid 不存在应报错")
	}
	// 缺 exampleUid：报错并给出获取方式
	if _, err := svc.UpdateResponseExample(UpdateResponseExampleInput{Project: "demo", UID: r.UID}); err == nil ||
		!strings.Contains(err.Error(), "exampleUid") {
		t.Errorf("缺 exampleUid 应报错并提示获取方式，实际: %v", err)
	}
}

// TestDeleteResponseExampleTool 删除后详情里消失、文件进 .trash。
func TestDeleteResponseExampleTool(t *testing.T) {
	root, svc := setup(t)
	r, err := svc.CreateRequest(CreateRequestInput{
		Project: "demo", Name: "ex-delete", Method: "GET", URL: "http://127.0.0.1/exd",
	})
	if err != nil {
		t.Fatal(err)
	}
	keep, err := svc.SaveResponseExample(SaveResponseExampleInput{Project: "demo", UID: r.UID, Name: "留下"})
	if err != nil {
		t.Fatal(err)
	}
	gone, err := svc.SaveResponseExample(SaveResponseExampleInput{
		Project: "demo", UID: r.UID, Name: "删掉", Body: `{"x":1}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.DeleteResponseExample("demo", r.UID, gone.UID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "demo", gone.Path)); !os.IsNotExist(err) {
		t.Errorf("示例文件应被移走: %v", err)
	}
	ents, _ := os.ReadDir(filepath.Join(root, "demo", ".trash"))
	found := false
	for _, e := range ents {
		if strings.Contains(e.Name(), "删掉") {
			found = true
		}
	}
	if !found {
		t.Errorf("删除应进 .trash: %+v", ents)
	}

	d, err := svc.GetRequestDetail("demo", r.UID, DetailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Examples) != 1 || d.Examples[0].UID != keep.UID {
		t.Fatalf("详情里应只剩 1 条示例: %+v", d.Examples)
	}
	// 再删一次应报错（示例已不存在）
	if err := svc.DeleteResponseExample("demo", r.UID, gone.UID, ""); err == nil {
		t.Error("删除不存在的示例应报错")
	}
	// 缺 uid / exampleUid
	if err := svc.DeleteResponseExample("demo", "", gone.UID, ""); err == nil {
		t.Error("缺 uid 应报错")
	}
	if err := svc.DeleteResponseExample("demo", r.UID, "", ""); err == nil {
		t.Error("缺 exampleUid 应报错")
	}
}
