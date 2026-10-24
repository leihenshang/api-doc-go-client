package collection

// 同步载荷 extra 与客户端 Request 的双向映射（请求头 / 请求参数 / 说明）。
//
// 服务端 api 表把结构化数据存成两段 JSON 文本（见服务端 model.Api）：
//   - http_request_header：[{name,value,description}]
//   - http_request_params：[{fieldName,type,required,description,example}]
//
// 展平/还原口径必须与服务端 Bruno 导入（service/bruno.go buildBrunoApi）一致：
// query 参数逐行映射，JSON 请求体按键名字典序展平为 a.b / a[].c 形式的参数行；
// 反向时简单名参数行回到 query，含点号/下标的行还原为 JSON 请求体。
// 这样桌面客户端（真发请求）与 Web 文档端看到的「请求参数」语义一致，往返不漂移。

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// syncHeaderRow 请求头行（对齐服务端 apiHeaderRow / Web ApiHeader）。
type syncHeaderRow struct {
	Name        string `json:"name"`
	Value       string `json:"value"`
	Description string `json:"description"`
}

// syncParamRow 请求参数行（对齐服务端 apiParamRow / Web ApiParam）。
type syncParamRow struct {
	FieldName   string `json:"fieldName"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
	Example     string `json:"example"`
}

// SyncExtra 把请求的可同步字段聚合成服务端 extra JSON（SyncPayload.extra）。
// 始终输出 desc/summary/headers/params/retType/retParams/retSample 等键（无内容时为空串），
// 表示整份覆盖：服务端只在键存在且为字符串时覆盖对应列，空串即清空，删除能正常传播。
// gRPC 请求没有 HTTP 参数/头/响应文档，只同步说明与概述（扩展键同样不下发）。
//
// 扩展键（请求体 / 认证 / 请求设置 / 脚本 / 变量 / 断言 / 响应内容类型）见
// sync_extra_fields.go；HTTP 请求一律整份下发，保证「本地删掉的东西服务端也删掉」。
func SyncExtra(r *Request) string {
	extra := map[string]any{
		"desc":          r.Docs,
		extraKeySummary: r.Summary,
		"headers":       "",
		"params":        "",
		"retType":       "",
		"retParams":     "",
		"retSample":     "",
	}
	if r.IsGRPC() {
		b, _ := json.Marshal(extra)
		return string(b)
	}
	if s := marshalHeaderRows(r.Headers); s != "" {
		extra["headers"] = s
	}
	if s := marshalParamRows(r); s != "" {
		extra["params"] = s
	}
	extra[extraKeyRetContentType] = ""
	if d := r.RespDoc; d != nil {
		extra["retType"] = d.Type
		extra["retSample"] = d.Sample
		extra[extraKeyRetContentType] = d.ContentType
		if b, err := json.Marshal(d.Params); err == nil && len(d.Params) > 0 {
			extra["retParams"] = string(b)
		}
	}
	extra[extraKeyBodyType] = r.Body.Type
	extra[extraKeyBody] = bodyTextOf(r.Body)
	extra[extraKeyBodyForm] = marshalFormRows(r.Body.Form)
	extra[extraKeyBodyDescs] = marshalDescs(r.Body.Descs)
	extra[extraKeyBodyFields] = marshalBodyFields(r.Body.Fields)
	extra[extraKeyAuth] = marshalAuth(r.Auth)
	extra[extraKeySettings] = marshalSettings(r.Settings)
	extra[extraKeyScript] = marshalScript(r.Script)
	extra[extraKeyVars] = marshalRows(r.VarsPreRequest)
	extra[extraKeyAsserts] = marshalRows(r.Asserts)
	b, _ := json.Marshal(extra)
	return string(b)
}

// ApplySyncExtra 应用服务端 extra JSON（拉取 / 冲突时「采用服务器版本」）。
// 空串或非 JSON 对象（如旧版服务端误塞的裸文本）一律忽略；键不存在的字段保持不动，
// 以兼容只下发部分字段的旧服务端。
//
// summary（API 概述）在 gRPC 早退之前应用：gRPC 请求没有 HTTP 参数/头/请求体，
// 但概述两端都要有，放在早退之后就永远同步不过来。
func ApplySyncExtra(r *Request, extraJSON string) {
	raw := strings.TrimSpace(extraJSON)
	if raw == "" {
		return
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return
	}
	if v, ok := m["desc"].(string); ok {
		r.Docs = v
	}
	if v, ok := m[extraKeySummary].(string); ok {
		r.Summary = v
	}
	if r.IsGRPC() {
		return
	}
	if v, ok := m["headers"].(string); ok {
		r.Headers = parseHeaderRows(v)
	}
	if v, ok := m["params"].(string); ok {
		applyParamRows(r, v)
	}
	applyResponseDoc(r, m)
	applyExtendedFields(r, m)
}

// applyResponseDoc 应用响应文档三键（retType/retParams/retSample）：
// 键存在才覆盖对应部分（空串=清空该部分），三键皆缺不建文档，清空到全空时移除 RespDoc。
// 返回参数行与 Web 端同形（fieldName 已为嵌套路径），原样存取、不展平。
func applyResponseDoc(r *Request, m map[string]any) {
	_, hasType := m["retType"]
	_, hasParams := m["retParams"]
	_, hasSample := m["retSample"]
	_, hasContentType := m[extraKeyRetContentType]
	if !hasType && !hasParams && !hasSample && !hasContentType {
		return
	}
	doc := r.RespDoc
	if doc == nil {
		doc = &ResponseDoc{}
	}
	if v, ok := m["retType"].(string); ok {
		doc.Type = v
	}
	if v, ok := m[extraKeyRetContentType].(string); ok {
		doc.ContentType = v
	}
	if v, ok := m["retParams"].(string); ok {
		doc.Params = parseResponseParams(v, doc.Params)
	}
	if v, ok := m["retSample"].(string); ok {
		if doc.Sample != v {
			// 样例被外部（服务端）改写：本地随样例存的响应快照（状态码/响应头）不再对应，一并清掉
			doc.Status, doc.Headers = 0, nil
		}
		doc.Sample = v
	}
	if doc.Type == "" && len(doc.Params) == 0 && doc.Sample == "" && doc.ContentType == "" {
		r.RespDoc = nil
		return
	}
	r.RespDoc = doc
}

// parseResponseParams 行 JSON → 返回参数行；空串清空，解析失败保留旧值（避免半截覆盖）。
func parseResponseParams(s string, keep []ResponseParam) []ResponseParam {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var rows []ResponseParam
	if json.Unmarshal([]byte(s), &rows) != nil {
		return keep
	}
	return rows
}

// marshalHeaderRows KV 头 → 行 JSON；空数组返回空串（让服务端列保持未设置）。
func marshalHeaderRows(headers []KV) string {
	if len(headers) == 0 {
		return ""
	}
	rows := make([]syncHeaderRow, 0, len(headers))
	for _, h := range headers {
		rows = append(rows, syncHeaderRow{Name: h.Name, Value: h.Value, Description: h.Description})
	}
	b, err := json.Marshal(rows)
	if err != nil {
		return ""
	}
	return string(b)
}

// marshalParamRows query 参数 + JSON 请求体展平 → 行 JSON。
func marshalParamRows(r *Request) string {
	rows := make([]syncParamRow, 0, len(r.Params))
	for _, p := range r.Params {
		rows = append(rows, syncParamRow{
			FieldName:   p.Name,
			Type:        "string",
			Description: p.Description,
			Example:     p.Value,
		})
	}
	if raw := strings.TrimSpace(bodyContent(r.Body)); strings.EqualFold(r.Body.Type, "json") && raw != "" {
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err == nil {
			rows = flattenJSON("", v, rows)
		}
	}
	if len(rows) == 0 {
		return ""
	}
	b, err := json.Marshal(rows)
	if err != nil {
		return ""
	}
	return string(b)
}

// bodyContent 请求体原文：兼容 Bruno 的 data 写法与本客户端的 raw。
func bodyContent(b Body) string {
	if strings.TrimSpace(b.Data) != "" {
		return b.Data
	}
	return b.Raw
}

// flattenJSON 把 JSON 值展平为 a.b / a[].c 形式的参数行。
// 与服务端 service/bruno.go.flattenJSON 逐行同口径（对象按键名字典序，数组按首元素推断）。
func flattenJSON(prefix string, v any, out []syncParamRow) []syncParamRow {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			out = append(out, syncParamRow{FieldName: p, Type: jsonTypeName(t[k]), Example: jsonExample(t[k])})
			out = flattenJSON(p, t[k], out)
		}
	case []any:
		if prefix == "" || len(t) == 0 {
			return out
		}
		if m, ok := t[0].(map[string]any); ok {
			out = flattenJSON(prefix+"[]", m, out)
		}
	}
	return out
}

// jsonTypeName 返回 JSON 值的类型名（整数与浮点区分），与服务端同口径。
func jsonTypeName(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64:
		if t == float64(int64(t)) {
			return "integer"
		}
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "string"
}

// jsonExample 取标量示例值（对象/数组不填）。
func jsonExample(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	}
	return ""
}

// parseHeaderRows 行 JSON → KV 头；空串清空，解析失败不动（避免半截数据覆盖本地）。
func parseHeaderRows(s string) []KV {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var rows []syncHeaderRow
	if json.Unmarshal([]byte(s), &rows) != nil {
		return nil
	}
	out := make([]KV, 0, len(rows))
	for _, row := range rows {
		out = append(out, KV{Name: row.Name, Value: row.Value, Description: row.Description, Enabled: true})
	}
	return out
}

// applyParamRows 行 JSON → query 参数；嵌套行还原为 JSON 请求体。
func applyParamRows(r *Request, s string) {
	if strings.TrimSpace(s) == "" {
		r.Params = nil
		// 服务端不建模 form/multipart 体：只清掉它能表达的 JSON 体，其余本地体保留。
		if strings.EqualFold(r.Body.Type, "json") {
			r.Body = Body{}
		}
		return
	}
	var rows []syncParamRow
	if json.Unmarshal([]byte(s), &rows) != nil {
		return
	}
	// 服务端参数表不区分位置（query 与 JSON 体展平混在同一张表，见服务端 Bruno 导入），
	// 还原只能用启发式：客户端推送时 query 行恒在前、JSON 体展平行（含点号/下标，或
	// 布尔/数值等非 string 类型）紧随其后且顺序稳定。因此找到第一条「体行」的位置：
	// 它之前的简单 string 行回 query，其后所有行进 JSON 体。Web 端手工重排过的极端
	// 情况下尽力而为；纯字符串标量组成的 JSON 体无法与 query 区分（服务端模型本就无位置信息）。
	bodyStart := -1
	for i, row := range rows {
		if strings.ContainsAny(row.FieldName, ".[") || !isStringish(row.Type) {
			bodyStart = i
			break
		}
	}
	params := make([]KV, 0, len(rows))
	var bodyRows []syncParamRow
	for i, row := range rows {
		if bodyStart >= 0 && i >= bodyStart {
			bodyRows = append(bodyRows, row)
			continue
		}
		params = append(params, KV{
			Name: row.FieldName, Value: row.Example, Description: row.Description, Enabled: true,
		})
	}
	r.Params = params
	switch {
	case len(bodyRows) > 0:
		if b, err := json.Marshal(unflattenRows(bodyRows)); err == nil {
			r.Body = Body{Type: "json", Raw: string(b)}
		}
	case strings.EqualFold(r.Body.Type, "json"):
		// 服务端已无 JSON 体（展平行消失），清掉本地 JSON 体；其它类型的体服务端不管，保留。
		r.Body = Body{}
	}
}

// isStringish 行类型是否按字符串处理（空类型按字符串，兼容手填/旧数据）。
func isStringish(typ string) bool {
	return typ == "" || strings.EqualFold(typ, "string")
}

// unflattenRows 把 a.b / a[].c 形式的参数行还原为 JSON 值（flattenJSON 的逆操作）。
// 容器行（object/array）只负责补结构，绝不覆盖已被叶子/其它路径填好的内容。
func unflattenRows(rows []syncParamRow) any {
	root := map[string]any{}
	for _, row := range rows {
		tokens := strings.Split(row.FieldName, ".")
		cur := root
		for i, token := range tokens {
			isArr := strings.HasSuffix(token, "[]")
			key := strings.TrimSuffix(token, "[]")
			last := i == len(tokens)-1
			if isArr {
				arr := ensureArray(cur, key)
				if last {
					// 标量数组兜底（常规展平不会产生，防御性处理）
					if len(arr) == 0 {
						cur[key] = []any{typedLeaf(row)}
					}
					break
				}
				cur = ensureArrayElemMap(arr, cur, key)
				continue
			}
			if last {
				if _, exists := cur[key]; !exists {
					cur[key] = typedLeaf(row)
				}
				break
			}
			cur = ensureObject(cur, key)
		}
	}
	return root
}

// ensureArray 取/建键下的数组（若该键已被其它路径占用则原样返回，不覆盖）。
func ensureArray(m map[string]any, key string) []any {
	if arr, ok := m[key].([]any); ok {
		return arr
	}
	arr := []any{}
	m[key] = arr
	return arr
}

// ensureArrayElemMap 取数组首元素（对象），返回该对象供继续下行。
func ensureArrayElemMap(arr []any, parent map[string]any, key string) map[string]any {
	if len(arr) == 0 {
		elem := map[string]any{}
		parent[key] = []any{elem}
		return elem
	}
	if elem, ok := arr[0].(map[string]any); ok {
		return elem
	}
	elem := map[string]any{}
	parent[key] = []any{elem}
	return elem
}

// ensureObject 取/建键下的对象；已有非对象值（如叶子先到）则保留不动。
func ensureObject(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	next := map[string]any{}
	m[key] = next
	return next
}

// typedLeaf 按行类型把 example 转成标量；object/array 行给空容器。
func typedLeaf(row syncParamRow) any {
	switch row.Type {
	case "object":
		return map[string]any{}
	case "array":
		return []any{}
	case "null":
		return nil
	case "boolean":
		b, _ := strconv.ParseBool(row.Example)
		return b
	case "integer":
		n, err := strconv.ParseInt(row.Example, 10, 64)
		if err != nil {
			return 0
		}
		return n
	case "number":
		f, err := strconv.ParseFloat(row.Example, 64)
		if err != nil {
			return 0
		}
		return f
	default:
		return row.Example
	}
}
