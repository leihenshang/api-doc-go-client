package collection

// 请求体字段表格（Body.Fields）与 API 概述（Summary）在客户端内存模型与磁盘形态之间的往返。
// 两处都必须逐字段相等：落盘时漏掉一个字段，用户手填的表格/概述就凭空消失。

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// decodeRequestFile 解码请求文件（走共享包的 UnmarshalYAML，与读盘路径同口径）。
func decodeRequestFile(t *testing.T, data []byte) *requestFile {
	t.Helper()
	var f requestFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		t.Fatalf("解码请求文件: %v", err)
	}
	return &f
}

// TestToFromFileKeepsSummaryAndFields toFile → Encode → DecodeRequest → fromFile 后 summary 与
// fields 逐字段相等（含 required=false、空 example / 空 description）。
func TestToFromFileKeepsSummaryAndFields(t *testing.T) {
	src := &Request{
		UID:     "6f1c2a9e",
		Name:    "创建订单",
		Seq:     3,
		Summary: "下单接口，创建一笔待支付订单",
		Method:  "POST",
		URL:     "{{host}}/api/v1/orders",
		Body: Body{
			Type:  "json",
			Raw:   `{"uid":"u-42"}`,
			Descs: map[string]string{"items[].sku": "商品编码"},
			Fields: []BodyField{
				{Name: "uid", Type: "string", Required: true, Example: "u-42", Description: "用户ID"},
				{Name: "items[].sku", Type: "string", Required: false, Example: "A-1"},
				{Name: "page", Type: "integer"},
			},
		},
		Docs: "## 简要描述\n- 下单\n",
	}

	data, err := yaml.Marshal(src.toFile())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := fromFile("orders.yml", decodeRequestFile(t, data))

	if got.Summary != src.Summary {
		t.Errorf("summary 往返不一致: got %q want %q", got.Summary, src.Summary)
	}
	if len(got.Body.Fields) != len(src.Body.Fields) {
		t.Fatalf("fields 行数: got %d want %d", len(got.Body.Fields), len(src.Body.Fields))
	}
	for i, want := range src.Body.Fields {
		if got.Body.Fields[i] != want {
			t.Errorf("fields[%d]: got %+v want %+v", i, got.Body.Fields[i], want)
		}
	}
	if got.Body.Descs["items[].sku"] != "商品编码" {
		t.Errorf("descs 往返丢失: %+v", got.Body.Descs)
	}
	// 落盘形状：summary 在顶层，fields 在 http.body 下，required 显式写出。
	text := string(data)
	for _, want := range []string{"summary: 下单接口", "fields:", "required: false"} {
		if !strings.Contains(text, want) {
			t.Errorf("落盘缺少 %q:\n%s", want, text)
		}
	}
}

// TestFieldsOnlyRequestKeepsHTTPBlock 只有表格（无 type/raw/descs）的请求在客户端落盘往返后表格仍在：
// 共享包 HTTPBlock.IsZero 漏判 Fields 时，http 段会被 omitempty 整段丢掉。
func TestFieldsOnlyRequestKeepsHTTPBlock(t *testing.T) {
	src := &Request{
		Name:    "只有表格",
		Summary: "只有表格的接口",
		Method:  "POST",
		URL:     "{{host}}/api/v1/x",
		Body: Body{
			Fields: []BodyField{{Name: "uid", Type: "string", Required: true, Example: "u-42", Description: "用户ID"}},
		},
	}
	data, err := yaml.Marshal(src.toFile())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(data), "http:") {
		t.Fatalf("只有 fields 时 http 段被整段丢弃:\n%s", data)
	}
	got := fromFile("only-fields.yml", decodeRequestFile(t, data))
	if len(got.Body.Fields) != 1 || got.Body.Fields[0].Name != "uid" {
		t.Fatalf("只有 fields 的请求往返丢失表格: %+v", got.Body.Fields)
	}
	if got.Summary != "只有表格的接口" {
		t.Fatalf("summary 往返丢失: %q", got.Summary)
	}
}

// TestFromFileLegacySample 老文件（无 summary / fields）解码后为零值，且不因新字段报错。
func TestFromFileLegacySample(t *testing.T) {
	_, r := readSample(t)
	if r.Summary != "" {
		t.Errorf("老文件 summary 应为空: %q", r.Summary)
	}
	if len(r.Body.Fields) != 0 {
		t.Errorf("老文件 fields 应为空: %+v", r.Body.Fields)
	}
}

// TestCreateRequestFromDraftKeepsSummary MCP create 走草稿落盘：漏搬 Summary 会导致
// create_request 传的概述无效（C4）。
func TestCreateRequestFromDraftKeepsSummary(t *testing.T) {
	c := openTemp(t)
	src := &Request{
		Summary: "草稿概述",
		Method:  "POST",
		URL:     "{{host}}/api/v1/x",
		Body: Body{
			Type:   "json",
			Fields: []BodyField{{Name: "uid", Type: "string", Required: true}},
		},
	}
	got, err := c.CreateRequestFromDraft("", "草稿请求", src)
	if err != nil {
		t.Fatalf("CreateRequestFromDraft: %v", err)
	}
	if got.Summary != "草稿概述" {
		t.Errorf("草稿摘要未保留: %q", got.Summary)
	}
	if len(got.Body.Fields) != 1 || got.Body.Fields[0].Name != "uid" {
		t.Errorf("草稿表格未保留: %+v", got.Body.Fields)
	}
	// 重新读盘确认真的写进文件了。
	reloaded, err := c.ReadRequest(got.UID)
	if err != nil {
		t.Fatalf("LoadRequest: %v", err)
	}
	if reloaded.Summary != "草稿概述" {
		t.Errorf("落盘后 summary 丢失: %q", reloaded.Summary)
	}
	if len(reloaded.Body.Fields) != 1 {
		t.Errorf("落盘后表格丢失: %+v", reloaded.Body.Fields)
	}
}
