package collection

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// decodeRows 解析 extra 里的 params 文本为参数行切片。
func decodeRows(t *testing.T, extraJSON, key string) []syncParamRow {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(extraJSON), &m); err != nil {
		t.Fatalf("extra 不是合法 JSON: %v\n%s", err, extraJSON)
	}
	s, _ := m[key].(string)
	var rows []syncParamRow
	if strings.TrimSpace(s) != "" {
		if err := json.Unmarshal([]byte(s), &rows); err != nil {
			t.Fatalf("%s 行解析失败: %v\n%s", key, err, s)
		}
	}
	return rows
}

func TestSyncExtra_QueryHeadersAndBody(t *testing.T) {
	r := &Request{
		Name:   "新建用户",
		Method: "POST",
		URL:    "/api/users",
		Params: []KV{{Name: "page", Value: "1", Description: "页码", Enabled: true}},
		Headers: []KV{
			{Name: "X-Token", Value: "abc", Enabled: true},
			{Name: "X-Disabled", Value: "off", Enabled: false},
		},
		Body: Body{Type: "json", Raw: `{"name":"alice","age":20,"addr":{"city":"SH"},"tags":["x"]}`},
		Docs: "接口说明",
	}
	extra := SyncExtra(r)

	// headers 行契约：{name,value,description}，启停状态不参与同步（与服务端 Bruno 导入一致）。
	var hm map[string]any
	_ = json.Unmarshal([]byte(extra), &hm)
	headerJSON, _ := hm["headers"].(string)
	var headers []syncHeaderRow
	if err := json.Unmarshal([]byte(headerJSON), &headers); err != nil {
		t.Fatalf("headers 解析失败: %v", err)
	}
	wantHeaders := []syncHeaderRow{
		{Name: "X-Token", Value: "abc"},
		{Name: "X-Disabled", Value: "off"},
	}
	if !reflect.DeepEqual(headers, wantHeaders) {
		t.Fatalf("headers=%v, want %v", headers, wantHeaders)
	}
	if hm["desc"] != "接口说明" {
		t.Fatalf("desc=%v", hm["desc"])
	}

	// params：query 行在前，JSON 体按服务端口径展平（对象键序 addr/age/name/tags）。
	got := decodeRows(t, extra, "params")
	want := []syncParamRow{
		{FieldName: "page", Type: "string", Description: "页码", Example: "1"},
		{FieldName: "addr", Type: "object"},
		{FieldName: "addr.city", Type: "string", Example: "SH"},
		{FieldName: "age", Type: "integer", Example: "20"},
		{FieldName: "name", Type: "string", Example: "alice"},
		{FieldName: "tags", Type: "array"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("params 展平不一致:\n got=%v\nwant=%v", got, want)
	}
}

func TestSyncExtra_EmptyAndGRPC(t *testing.T) {
	// 空请求：三个键恒在，值为空串（整份覆盖语义）。
	extra := SyncExtra(&Request{Method: "GET", URL: "/x"})
	var m map[string]any
	_ = json.Unmarshal([]byte(extra), &m)
	for _, k := range []string{"desc", "headers", "params"} {
		if v, ok := m[k]; !ok || v != "" {
			t.Fatalf("空请求 %s 应为空串，got %v", k, v)
		}
	}

	// 非法 JSON 体不阻断：仅保留 query 行。
	r := &Request{
		Method: "POST",
		Params: []KV{{Name: "q", Value: "v"}},
		Body:   Body{Type: "json", Raw: "{not-json"},
	}
	rows := decodeRows(t, SyncExtra(r), "params")
	if len(rows) != 1 || rows[0].FieldName != "q" {
		t.Fatalf("非法 JSON 体时应只剩 query 行, got %v", rows)
	}

	// gRPC：只同步说明，不产生 http 参数/响应文档。
	g := &Request{Method: MethodGRPC, GRPC: &GrpcBlock{}, Docs: "rpc 说明"}
	ge := SyncExtra(g)
	var gm map[string]any
	_ = json.Unmarshal([]byte(ge), &gm)
	if gm["desc"] != "rpc 说明" || gm["params"] != "" || gm["headers"] != "" ||
		gm["retType"] != "" || gm["retParams"] != "" || gm["retSample"] != "" {
		t.Fatalf("gRPC extra 异常: %v", gm)
	}
}

func TestSyncExtra_ResponseDoc(t *testing.T) {
	// 响应文档随推送上行：retType/retParams/retSample 三键齐全；
	// 返回参数行原样输出（fieldName 已是嵌套路径，不展平）。
	r := &Request{
		Method: "POST", URL: "/users",
		RespDoc: &ResponseDoc{
			Type: "object",
			Params: []ResponseParam{
				{FieldName: "code", Type: "integer", Required: true, Description: "状态码", Example: "200"},
				{FieldName: "data.list[].id", Type: "integer", Example: "9"},
			},
			Sample: `{"code":200,"data":{"list":[{"id":9}]}}`,
		},
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(SyncExtra(r)), &m)
	if m["retType"] != "object" || m["retSample"] != r.RespDoc.Sample {
		t.Fatalf("retType/retSample 异常: %v", m)
	}
	var rows []ResponseParam
	if err := json.Unmarshal([]byte(m["retParams"].(string)), &rows); err != nil {
		t.Fatalf("retParams 解析失败: %v", err)
	}
	if !reflect.DeepEqual(rows, r.RespDoc.Params) {
		t.Fatalf("retParams 行被改写（应原样）:\n got=%v\nwant=%v", rows, r.RespDoc.Params)
	}

	// 无响应文档：三键为空串（整份覆盖语义）。
	empty := SyncExtra(&Request{Method: "GET", URL: "/x"})
	_ = json.Unmarshal([]byte(empty), &m)
	for _, k := range []string{"retType", "retParams", "retSample"} {
		if v, ok := m[k]; !ok || v != "" {
			t.Fatalf("无响应文档时 %s 应为空串, got %v", k, v)
		}
	}
}

// 响应快照（状态码/响应头，item 8）不参与同步；样例被服务端改写时本地快照必须清掉，
// 避免「新样例 + 旧响应头」错位展示。
func TestApplySyncExtraSnapshotFollowsSample(t *testing.T) {
	newDoc := func(sample string) *Request {
		return &Request{RespDoc: &ResponseDoc{
			Type: "object", Sample: sample, Status: 200,
			Headers: []KV{{Name: "X-Trace", Value: "t1", Enabled: true}},
		}}
	}
	// 样例变了 → 快照清空
	r := newDoc(`{"a":1}`)
	ApplySyncExtra(r, `{"retSample":"{\"a\":2}"}`)
	if r.RespDoc.Sample != `{"a":2}` {
		t.Fatalf("样例未更新: %v", r.RespDoc.Sample)
	}
	if r.RespDoc.Status != 0 || len(r.RespDoc.Headers) != 0 {
		t.Fatalf("样例被外部改写后快照应清空: %+v", r.RespDoc)
	}
	// 样例没变 → 快照保留
	r2 := newDoc(`{"a":1}`)
	ApplySyncExtra(r2, `{"retSample":"{\"a\":1}"}`)
	if r2.RespDoc.Status != 200 || len(r2.RespDoc.Headers) != 1 {
		t.Fatalf("样例一致时快照应保留: %+v", r2.RespDoc)
	}
}

func TestApplySyncExtra_ResponseDoc(t *testing.T) {
	retRows := `[{"fieldName":"code","type":"integer","required":true,"description":"状态码","example":"200"},` +
		`{"fieldName":"data.list[].name","type":"string","required":false,"description":"","example":"n1"}]`
	em, _ := json.Marshal(map[string]any{
		"retType":   "object",
		"retParams": retRows,
		"retSample": `{"code":200}`,
	})
	r := &Request{}
	ApplySyncExtra(r, string(em))
	if r.RespDoc == nil || r.RespDoc.Type != "object" || r.RespDoc.Sample != `{"code":200}` {
		t.Fatalf("响应文档未还原: %+v", r.RespDoc)
	}
	wantRows := []ResponseParam{
		{FieldName: "code", Type: "integer", Required: true, Description: "状态码", Example: "200"},
		{FieldName: "data.list[].name", Type: "string", Example: "n1"},
	}
	if !reflect.DeepEqual(r.RespDoc.Params, wantRows) {
		t.Fatalf("retParams 还原不一致:\n got=%v\nwant=%v", r.RespDoc.Params, wantRows)
	}

	// 空串：清空对应部分；三键全空 ⇒ RespDoc 移除。
	ApplySyncExtra(r, `{"retType":"","retParams":"","retSample":""}`)
	if r.RespDoc != nil {
		t.Fatalf("三键空串应移除 RespDoc, got %+v", r.RespDoc)
	}

	// 缺键：各部分保持不动（兼容旧服务端/部分更新）。
	r2 := &Request{RespDoc: &ResponseDoc{Type: "array", Sample: `[1]`,
		Params: []ResponseParam{{FieldName: "id"}}}}
	ApplySyncExtra(r2, `{"retSample":"[2]" }`)
	if r2.RespDoc == nil || r2.RespDoc.Type != "array" || len(r2.RespDoc.Params) != 1 ||
		r2.RespDoc.Sample != "[2]" {
		t.Fatalf("缺键时应只改样例: %+v", r2.RespDoc)
	}

	// retParams 非法 JSON：保留旧行，不做半截覆盖。
	ApplySyncExtra(r2, `{"retParams":"{bad"}`)
	if len(r2.RespDoc.Params) != 1 || r2.RespDoc.Params[0].FieldName != "id" {
		t.Fatalf("非法 retParams 应保留旧值: %+v", r2.RespDoc.Params)
	}

	// gRPC：响应三键一律忽略。
	g := &Request{Method: MethodGRPC, GRPC: &GrpcBlock{}}
	ApplySyncExtra(g, string(em))
	if g.RespDoc != nil {
		t.Fatalf("gRPC 不应产生响应文档: %+v", g.RespDoc)
	}
}

func TestSyncExtra_ResponseDocRoundTripAndPersist(t *testing.T) {
	src := &Request{
		Method: "POST", URL: "/u", Docs: "说明",
		RespDoc: &ResponseDoc{
			Type: "object",
			Params: []ResponseParam{
				{FieldName: "code", Type: "integer", Required: true, Example: "200"},
				{FieldName: "data.items[].name", Type: "string"},
			},
			Sample: `{"code":200}`,
		},
	}
	got := &Request{Method: "POST", URL: "/u"}
	ApplySyncExtra(got, SyncExtra(src))
	if !reflect.DeepEqual(got.RespDoc, src.RespDoc) {
		t.Fatalf("响应文档往返不一致:\n got=%+v\nwant=%+v", got.RespDoc, src.RespDoc)
	}

	// 文件持久化：respdoc 段写盘后重新读回（文件即真相源，同步哈希才能覆盖该段）。
	c := openTemp(t)
	saved, err := c.CreateRequest("", "响应接口", "POST")
	if err != nil {
		t.Fatal(err)
	}
	saved.RespDoc = src.RespDoc
	if err := c.SaveRequest(saved); err != nil {
		t.Fatalf("SaveRequest: %v", err)
	}
	reloaded, err := c.ReadRequest(saved.UID)
	if err != nil {
		t.Fatalf("ReadRequest: %v", err)
	}
	if !reflect.DeepEqual(reloaded.RespDoc, src.RespDoc) {
		t.Fatalf("响应文档落盘往返不一致:\n got=%+v\nwant=%+v", reloaded.RespDoc, src.RespDoc)
	}
}

func TestApplySyncExtra_QueryAndNestedBody(t *testing.T) {
	// 服务端行：简单名 → query；含点号/[] → 还原 JSON 体（含数组对象与类型推断）。
	rows := []syncParamRow{
		{FieldName: "page", Type: "string", Description: "页码", Example: "2"},
		{FieldName: "ok", Type: "boolean", Example: "true"},
		{FieldName: "data", Type: "object"},
		{FieldName: "data.count", Type: "integer", Example: "3"},
		{FieldName: "data.rate", Type: "number", Example: "1.5"},
		{FieldName: "data.list", Type: "array"},
		{FieldName: "data.list[].id", Type: "integer", Example: "9"},
		{FieldName: "data.list[].name", Type: "string", Example: "n1"},
	}
	rb, _ := json.Marshal(rows)
	hb, _ := json.Marshal([]syncHeaderRow{{Name: "X-Token", Value: "secret"}})
	extraMap := map[string]any{
		"desc":    "来自服务端",
		"params":  string(rb),
		"headers": string(hb),
	}
	eb, _ := json.Marshal(extraMap)

	r := &Request{Body: Body{Type: "form", Form: []KV{{Name: "keep", Value: "1"}}}}
	ApplySyncExtra(r, string(eb))

	if r.Docs != "来自服务端" {
		t.Fatalf("docs=%q", r.Docs)
	}
	if !reflect.DeepEqual(r.Headers, []KV{{Name: "X-Token", Value: "secret", Enabled: true}}) {
		t.Fatalf("headers=%v", r.Headers)
	}
	// 简单行进 query（含描述与启用位）。
	if !reflect.DeepEqual(r.Params[:1], []KV{{Name: "page", Value: "2", Description: "页码", Enabled: true}}) {
		t.Fatalf("query 参数=%v", r.Params)
	}
	if r.Body.Type != "json" {
		t.Fatalf("嵌套行应还原为 json 体, got type=%q body=%+v", r.Body.Type, r.Body)
	}
	var got any
	if err := json.Unmarshal([]byte(r.Body.Raw), &got); err != nil {
		t.Fatalf("还原的 body 不是合法 JSON: %v", err)
	}
	want := map[string]any{
		"ok": true,
		"data": map[string]any{
			"count": float64(3),
			"rate":  1.5,
			"list":  []any{map[string]any{"id": float64(9), "name": "n1"}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("body 还原不一致:\n got=%v\nwant=%v", got, want)
	}
}

func TestApplySyncExtra_ClearAndPreserve(t *testing.T) {
	// params 空串：清空 query；JSON 体一起清，form 体保留（服务端不建模 form）。
	r := &Request{
		Params:  []KV{{Name: "old", Value: "1"}},
		Headers: []KV{{Name: "H", Value: "v"}},
		Body:    Body{Type: "json", Raw: `{"a":1}`},
		Docs:    "旧",
	}
	ApplySyncExtra(r, `{"desc":"","headers":"","params":""}`)
	if len(r.Params) != 0 || len(r.Headers) != 0 || r.Docs != "" {
		t.Fatalf("空串应清空, got params=%v headers=%v docs=%q", r.Params, r.Headers, r.Docs)
	}
	if r.Body.Type != "" || r.Body.Raw != "" {
		t.Fatalf("JSON 体应被清空, got %+v", r.Body)
	}

	form := &Request{Body: Body{Type: "multipart", Form: []KV{{Name: "f", Value: "x"}}}}
	ApplySyncExtra(form, `{"params":""}`)
	if form.Body.Type != "multipart" || len(form.Body.Form) != 1 {
		t.Fatalf("form/multipart 体不应被服务端状态清空, got %+v", form.Body)
	}

	// 缺键：对应字段保持不动（兼容旧服务端）。
	keep := &Request{Params: []KV{{Name: "k", Value: "v"}}, Docs: "保留"}
	ApplySyncExtra(keep, `{"desc":"新说明"}`)
	if len(keep.Params) != 1 || keep.Docs != "新说明" {
		t.Fatalf("缺 params 键时不应动参数, got %+v", keep)
	}

	// 非 JSON 对象（旧版快照曾把裸头文本塞进 extra）：整体忽略。
	before := &Request{Params: []KV{{Name: "k", Value: "v"}}, Docs: "d"}
	ApplySyncExtra(before, `[{"name":"x"}]`)
	if len(before.Params) != 1 || before.Docs != "d" {
		t.Fatalf("非法 extra 应整体忽略, got %+v", before)
	}
}

func TestSyncExtra_RoundTrip(t *testing.T) {
	src := &Request{
		Method:  "POST",
		URL:     "/u",
		Params:  []KV{{Name: "page", Value: "1", Description: "p", Enabled: true}},
		Headers: []KV{{Name: "H", Value: "v", Description: "d", Enabled: true}},
		Body:    Body{Type: "json", Raw: `{"addr":{"city":"SH"},"age":20}`},
		Docs:    "说明",
	}
	got := &Request{Method: "POST", URL: "/u"}
	ApplySyncExtra(got, SyncExtra(src))

	if !reflect.DeepEqual(got.Params, src.Params) {
		t.Fatalf("query 往返不一致:\n got=%v\nwant=%v", got.Params, src.Params)
	}
	if !reflect.DeepEqual(got.Headers, src.Headers) {
		t.Fatalf("headers 往返不一致:\n got=%v\nwant=%v", got.Headers, src.Headers)
	}
	if got.Docs != src.Docs {
		t.Fatalf("docs 往返不一致: %q vs %q", got.Docs, src.Docs)
	}
	var gb, sb any
	_ = json.Unmarshal([]byte(got.Body.Raw), &gb)
	_ = json.Unmarshal([]byte(src.Body.Raw), &sb)
	if !reflect.DeepEqual(gb, sb) {
		t.Fatalf("JSON 体往返不一致:\n got=%v\nwant=%v", gb, sb)
	}
}
