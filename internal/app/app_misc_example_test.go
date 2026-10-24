package app

// 响应示例的「改」能力（MCP update_response_example 的落点）：
// 改名必须把磁盘文件名与 path 一起改（旧文件不残留），重名要自动加 (1) 而不是覆盖别人的示例；
// 改响应体时 Size（字节数）要跟着变。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"api-doc-go-client/internal/collection"
)

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

// TestUpdateResponseExampleRename 改名 → 旧文件消失、新文件出现、path 与 name 同步。
func TestUpdateResponseExampleRename(t *testing.T) {
	core, dir := newTestApp(t)
	r, err := core.CreateRequest("", "示例接口", "GET")
	if err != nil {
		t.Fatalf("建请求失败: %v", err)
	}
	ex, err := core.SaveResponseExampleRaw(r.UID, "旧名", 200, nil, `{"ok":true}`, "")
	if err != nil {
		t.Fatalf("存示例失败: %v", err)
	}
	if ex.Response.ContentType != "application/json" {
		t.Errorf("contentType 应按 body 推断为 application/json，实际 %q", ex.Response.ContentType)
	}
	oldAbs := filepath.Join(dir, filepath.FromSlash(ex.Path))
	if _, err := os.Stat(oldAbs); err != nil {
		t.Fatalf("旧文件应存在: %v", err)
	}

	headers := []collection.KV{{Name: "X-Trace", Value: "t-9", Enabled: true}}
	up, err := core.UpdateResponseExample(r.UID, ex.UID, strPtr("新名"), intPtr(201), &headers, nil, nil)
	if err != nil {
		t.Fatalf("更新示例失败: %v", err)
	}
	if up.Name != "新名" || up.Response.Status != 201 {
		t.Fatalf("改名/改状态码未生效: %+v", up)
	}
	if len(up.Response.Headers) != 1 || up.Response.Headers[0].Value != "t-9" {
		t.Fatalf("响应头未生效: %+v", up.Response.Headers)
	}
	if up.Path == ex.Path || !strings.Contains(up.Path, "新名") {
		t.Fatalf("改名后 path 应带上新名，实际 %q（原 %q）", up.Path, ex.Path)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(up.Path))); err != nil {
		t.Errorf("新文件应存在: %v", err)
	}
	if _, err := os.Stat(oldAbs); !os.IsNotExist(err) {
		t.Errorf("旧文件不应残留（rename 漏删旧文件）: %v", err)
	}
	// 改名后仍能按 UID 在列表里找到（不是删了重建）
	list, err := core.ListResponseExamples(r.UID)
	if err != nil {
		t.Fatalf("列示例失败: %v", err)
	}
	if len(list) != 1 || list[0].UID != ex.UID || list[0].Name != "新名" {
		t.Fatalf("列表应与更新结果一致: %+v", list)
	}
}

// TestUpdateResponseExampleBodyUpdatesSize 只改响应体：Size 跟着变，其它字段不动。
func TestUpdateResponseExampleBodyUpdatesSize(t *testing.T) {
	core, _ := newTestApp(t)
	r, err := core.CreateRequest("", "示例体", "POST")
	if err != nil {
		t.Fatalf("建请求失败: %v", err)
	}
	ex, err := core.SaveResponseExampleRaw(r.UID, "体", 200, nil, `{"a":1}`, "json")
	if err != nil {
		t.Fatalf("存示例失败: %v", err)
	}
	body := `{"a":1,"b":[1,2,3]}`
	up, err := core.UpdateResponseExample(r.UID, ex.UID, nil, nil, nil, &body, nil)
	if err != nil {
		t.Fatalf("更新示例失败: %v", err)
	}
	if up.Response.Body != body || up.Response.Size != len(body) {
		t.Fatalf("响应体/Size 未同步: body=%q size=%d（期望 %d）", up.Response.Body, up.Response.Size, len(body))
	}
	if up.Name != "体" || up.Response.Status != 200 || up.Response.ContentType != "json" {
		t.Fatalf("未传字段应保持原值: %+v", up)
	}
}

// TestUpdateResponseExampleDuplicateNameGetsSuffix 改成别人已用的名字 → 自动加 (1)，不覆盖别人。
func TestUpdateResponseExampleDuplicateNameGetsSuffix(t *testing.T) {
	core, dir := newTestApp(t)
	r, err := core.CreateRequest("", "重名接口", "GET")
	if err != nil {
		t.Fatalf("建请求失败: %v", err)
	}
	first, err := core.SaveResponseExampleRaw(r.UID, "同名", 200, nil, `{"n":1}`, "")
	if err != nil {
		t.Fatalf("存第一条示例失败: %v", err)
	}
	second, err := core.SaveResponseExampleRaw(r.UID, "另一个", 200, nil, `{"n":2}`, "")
	if err != nil {
		t.Fatalf("存第二条示例失败: %v", err)
	}
	up, err := core.UpdateResponseExample(r.UID, second.UID, strPtr("同名"), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("改名失败: %v", err)
	}
	if up.Name != "同名 (1)" {
		t.Fatalf("重名应自动加 (1)，实际 %q", up.Name)
	}
	if !strings.Contains(up.Path, "同名 (1)") {
		t.Errorf("path 应同步为去重后的名字: %s", up.Path)
	}
	// 两条都在，第一条内容未被动到
	list, err := core.ListResponseExamples(r.UID)
	if err != nil {
		t.Fatalf("列示例失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("应仍是 2 条示例: %+v", list)
	}
	names := map[string]string{}
	for _, it := range list {
		names[it.UID] = it.Name
	}
	if names[first.UID] != "同名" || names[second.UID] != "同名 (1)" {
		t.Fatalf("去重结果不符: %+v", names)
	}
	// 旧文件（另一个.yml）不残留
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(second.Path))); !os.IsNotExist(err) {
		t.Errorf("改名后旧文件不应残留: %v", err)
	}
}

// TestUpdateResponseExampleNotFound 找不到示例要报错，不能静默成功。
func TestUpdateResponseExampleNotFound(t *testing.T) {
	core, _ := newTestApp(t)
	r, err := core.CreateRequest("", "找不到", "GET")
	if err != nil {
		t.Fatalf("建请求失败: %v", err)
	}
	if _, err := core.UpdateResponseExample(r.UID, "no-such-uid", strPtr("x"), nil, nil, nil, nil); err == nil {
		t.Fatal("示例 uid 不存在应报错")
	}
}

// TestUpdateResponseExampleRejectsIllegalName 非法名（含 / 或超长）直接报错且**不动任何文件**，
// 与「新建示例」的口径一致（SaveResponseExample 也是 validEntryName 校验后报错）。
// 回归 QA P1：旧实现用 app 自带的 sanitize（不校验、不截断），200 个中文会走到
// ENAMETOOLONG，而当时旧文件已被删掉 —— 示例整条消失。
func TestUpdateResponseExampleRejectsIllegalName(t *testing.T) {
	core, dir := newTestApp(t)
	r, err := core.CreateRequest("", "非法名接口", "GET")
	if err != nil {
		t.Fatalf("建请求失败: %v", err)
	}
	ex, err := core.SaveResponseExampleRaw(r.UID, "原名", 200, nil, `{"ok":true}`, "")
	if err != nil {
		t.Fatalf("存示例失败: %v", err)
	}
	oldAbs := filepath.Join(dir, filepath.FromSlash(ex.Path))

	for _, bad := range []string{"改名/带斜杠", strings.Repeat("超", 200)} {
		if _, err := core.UpdateResponseExample(r.UID, ex.UID, &bad, nil, nil, nil, nil); err == nil {
			t.Fatalf("非法示例名 %q 应报错", bad)
		}
		// 报错后旧文件必须还在（不能先删后写）
		if _, err := os.Stat(oldAbs); err != nil {
			t.Fatalf("报错后旧示例不该消失（%q）: %v", bad, err)
		}
		list, lerr := core.ListResponseExamples(r.UID)
		if lerr != nil {
			t.Fatalf("列示例失败: %v", lerr)
		}
		if len(list) != 1 || list[0].Name != "原名" {
			t.Fatalf("报错后示例应保持原样: %+v", list)
		}
	}
}

// TestUpdateResponseExampleNameMatchesFileName 名字与文件名同源：
// 长度按 rune 截断到 100（collection.SanitizeFileName 的口径），显示名与文件名用同一个值。
// 这条能区分「两套 sanitize」：app 自带的那个不截断，110 字符的名字会写出
// 「YAML 名 110 字 / 文件 100 字」的不一致状态。
func TestUpdateResponseExampleNameMatchesFileName(t *testing.T) {
	core, dir := newTestApp(t)
	r, err := core.CreateRequest("", "同源接口", "GET")
	if err != nil {
		t.Fatalf("建请求失败: %v", err)
	}
	ex, err := core.SaveResponseExampleRaw(r.UID, "起点", 200, nil, `{"ok":true}`, "")
	if err != nil {
		t.Fatalf("存示例失败: %v", err)
	}

	// 110 个 ASCII 字符：validEntryName 放行（≤120），sanitize 截到 100
	raw := strings.Repeat("a", 110)
	up, err := core.UpdateResponseExample(r.UID, ex.UID, &raw, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("改名失败: %v", err)
	}
	want := collection.SanitizeFileName(raw)
	if len([]rune(want)) != 100 {
		t.Fatalf("前置条件：SanitizeFileName 应截断到 100 rune，实际 %d", len([]rune(want)))
	}
	if up.Name != want {
		t.Fatalf("显示名应与文件名同源：got %q（%d 字） want %q（%d 字）",
			up.Name, len([]rune(up.Name)), want, len([]rune(want)))
	}
	if got := filepath.Base(up.Path); got != want+".yml" {
		t.Fatalf("文件名应与显示名同源：got %q want %q", got, want+".yml")
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(up.Path))); err != nil {
		t.Fatalf("新文件应存在: %v", err)
	}

	// 首尾空格被 trim（同一套规则）
	spaced := "  带首尾空格的名字  "
	up2, err := core.UpdateResponseExample(r.UID, ex.UID, &spaced, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("改名失败: %v", err)
	}
	if up2.Name != "带首尾空格的名字" || filepath.Base(up2.Path) != "带首尾空格的名字.yml" {
		t.Fatalf("首尾空格未按同一套规则处理: name=%q path=%q", up2.Name, up2.Path)
	}
}

// TestUpdateResponseExampleTooLongName 中文长名（三个字节一个字）会在落盘前被长度检查拦下，
// 报可读的中文错误而不是 ENAMETOOLONG，且旧文件原样保留。
func TestUpdateResponseExampleTooLongName(t *testing.T) {
	core, dir := newTestApp(t)
	r, err := core.CreateRequest("", "超长名接口", "GET")
	if err != nil {
		t.Fatalf("建请求失败: %v", err)
	}
	ex, err := core.SaveResponseExampleRaw(r.UID, "原名", 200, nil, `{"ok":true}`, "")
	if err != nil {
		t.Fatalf("存示例失败: %v", err)
	}
	oldAbs := filepath.Join(dir, filepath.FromSlash(ex.Path))
	long := strings.Repeat("超", 100) // 300 字节 > 255 上限
	_, err = core.UpdateResponseExample(r.UID, ex.UID, &long, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("超长示例名应报错")
	}
	if !strings.Contains(err.Error(), "示例名过长") {
		t.Fatalf("应报可读的「示例名过长」，实际: %v", err)
	}
	if _, serr := os.Stat(oldAbs); serr != nil {
		t.Fatalf("报错后旧示例不该消失: %v", serr)
	}
	list, lerr := core.ListResponseExamples(r.UID)
	if lerr != nil {
		t.Fatalf("列示例失败: %v", lerr)
	}
	if len(list) != 1 || list[0].Name != "原名" {
		t.Fatalf("报错后示例应保持原样: %+v", list)
	}
}

// TestLegacyLongNamedExampleStillEditableAndDeletable 本版本之前存下的超长名示例
// （文件名 > 240 字节但仍在文件系统 255 字节上限内）必须还能改字段、删掉 ——
// QA P2 复现路径：只改 status 报「示例名过长」，删除报「文件可能被其他程序占用」。
func TestLegacyLongNamedExampleStillEditableAndDeletable(t *testing.T) {
	core, dir := newTestApp(t)
	r, err := core.CreateRequest("", "遗留", "GET")
	if err != nil {
		t.Fatalf("建请求失败: %v", err)
	}
	ex, err := core.SaveResponseExampleRaw(r.UID, "老示例", 200, nil, `{"ok":true}`, "")
	if err != nil {
		t.Fatalf("存示例失败: %v", err)
	}
	// 造「历史遗留」状态：把磁盘文件改名成 83 个中文（249 字节），YAML 与 UID 不动
	longBase := strings.Repeat("超", 83)
	longRel := filepath.Join(filepath.Dir(ex.Path), longBase+".yml")
	if err := os.Rename(filepath.Join(dir, filepath.FromSlash(ex.Path)), filepath.Join(dir, filepath.FromSlash(longRel))); err != nil {
		t.Fatalf("造历史遗留状态: %v", err)
	}

	// ① 只改状态码：不能因为「名字本来就很长」被拦住
	status := 500
	up, err := core.UpdateResponseExample(r.UID, ex.UID, nil, &status, nil, nil, nil)
	if err != nil {
		t.Fatalf("遗留超长名示例应能改状态码: %v", err)
	}
	if up.Response.Status != 500 || up.Path != longRel {
		t.Fatalf("改状态码结果异常: status=%d path=%q", up.Response.Status, up.Path)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(longRel))); err != nil {
		t.Fatalf("文件应仍在原路径（原地重写）: %v", err)
	}

	// ② 删除：.trash 里的名字要被截断，不能 ENAMETOOLONG
	if err := core.DeleteResponseExample(r.UID, ex.UID); err != nil {
		t.Fatalf("遗留超长名示例应能删除: %v", err)
	}
	ents, err := os.ReadDir(filepath.Join(dir, ".trash"))
	if err != nil {
		t.Fatalf("读 .trash: %v", err)
	}
	found := false
	for _, e := range ents {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		if len(e.Name()) > 240 {
			t.Fatalf(".trash 文件名应被截断，实际 %d 字节: %s", len(e.Name()), e.Name())
		}
		found = true
	}
	if !found {
		t.Fatal("示例应进 .trash")
	}
	list, lerr := core.ListResponseExamples(r.UID)
	if lerr != nil {
		t.Fatalf("列示例失败: %v", lerr)
	}
	if len(list) != 0 {
		t.Fatalf("删除后不该还有示例: %+v", list)
	}
}
