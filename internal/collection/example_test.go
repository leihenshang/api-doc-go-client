package collection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
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
	// 回收站应有 1 条可恢复记录（ListTrash 会排除 .index.jsonl 索引本身）
	trash, terr := c.ListTrash()
	if terr != nil || len(trash) != 1 {
		t.Fatalf(".trash 应有 1 条回收记录: %+v %v", trash, terr)
	}
}

// TestUpsertResponseExampleFileKeepsOldOnWriteFailure 改名写失败时**不能**先删旧文件。
//
// 回归（QA P1）：旧实现是「先删旧落点 → 再 writeFileAtomic」，一旦新名写不进去
// （名字超长 / 目录不可写 / 磁盘满），示例就整条消失 —— 静默的数据丢失。
// 这里用「目标路径被一个同名目录占住」稳定制造写入失败，断言旧文件还在、内容没变。
func TestUpsertResponseExampleFileKeepsOldOnWriteFailure(t *testing.T) {
	c := openTemp(t)
	r, err := c.CreateRequest("", "接口", "GET")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	ex, err := c.SaveResponseExample(r.UID, "原名", ExampleRequest{Method: "GET", URL: "{{host}}/x"}, newExampleResponse())
	if err != nil {
		t.Fatalf("SaveResponseExample: %v", err)
	}
	oldAbs := filepath.Join(c.Dir, filepath.FromSlash(ex.Path))
	before, err := os.ReadFile(oldAbs)
	if err != nil {
		t.Fatalf("读旧文件: %v", err)
	}

	// 改名：新落点被一个目录占住 → 写入必然失败
	newPath := filepath.Join(filepath.Dir(ex.Path), "改坏了.yml")
	if err := os.MkdirAll(filepath.Join(c.Dir, filepath.FromSlash(newPath)), 0o755); err != nil {
		t.Fatalf("造阻塞目录: %v", err)
	}
	renamed := *ex
	renamed.Name = "改坏了"
	renamed.Path = newPath
	if _, err := c.UpsertResponseExampleFile(&renamed); err == nil {
		t.Fatal("目标被目录占住时写入应失败")
	}

	// 旧文件必须原样还在（先写后删的核心保证）
	after, err := os.ReadFile(oldAbs)
	if err != nil {
		t.Fatalf("写失败后旧示例不该消失: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("写失败后旧示例内容被改动")
	}
	list, err := c.ListResponseExamples(r.UID)
	if err != nil {
		t.Fatalf("ListResponseExamples: %v", err)
	}
	if len(list) != 1 || list[0].UID != ex.UID || list[0].Name != "原名" {
		t.Fatalf("写失败后列表示例应保持原样: %+v", list)
	}

	// 正常改名仍要生效：旧文件在写成功后被清掉
	renamed2 := *ex
	renamed2.Name = "新名"
	renamed2.Path = filepath.Join(filepath.Dir(ex.Path), "新名.yml")
	got, err := c.UpsertResponseExampleFile(&renamed2)
	if err != nil {
		t.Fatalf("正常改名应成功: %v", err)
	}
	if got != renamed2.Path {
		t.Fatalf("落盘路径: got %q want %q", got, renamed2.Path)
	}
	if _, err := os.Stat(oldAbs); !os.IsNotExist(err) {
		t.Fatalf("写成功后旧文件应被清掉: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, filepath.FromSlash(got))); err != nil {
		t.Fatalf("新文件应存在: %v", err)
	}
}

// legacyLongNamedExample 造一个「本版本之前存下的超长名示例」：
// 先正常存一条，再把磁盘文件改名成 83 个中文（249 字节，仍在文件系统的 255 字节上限内，
// 但超过现在的 240 字节基名上限），YAML 内容与 UID 保持不变 —— 这正是老版本留下的状态。
func legacyLongNamedExample(t *testing.T, c *Collection, reqUID string) *ResponseExample {
	t.Helper()
	ex, err := c.SaveResponseExample(reqUID, "老示例", ExampleRequest{Method: "GET", URL: "{{host}}/x"}, newExampleResponse())
	if err != nil {
		t.Fatalf("SaveResponseExample: %v", err)
	}
	oldAbs := filepath.Join(c.Dir, filepath.FromSlash(ex.Path))
	longBase := strings.Repeat("超", 83) // 249 字节
	newRel := filepath.Join(filepath.Dir(ex.Path), longBase+".yml")
	newAbs := filepath.Join(c.Dir, filepath.FromSlash(newRel))
	if err := os.Rename(oldAbs, newAbs); err != nil {
		t.Fatalf("改名造历史遗留状态: %v", err)
	}
	ex.Path = newRel
	return ex
}

// TestUpsertResponseExampleInPlaceSkipsLengthCheck 原地重写（路径没变）不做长度校验：
// 历史遗留的超长名示例必须还能「只改状态码」，否则它连编辑都编辑不了（QA P2）。
func TestUpsertResponseExampleInPlaceSkipsLengthCheck(t *testing.T) {
	c := openTemp(t)
	r, err := c.CreateRequest("", "接口", "GET")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	ex := legacyLongNamedExample(t, c, r.UID)
	if len(exampleBaseOf(ex.Path)) <= exampleNameMaxBytes {
		t.Fatalf("前置条件：基名应超过 %d 字节，实际 %d", exampleNameMaxBytes, len(exampleBaseOf(ex.Path)))
	}

	ex.Response.Status = 500
	if _, err := c.UpsertResponseExampleFile(ex); err != nil {
		t.Fatalf("原地重写不该被长度上限拦住: %v", err)
	}
	got, err := c.ListResponseExamples(r.UID)
	if err != nil {
		t.Fatalf("ListResponseExamples: %v", err)
	}
	if len(got) != 1 || got[0].Response.Status != 500 || got[0].Path != ex.Path {
		t.Fatalf("原地重写结果异常: %+v", got)
	}

	// 真改名（路径变了）仍受长度上限约束，且旧文件不动
	renamed := *ex
	renamed.Name = strings.Repeat("改", 90) // 270 字节 > 240
	renamed.Path = filepath.Join(filepath.Dir(ex.Path), renamed.Name+".yml")
	if _, rerr := c.UpsertResponseExampleFile(&renamed); rerr == nil {
		t.Fatal("改成超长名应报错")
	} else if !strings.Contains(rerr.Error(), "示例名过长") {
		t.Fatalf("应报「示例名过长」，实际: %v", rerr)
	}
	if _, serr := os.Stat(filepath.Join(c.Dir, filepath.FromSlash(ex.Path))); serr != nil {
		t.Fatalf("报错后旧文件应保持原样: %v", serr)
	}
}

// TestDeleteLongNamedExampleGoesToTrash 超长名示例必须删得掉：
// .trash 里的名字由 trashSafeBase 截断（时间戳前缀 + 截断基名 + 短哈希 + 扩展名），
// 否则 rename 会 ENAMETOOLONG，条目就永远留在集合里（QA P2）。
func TestDeleteLongNamedExampleGoesToTrash(t *testing.T) {
	c := openTemp(t)
	r, err := c.CreateRequest("", "接口", "GET")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	ex := legacyLongNamedExample(t, c, r.UID)

	if err := c.DeleteResponseExample(r.UID, ex.UID); err != nil {
		t.Fatalf("超长名示例应能删除: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, filepath.FromSlash(ex.Path))); !os.IsNotExist(err) {
		t.Fatalf("删除后原文件应不在: %v", err)
	}
	ents, err := os.ReadDir(filepath.Join(c.Dir, trashDirName))
	if err != nil {
		t.Fatalf("读 .trash: %v", err)
	}
	files := make([]string, 0, len(ents))
	for _, e := range ents {
		if e.IsDir() || e.Name() == trashIndexName {
			continue
		}
		if len(e.Name()) > trashNameMaxBytes {
			t.Fatalf(".trash 文件名应被截断到 %d 字节内，实际 %d: %s", trashNameMaxBytes, len(e.Name()), e.Name())
		}
		files = append(files, e.Name())
	}
	if len(files) != 1 {
		t.Fatalf(".trash 应有 1 个文件: %+v", files)
	}
	// 截断后仍要能认出是「哪条」：哈希后缀 + 原扩展名都在
	if !strings.Contains(files[0], "~") || !strings.HasSuffix(files[0], ".yml") {
		t.Fatalf("截断后的回收站名应保留短哈希与扩展名: %s", files[0])
	}
	// 索引里的 origin 保留原相对路径，恢复才能回到原位
	entries, err := c.ListTrash()
	if err != nil {
		t.Fatalf("ListTrash: %v", err)
	}
	if len(entries) != 1 || entries[0].Origin != filepath.ToSlash(ex.Path) {
		t.Fatalf("回收站索引应记住原路径: %+v", entries)
	}
}

// TestTrashSafeBaseKeepsShortNames 短名原样保留（不能给正常条目加哈希尾巴）。
func TestTrashSafeBaseKeepsShortNames(t *testing.T) {
	for _, base := range []string{"ping.yml", "用户列表.yml", "a.yml"} {
		if got := trashSafeBase(base); got != base {
			t.Errorf("短名不应被改写：%q → %q", base, got)
		}
	}
	// 超长名：截断 + 哈希 + 扩展名，且不切断 UTF-8
	long := strings.Repeat("超", 100) + ".yml"
	got := trashSafeBase(long)
	if len(got)+len(".20060102-150405") > trashNameMaxBytes {
		t.Errorf("截断后仍超长: %d 字节", len(got))
	}
	if !strings.HasSuffix(got, ".yml") || !strings.Contains(got, "~") {
		t.Errorf("截断结果应保留扩展名与短哈希: %q", got)
	}
	if !utf8.ValidString(got) {
		t.Errorf("截断切断了 UTF-8 字符: %q", got)
	}
}
