package collection

import (
	"encoding/json"
	"testing"
)

// 扩展键（请求体 / 认证 / 请求设置 / 脚本 / 变量 / 断言 / 响应内容类型）的双向往返与兼容用例。
// 兼容口径见 sync_extra_fields.go 头注释：键存在且为字符串才覆盖，空串清空，缺键不动。

var extendedKeys = []string{
	extraKeyBodyType, extraKeyBody, extraKeyBodyForm,
	extraKeyAuth, extraKeySettings, extraKeyScript,
	extraKeyVars, extraKeyAsserts, extraKeyRetContentType,
}

// extraMap 解析 extra JSON 为 map，便于逐键断言。
func extraMap(t *testing.T, extraJSON string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(extraJSON), &m); err != nil {
		t.Fatalf("extra 不是合法 JSON: %v\n%s", err, extraJSON)
	}
	return m
}

// fullRequest 构造「扩展键全填」的请求：往返用例的源，也是兼容用例的初始状态。
func fullRequest() *Request {
	follow, insecure := false, true
	return &Request{
		Name:   "demo",
		Method: "POST",
		URL:    "/api/demo",
		Body: Body{
			Type: "form",
			Form: []KV{
				{Name: "a", Value: "1", Enabled: true, Description: "字段 a"},
				{Name: "b", Value: "2", Enabled: false},
			},
		},
		Auth: &Auth{Type: "bearer", Token: "tok"},
		Settings: &RequestSettings{
			TimeoutSec: 30, FollowRedirects: &follow, MaxRedirects: 5, InsecureSSL: &insecure, EncodeURL: true,
		},
		Script:         &ScriptBlock{PreRequest: "var a=1", PostResponse: "assert(1)"},
		VarsPreRequest: []ScriptVar{{Name: "v1", Value: "x", Enabled: true}},
		Asserts:        []ScriptAssert{{Name: "状态 200", Expr: "res.status==200"}},
		RespDoc:        &ResponseDoc{Type: "object", ContentType: "xml", Sample: `<ok/>`},
	}
}

// boolPtrEq 比较 *bool 的值（nil 视为「未设置」，与 nil 相等、与 true/false 不等）。
func boolPtrEq(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// settingsEq 逐字段比较请求设置：结构体 == 会比较 *bool 的指针地址而非值，不能直接用。
func settingsEq(a, b *RequestSettings) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.TimeoutSec == b.TimeoutSec &&
		a.MaxRedirects == b.MaxRedirects &&
		a.EncodeURL == b.EncodeURL &&
		boolPtrEq(a.FollowRedirects, b.FollowRedirects) &&
		boolPtrEq(a.InsecureSSL, b.InsecureSSL)
}

// TestSyncExtra_ExtendedRoundTrip 扩展键必须能完整往返：推出去再拉回来，字段逐一相等。
func TestSyncExtra_ExtendedRoundTrip(t *testing.T) {
	src := fullRequest()
	got := &Request{}
	ApplySyncExtra(got, SyncExtra(src))

	if got.Body.Type != src.Body.Type {
		t.Errorf("bodyType: got %q want %q", got.Body.Type, src.Body.Type)
	}
	if len(got.Body.Form) != len(src.Body.Form) {
		t.Fatalf("bodyForm 行数: got %d want %d", len(got.Body.Form), len(src.Body.Form))
	}
	for i, want := range src.Body.Form {
		if got.Body.Form[i] != want {
			t.Errorf("bodyForm[%d]: got %+v want %+v", i, got.Body.Form[i], want)
		}
	}
	if got.Auth == nil || *got.Auth != *src.Auth {
		t.Errorf("auth: got %+v want %+v", got.Auth, src.Auth)
	}
	if !settingsEq(got.Settings, src.Settings) {
		t.Errorf("settings: got %+v want %+v", got.Settings, src.Settings)
	}
	if got.Script == nil || *got.Script != *src.Script {
		t.Errorf("script: got %+v want %+v", got.Script, src.Script)
	}
	if len(got.VarsPreRequest) != 1 || got.VarsPreRequest[0] != src.VarsPreRequest[0] {
		t.Errorf("vars: got %+v want %+v", got.VarsPreRequest, src.VarsPreRequest)
	}
	if len(got.Asserts) != 1 || got.Asserts[0] != src.Asserts[0] {
		t.Errorf("asserts: got %+v want %+v", got.Asserts, src.Asserts)
	}
	if got.RespDoc == nil || got.RespDoc.ContentType != src.RespDoc.ContentType {
		t.Errorf("retContentType: got %+v want %q", got.RespDoc, src.RespDoc.ContentType)
	}
}

// TestSyncExtra_HTTPAlwaysEmitsExtendedKeys HTTP 请求必须整份下发扩展键，
// 否则本地删掉的内容推不到服务端（服务端「缺键不动」会保留旧值）。
func TestSyncExtra_HTTPAlwaysEmitsExtendedKeys(t *testing.T) {
	m := extraMap(t, SyncExtra(&Request{Name: "空请求", Method: "GET", URL: "/x"}))
	for _, k := range extendedKeys {
		if _, ok := m[k]; !ok {
			t.Errorf("缺少扩展键 %s（应始终输出以覆盖服务端旧值）", k)
		}
	}
}

// TestSyncExtra_GRPCOmitsExtendedKeys gRPC 只同步说明：扩展键一律不下发，
// 否则服务端会把 gRPC 条目当 HTTP 条目写入请求体 / 认证等列。
func TestSyncExtra_GRPCOmitsExtendedKeys(t *testing.T) {
	r := fullRequest()
	r.GRPC = &GrpcBlock{Service: "demo.Greeter", Method: "SayHello"}
	if !r.IsGRPC() {
		t.Fatal("构造失败：请求未被识别为 gRPC")
	}
	m := extraMap(t, SyncExtra(r))
	for _, k := range extendedKeys {
		if _, ok := m[k]; ok {
			t.Errorf("gRPC 请求不应下发扩展键 %s", k)
		}
	}
}

// TestApplySyncExtra_OldKeySetKeepsExtendedFields 旧服务端只下发既有六键时，
// 本地已填的扩展字段必须原样保留（缺键不动）——这是「旧服务端不清空新字段」的核心用例。
func TestApplySyncExtra_OldKeySetKeepsExtendedFields(t *testing.T) {
	got := fullRequest()
	old := `{"desc":"新说明","headers":"[]","params":"[]","retType":"object","retParams":"[]","retSample":"{}"}`
	ApplySyncExtra(got, old)

	if got.Auth == nil || got.Auth.Token != "tok" {
		t.Errorf("auth 被旧键集清空: %+v", got.Auth)
	}
	if !settingsEq(got.Settings, fullRequest().Settings) {
		t.Errorf("settings 被旧键集改动: %+v", got.Settings)
	}
	if got.Script == nil || got.Script.PreRequest != "var a=1" {
		t.Errorf("script 被旧键集清空: %+v", got.Script)
	}
	if len(got.VarsPreRequest) != 1 || len(got.Asserts) != 1 {
		t.Errorf("vars/asserts 被旧键集清空: %+v %+v", got.VarsPreRequest, got.Asserts)
	}
	if got.RespDoc == nil || got.RespDoc.ContentType != "xml" {
		t.Errorf("retContentType 被旧键集清空: %+v", got.RespDoc)
	}
	if got.Body.Type != "form" || len(got.Body.Form) != 2 {
		t.Errorf("body 被旧键集改动: %+v", got.Body)
	}
}

// TestApplySyncExtra_EmptyStringClearsExtendedFields 空串即清空：
// 服务端明确下发空串时，本地对应字段必须清掉（删除要能传播）。
func TestApplySyncExtra_EmptyStringClearsExtendedFields(t *testing.T) {
	clearAll := map[string]any{}
	for _, k := range extendedKeys {
		clearAll[k] = ""
	}
	b, err := json.Marshal(clearAll)
	if err != nil {
		t.Fatal(err)
	}
	got := fullRequest()
	ApplySyncExtra(got, string(b))

	if got.Auth != nil {
		t.Errorf("auth 应被清空，实际 %+v", got.Auth)
	}
	if got.Settings != nil {
		t.Errorf("settings 应被清空，实际 %+v", got.Settings)
	}
	if got.Script != nil {
		t.Errorf("script 应被清空，实际 %+v", got.Script)
	}
	if len(got.VarsPreRequest) != 0 || len(got.Asserts) != 0 {
		t.Errorf("vars/asserts 应被清空，实际 %+v %+v", got.VarsPreRequest, got.Asserts)
	}
	if len(got.Body.Form) != 0 {
		t.Errorf("bodyForm 应被清空，实际 %+v", got.Body.Form)
	}
	if got.RespDoc != nil && got.RespDoc.ContentType != "" {
		t.Errorf("retContentType 应被清空，实际 %q", got.RespDoc.ContentType)
	}
}

// TestTriBoolRoundTrip 三态布尔：nil=-1 / true=1 / false=0，来回不丢「未设置」这一档。
func TestTriBoolRoundTrip(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name string
		in   *bool
		want int8
	}{
		{"未设置", nil, -1},
		{"开启", &yes, 1},
		{"关闭", &no, 0},
	}
	for _, c := range cases {
		got := triBoolToInt(c.in)
		if got != c.want {
			t.Errorf("%s: triBoolToInt got %d want %d", c.name, got, c.want)
		}
		if back := intToTriBool(got); !boolPtrEq(back, c.in) {
			t.Errorf("%s: intToTriBool 未还原，got %v want %v", c.name, back, c.in)
		}
	}
}

// TestBodyTextOf form/multipart 的原文不应进 body 键（内容由 bodyForm 承载，避免重复落库）。
func TestBodyTextOf(t *testing.T) {
	cases := []struct {
		body Body
		want string
	}{
		{Body{Type: "json", Raw: `{"a":1}`}, `{"a":1}`},
		{Body{Type: "text", Raw: "hello"}, "hello"},
		{Body{Type: "form", Form: []KV{{Name: "a", Value: "1"}}}, ""},
		{Body{Type: "multipart", Form: []KV{{Name: "a", Value: "1"}}}, ""},
		{Body{Type: "none", Raw: "x"}, ""},
	}
	for _, c := range cases {
		if got := bodyTextOf(c.body); got != c.want {
			t.Errorf("body(type=%s): got %q want %q", c.body.Type, got, c.want)
		}
	}
}
