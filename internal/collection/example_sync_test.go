package collection

import (
	"path"
	"testing"
)

// 同步原语：全量遍历 / 按 UID 落盘（含同名冲突换名）/ 按 UID 删除。
func TestExampleSyncPrimitives(t *testing.T) {
	c := newTestCollection(t)
	r, err := c.CreateRequest("用户/列表", "列表", "GET")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	dir := ExampleDirForPath(r.Path)

	// 已有同名文件（本请求保存的示例）：Upsert 不同 UID → 自动换名
	if _, err := c.SaveResponseExample(r.UID, "成功", ExampleRequest{Method: "GET"}, newExampleResponse()); err != nil {
		t.Fatalf("SaveResponseExample: %v", err)
	}

	remote := &ResponseExample{
		UID: "remote-uid-1", Name: "成功", RequestUID: r.UID,
		Path: path.Join(dir, "成功.yml"), Seq: 99,
		Response: ExampleResponse{Status: 200, Body: `{"from":"server"}`},
	}
	wantOrig := remote.Path
	final, err := c.UpsertResponseExampleFile(remote)
	if err != nil {
		t.Fatalf("UpsertResponseExampleFile: %v", err)
	}
	if final == wantOrig {
		t.Fatalf("同名冲突应换名，实际仍为 %s", final)
	}

	all, err := c.ListAllResponseExamples()
	if err != nil {
		t.Fatalf("ListAllResponseExamples: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("应有两个示例，实际 %d: %+v", len(all), all)
	}
	var found bool
	for _, ex := range all {
		if ex.UID == "remote-uid-1" {
			found = true
			if ex.Response.Body != `{"from":"server"}` || ex.Name != remote.Name {
				t.Errorf("远端示例内容不符: %+v", ex)
			}
			if ex.Path != final {
				t.Errorf("路径应与返回一致: %s != %s", ex.Path, final)
			}
		}
	}
	if !found {
		t.Fatal("未找到远端拉取的示例")
	}

	// 同 UID 重写换名场景：旧文件被移除
	remote.Name = "改名"
	remote.Path = path.Join(dir, "改名.yml")
	final2, err := c.UpsertResponseExampleFile(remote)
	if err != nil {
		t.Fatalf("UpsertResponseExampleFile(改名): %v", err)
	}
	all2, _ := c.ListAllResponseExamples()
	n := 0
	for _, ex := range all2 {
		if ex.UID == "remote-uid-1" {
			n++
			if ex.Path != final2 {
				t.Errorf("重写后路径不符: %s != %s", ex.Path, final2)
			}
		}
	}
	if n != 1 {
		t.Fatalf("同 UID 应只有一份文件，实际 %d", n)
	}

	// 按 UID 删除
	if err := c.DeleteResponseExampleByUID("remote-uid-1"); err != nil {
		t.Fatalf("DeleteResponseExampleByUID: %v", err)
	}
	all3, _ := c.ListAllResponseExamples()
	if len(all3) != 1 || all3[0].UID == "remote-uid-1" {
		t.Fatalf("删除后应只剩本地那份: %+v", all3)
	}
	// 删除不存在的 UID 视为成功（幂等）
	if err := c.DeleteResponseExampleByUID("remote-uid-1"); err != nil {
		t.Errorf("重复删除应幂等: %v", err)
	}
}
