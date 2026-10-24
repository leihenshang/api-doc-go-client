package collection

// 同步 extra 的扩展键集：请求体 / 认证 / 请求设置 / 脚本 / 断言 / 响应内容类型。
//
// 与服务端 service/sync.go 的 apiSyncPayload、applyUpsert 必须逐字一致（键名即契约）。
// 语义沿用既有 extra 的「整份覆盖」：键存在且为字符串才覆盖，空串清空，缺键不动 ——
// 于是旧客户端推旧键集不会清空服务端新列，旧服务端下发缺新键的 extra 也不会清空本地字段。
//
// 未接入项：（无）。bodyDescs（请求体字段备注）依赖共享包 Body.Descs，随其发版后已接入，
// 与服务端 http_request_body_descs 列双向往返；bodyFields（请求体字段表格）同法接入
// http_request_body_fields，summary（API 概述）接入 http_request_summary。

import (
	"encoding/json"
	"strings"
)

const (
	extraKeyBodyType       = "bodyType"       // none|json|text|form|multipart
	extraKeyBody           = "body"           // json/text 原文
	extraKeyBodyForm       = "bodyForm"       // form/multipart 行 JSON
	extraKeyBodyDescs      = "bodyDescs"      // {"a.b":"备注"}（JSON 体字段备注，随 Body.Descs 往返）
	extraKeyBodyFields     = "bodyFields"     // [BodyField]（请求体字段表格，随 Body.Fields 往返）
	extraKeySummary        = "summary"        // API 概述（纯文本）
	extraKeyAuth           = "auth"           // {type,username,password,token,key,value,in}
	extraKeySettings       = "settings"       // {timeout,followRedirects,maxRedirects,insecureSsl,encodeUrl}
	extraKeyScript         = "script"         // {preRequest,postResponse}
	extraKeyVars           = "vars"           // [{name,value,enabled}]
	extraKeyAsserts        = "asserts"        // [{name,expr}]
	extraKeyRetContentType = "retContentType" // json|text|xml|html|binary
)

// syncSettingsRow 请求设置行：三态布尔落 smallint（1=true / 0=false / -1=未设置）。
// FollowRedirects / InsecureSSL 在本地是 *bool，退化成 bool 会丢掉「未设置 → 沿用全局设置」这一档。
type syncSettingsRow struct {
	Timeout         int   `json:"timeout,omitempty"`
	FollowRedirects int8  `json:"followRedirects"`
	MaxRedirects    int   `json:"maxRedirects,omitempty"`
	InsecureSSL     int8  `json:"insecureSsl"`
	EncodeURL       *bool `json:"encodeUrl,omitempty"`
}

// syncFormRow form/multipart 行（对齐服务端 http_request_body_form）。
type syncFormRow struct {
	Name        string `json:"name"`
	Value       string `json:"value,omitempty"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type,omitempty"`
}

// ---- 通用编解码（行集合 / 指针，空=清空、失败=保留旧值）----

// marshalRows 行切片 → JSON 字符串；空切片返回空串（服务端据此清空对应列）。
func marshalRows[T any](rows []T) string {
	if len(rows) == 0 {
		return ""
	}
	b, err := json.Marshal(rows)
	if err != nil {
		return ""
	}
	return string(b)
}

// parseRows 行 JSON → 切片；空串清空，解析失败保留旧值（避免半截数据覆盖本地）。
func parseRows[T any](s string, keep []T) []T {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var rows []T
	if json.Unmarshal([]byte(s), &rows) != nil {
		return keep
	}
	return rows
}

// marshalPtr 单个结构体 → JSON 字符串；nil 返回空串。
func marshalPtr[T any](v *T) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// parsePtr 行 JSON → 单个结构体；空串清空，解析失败保留旧值。
func parsePtr[T any](s string, keep *T) *T {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var v T
	if json.Unmarshal([]byte(s), &v) != nil {
		return keep
	}
	return &v
}

// ---- 三态布尔 ----

// triBoolToInt *bool → smallint：nil（未设置）为 -1。
func triBoolToInt(b *bool) int8 {
	if b == nil {
		return -1
	}
	if *b {
		return 1
	}
	return 0
}

// intToTriBool smallint → *bool：-1 回落 nil（沿用全局设置）。
func intToTriBool(v int8) *bool {
	switch v {
	case 1:
		t := true
		return &t
	case 0:
		f := false
		return &f
	default:
		return nil
	}
}

// ---- 认证 / 脚本 / 变量 / 断言（字段与本地类型 JSON 名一致，直接编解码）----

// marshalAuth 认证 → 行 JSON；未设置类型时返回空串（清空服务端认证列）。
func marshalAuth(a *Auth) string {
	if a == nil || strings.TrimSpace(a.Type) == "" {
		return ""
	}
	return marshalPtr(a)
}

// parseAuth 行 JSON → 认证；空串清空，解析失败保留旧值。
func parseAuth(s string, keep *Auth) *Auth {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return parsePtr(s, keep)
}

// marshalScript 脚本段 → 行 JSON；两段皆空时返回空串。
func marshalScript(b *ScriptBlock) string {
	if b == nil || (strings.TrimSpace(b.PreRequest) == "" && strings.TrimSpace(b.PostResponse) == "") {
		return ""
	}
	return marshalPtr(b)
}

// parseScript 行 JSON → 脚本段；空串清空，解析失败保留旧值。
func parseScript(s string, keep *ScriptBlock) *ScriptBlock {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return parsePtr(s, keep)
}

// ---- 请求设置（需 *bool ↔ smallint 转换）----

// marshalSettings 请求设置 → 行 JSON。
func marshalSettings(s *RequestSettings) string {
	if s == nil {
		return ""
	}
	return marshalPtr(&syncSettingsRow{
		Timeout:         s.TimeoutSec,
		FollowRedirects: triBoolToInt(s.FollowRedirects),
		MaxRedirects:    s.MaxRedirects,
		InsecureSSL:     triBoolToInt(s.InsecureSSL),
		EncodeURL:       s.EncodeURL,
	})
}

// parseSettings 行 JSON → 请求设置；空串清空，解析失败保留旧值。
func parseSettings(s string, keep *RequestSettings) *RequestSettings {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var row syncSettingsRow
	if json.Unmarshal([]byte(s), &row) != nil {
		return keep
	}
	return &RequestSettings{
		TimeoutSec:      row.Timeout,
		FollowRedirects: intToTriBool(row.FollowRedirects),
		MaxRedirects:    row.MaxRedirects,
		InsecureSSL:     intToTriBool(row.InsecureSSL),
		EncodeURL:       row.EncodeURL,
	}
}

// bodyTextOf 只有 json/text 体的原文走 extra 的 body 键：
// form/multipart 的内容由 bodyForm 的行承载，若也塞进 body 会在服务端重复落两处。
func bodyTextOf(b Body) string {
	if strings.EqualFold(b.Type, "json") || strings.EqualFold(b.Type, "text") {
		return bodyContent(b)
	}
	return ""
}

// applyBodyText 把 extra 的 body 原文写回请求体；写回字段与 bodyContent 的读取偏好一致
// （Bruno 的 data 优先，否则写 raw），避免往返后落盘位置漂移。
func applyBodyText(r *Request, s string) {
	if !strings.EqualFold(r.Body.Type, "json") && !strings.EqualFold(r.Body.Type, "text") {
		return
	}
	if strings.TrimSpace(r.Body.Data) != "" {
		r.Body.Data = s
		return
	}
	r.Body.Raw = s
}

// applyExtendedFields 应用扩展键（请求体 / 认证 / 请求设置 / 脚本 / 变量 / 断言）。
// 每个键只在「存在且为字符串」时覆盖：空串清空、缺键不动 ——
// 这样只下发部分字段的旧服务端不会把本地这些字段清掉。
func applyExtendedFields(r *Request, m map[string]any) {
	if v, ok := m[extraKeyBodyType].(string); ok {
		r.Body.Type = v
	}
	if v, ok := m[extraKeyBody].(string); ok {
		applyBodyText(r, v)
	}
	if v, ok := m[extraKeyBodyForm].(string); ok {
		r.Body.Form = parseFormRows(v, r.Body.Form)
	}
	if v, ok := m[extraKeyBodyDescs].(string); ok {
		r.Body.Descs = parseDescs(v, r.Body.Descs)
	}
	if v, ok := m[extraKeyBodyFields].(string); ok {
		r.Body.Fields = parseRows[BodyField](v, r.Body.Fields)
	}
	if v, ok := m[extraKeyAuth].(string); ok {
		r.Auth = parseAuth(v, r.Auth)
	}
	if v, ok := m[extraKeySettings].(string); ok {
		r.Settings = parseSettings(v, r.Settings)
	}
	if v, ok := m[extraKeyScript].(string); ok {
		r.Script = parseScript(v, r.Script)
	}
	if v, ok := m[extraKeyVars].(string); ok {
		r.VarsPreRequest = parseRows(v, r.VarsPreRequest)
	}
	if v, ok := m[extraKeyAsserts].(string); ok {
		r.Asserts = parseRows(v, r.Asserts)
	}
}

// ---- form / multipart 行（KV ↔ syncFormRow 字段名对齐）----

// marshalFormRows 表单行 → 行 JSON。
func marshalFormRows(form []KV) string {
	if len(form) == 0 {
		return ""
	}
	rows := make([]syncFormRow, 0, len(form))
	for _, f := range form {
		rows = append(rows, syncFormRow{
			Name: f.Name, Value: f.Value, Enabled: f.Enabled, Description: f.Description, Type: f.Type,
		})
	}
	return marshalRows(rows)
}

// parseFormRows 行 JSON → 表单行；空串清空，解析失败保留旧值。
func parseFormRows(s string, keep []KV) []KV {
	rows := parseRows(s, []syncFormRow(nil))
	if rows == nil {
		if strings.TrimSpace(s) == "" {
			return nil // 显式清空
		}
		return keep // 解析失败
	}
	out := make([]KV, 0, len(rows))
	for _, r := range rows {
		out = append(out, KV{
			Name: r.Name, Value: r.Value, Enabled: r.Enabled, Description: r.Description, Type: r.Type,
		})
	}
	return out
}

// ---- JSON 体字段备注（Body.Descs：path → 备注）----

// marshalDescs 字段备注 map → JSON 字符串；空 map 返回空串（服务端据此清空该列）。
func marshalDescs(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

// parseDescs JSON 字符串 → 字段备注 map；空串清空，解析失败保留旧值（避免半截覆盖）。
func parseDescs(s string, keep map[string]string) map[string]string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var m map[string]string
	if json.Unmarshal([]byte(s), &m) != nil {
		return keep
	}
	return m
}

// ---- 请求体字段表格（Body.Fields → BodyField 行 JSON）----

// marshalBodyFields 字段表格 → 行 JSON；空表格返回空串（服务端据此清空该列）。
func marshalBodyFields(fields []BodyField) string {
	return marshalRows(fields)
}
