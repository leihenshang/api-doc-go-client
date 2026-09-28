package collection

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	shopenapi "github.com/zqstudio/api-doc-go-share/openapi"
	shpostman "github.com/zqstudio/api-doc-go-share/postman"
)

// hostVar 导入 OpenAPI（路径为相对路径）时补的宿主变量前缀。
const hostVar = "{{host}}"

// ImportSummary 一次导入的结果，供上层提示。
type ImportSummary struct {
	Imported int      `json:"imported"`
	Skipped  int      `json:"skipped"`
	Failures []string `json:"failures,omitempty"`
}

func (s *ImportSummary) addFailure(msg string) {
	if len(s.Failures) < 10 { // 明细最多留 10 条，避免提示过长
		s.Failures = append(s.Failures, msg)
	}
}

// ImportPostman 把 Postman Collection JSON 导入到 parent 分组下：
// 目录映射为分组、请求落盘；已存在「同方法同地址」的请求跳过。
func (c *Collection) ImportPostman(parent string, data []byte) (*ImportSummary, error) {
	col, err := shpostman.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("Postman 集合解析失败: %w", err)
	}
	if len(col.Items) == 0 {
		return nil, errors.New("集合里没有请求（item 为空）")
	}
	sum := &ImportSummary{}
	keys := c.existingKeys()
	if err := c.importPostmanItems(parent, col.Items, keys, sum); err != nil {
		return nil, err
	}
	return sum, nil
}

// ImportOpenAPI 把 OpenAPI JSON 导入到 parent 分组下：
// tag 作为子分组、路径补 {{host}} 前缀（见 hostVar），query 参数进参数表。
func (c *Collection) ImportOpenAPI(parent string, data []byte) (*ImportSummary, error) {
	doc, err := shopenapi.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("OpenAPI 解析失败: %w", err)
	}
	if len(doc.Ops) == 0 {
		return nil, errors.New("文档里没有可导入的接口（paths 为空）")
	}
	sum := &ImportSummary{}
	keys := c.existingKeys()
	for _, op := range doc.Ops {
		dir := parent
		if len(op.Tags) > 0 {
			if sub, err := c.ensureImportFolder(parent, op.Tags[0]); err != nil {
				return nil, err
			} else if sub != "" {
				dir = sub
			}
		}
		if err := c.importOperation(dir, op, keys, sum); err != nil {
			return nil, err
		}
	}
	return sum, nil
}

// importPostmanItems 递归导入 Postman 树。
func (c *Collection) importPostmanItems(parent string, items []shpostman.Item, keys map[string]struct{}, sum *ImportSummary) error {
	for _, it := range items {
		if it.Folder {
			dir, err := c.ensureImportFolder(parent, it.Name)
			if err != nil {
				return err
			}
			if err := c.importPostmanItems(dir, it.Items, keys, sum); err != nil {
				return err
			}
			continue
		}
		if it.Req != nil {
			c.importPostmanRequest(parent, it, keys, sum)
		}
	}
	return nil
}

// importPostmanRequest 导入单个中立请求；名称/地址缺失等单条问题只记明细，不中断整次导入。
func (c *Collection) importPostmanRequest(parent string, it shpostman.Item, keys map[string]struct{}, sum *ImportSummary) {
	req := it.Req
	name := strings.TrimSpace(it.Name)
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	rawURL := strings.TrimSpace(req.URL)
	if method == "" || rawURL == "" {
		sum.addFailure(name + "：缺少 method 或 url")
		return
	}
	if name == "" {
		name = method + " " + rawURL
	}
	r := &Request{
		Method:  method,
		URL:     rawURL,
		Params:  queryRows(rawURL),
		Headers: headerRows(req.Headers),
		Body:    bodyOf(req.BodyRaw),
		Docs:    req.Description,
	}
	c.saveImported(parent, name, r, keys, sum)
}

// importOperation 导入单个 OpenAPI 操作（同样单条失败不中断）。
func (c *Collection) importOperation(parent string, op shopenapi.Operation, keys map[string]struct{}, sum *ImportSummary) error {
	method := strings.ToUpper(strings.TrimSpace(op.Method))
	rawURL := operationURL(op.Path)
	name := firstNonEmpty(op.Summary, op.OperationID, method+" "+op.Path)
	if method == "" || rawURL == "" {
		sum.addFailure(name + "：缺少 method 或 path")
		return nil
	}
	r := &Request{
		Method: method,
		URL:    rawURL,
		Params: paramRows(op.Params),
		Body:   Body{Type: "none"},
		Docs:   op.Description,
	}
	c.saveImported(parent, name, r, keys, sum)
	return nil
}

// saveImported 去重后落盘，并累计导入结果。
func (c *Collection) saveImported(parent, name string, r *Request, keys map[string]struct{}, sum *ImportSummary) {
	key := requestKey(r.Method, r.URL)
	if _, dup := keys[key]; dup {
		sum.Skipped++
		return
	}
	if _, err := c.saveNewRequest(parent, name, r); err != nil {
		sum.addFailure(name + "：" + err.Error())
		return
	}
	keys[key] = struct{}{}
	sum.Imported++
}

// ensureImportFolder 确保导入用的子分组存在（空名兜底「未命名目录」）。
func (c *Collection) ensureImportFolder(parent, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return parent, nil
	}
	dir := joinRel(parent, sanitizeFileName(name))
	if err := os.MkdirAll(filepath.Join(c.Dir, filepath.FromSlash(dir)), 0o755); err != nil {
		return "", err
	}
	if _, ok := c.folderUID(dir); !ok {
		if err := c.ensureFolderYML(dir, name); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// existingKeys 集合内已有请求的「方法 + 地址」去重键。
func (c *Collection) existingKeys() map[string]struct{} {
	res, err := c.scan()
	if err != nil {
		return map[string]struct{}{}
	}
	keys := make(map[string]struct{}, len(res.reqs))
	for _, r := range res.reqs {
		keys[requestKey(r.Method, r.URL)] = struct{}{}
	}
	return keys
}

func requestKey(method, rawURL string) string {
	return strings.ToUpper(strings.TrimSpace(method)) + " " + strings.TrimSpace(rawURL)
}

// queryRows URL 上的查询串 → 参数行（保留原文，不重复编码）。
func queryRows(rawURL string) []KV {
	var rows []KV
	for _, kv := range shpostman.QueryPairs(rawURL) {
		rows = append(rows, KV{Name: kv[0], Value: kv[1], Enabled: true})
	}
	return rows
}

func headerRows(headers []shpostman.Header) []KV {
	rows := make([]KV, 0, len(headers))
	for _, h := range headers {
		rows = append(rows, KV{Name: h.Name, Value: h.Value, Enabled: true})
	}
	return rows
}

// paramRows OpenAPI 参数 → 参数行（只取 query 与未标注位置的参数）。
func paramRows(params []shopenapi.Param) []KV {
	var rows []KV
	for _, p := range params {
		if p.In != "" && !strings.EqualFold(p.In, "query") {
			continue
		}
		rows = append(rows, KV{Name: p.Name, Value: p.Example, Enabled: true})
	}
	return rows
}

// operationURL OpenAPI 的 path 转本地请求地址：相对路径补 {{host}}，完整地址原样保留。
func operationURL(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	if strings.Contains(p, "://") || strings.HasPrefix(p, "{{") {
		return p
	}
	return hostVar + "/" + strings.TrimPrefix(p, "/")
}

// bodyOf 请求体映射：空则不发送，能解析为 JSON 按 json，否则按纯文本。
func bodyOf(raw string) Body {
	raw = strings.TrimSpace(raw)
	switch {
	case raw == "":
		return Body{Type: "none"}
	case json.Valid([]byte(raw)):
		return Body{Type: "json", Raw: raw}
	default:
		return Body{Type: "text", Raw: raw}
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
