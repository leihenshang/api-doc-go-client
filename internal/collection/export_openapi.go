package collection

// U4：把集合导出为 OpenAPI 3.0（JSON）。
//
// 为什么用共享库的 openapi.Document：服务端导入（ImportOpenAPI）与契约 diff 用的是同一套
// 中立结构，导出走同一个编码器才能保证「客户端导出 → Web 导入」不丢字段、不产生口径分叉。

import (
	"encoding/json"
	"strconv"
	"strings"

	shopenapi "github.com/leihenshang/api-doc-go-share/openapi"
)

// ExportOpenAPI 导出 OpenAPI 3.0 文档（只含 HTTP 请求；gRPC 不在 OpenAPI 表达范围内）。
//
// 取舍：
//   - 路径只取 URL 的 path 部分（去掉 host 与 {{变量}} 前缀、去掉 query），
//     因为 OpenAPI 的 path 必须是稳定模板；
//   - 查询参数/请求头映射为 parameters（query / header），响应样例映射为 200（或
//     respDoc 里记录的状态码）的 example；
//   - 请求体（JSON 原文）映射为 requestBody？—— 共享编码器当前不支持 requestBody
//     （导入侧也只解析参数与响应），所以这里不写，避免产出对方读不懂的键。
func (c *Collection) ExportOpenAPI() ([]byte, error) {
	res, err := c.scan()
	if err != nil {
		return nil, err
	}
	doc := &shopenapi.Document{Title: c.Name, Version: "1.0.0"}
	seen := map[string]bool{}
	var walk func(nodes []*Node)
	walk = func(nodes []*Node) {
		for _, n := range nodes {
			switch n.Type {
			case "folder":
				walk(n.Children)
			case "request":
				r := res.reqs[n.UID]
				if r == nil || r.IsGRPC() {
					continue
				}
				p := openAPIPath(r.URL)
				key := strings.ToUpper(strings.TrimSpace(r.Method)) + " " + p
				if seen[key] {
					continue // 同方法同路径只保留第一条（OpenAPI 的 path+method 唯一）
				}
				seen[key] = true
				doc.Ops = append(doc.Ops, openAPIOperation(r, p))
			}
		}
	}
	walk(c.treeFromScan(res))
	return json.MarshalIndent(doc.Encode(), "", "  ")
}

// openAPIPath 从请求 URL 取 path：`{{host}}/api/login?x=1` → `/api/login`；
// 绝对地址 `https://a.com/x` → `/x`；没有路径段时回落 `/`。
func openAPIPath(u string) string {
	s := strings.TrimSpace(u)
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	// 去掉主机/变量段：第一段到第一个 `/`
	if !strings.HasPrefix(s, "/") {
		if i := strings.Index(s, "/"); i >= 0 {
			s = s[i:]
		} else {
			s = "/"
		}
	}
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	if strings.TrimSpace(s) == "" {
		return "/"
	}
	return s
}

// openAPIOperation 把一个请求转成 OpenAPI operation。
func openAPIOperation(r *Request, p string) shopenapi.Operation {
	op := shopenapi.Operation{
		Path:        p,
		Method:      strings.ToLower(strings.TrimSpace(r.Method)),
		Summary:     r.Summary,
		Description: r.Docs,
	}
	for _, q := range r.Params {
		if !q.Enabled || strings.TrimSpace(q.Name) == "" {
			continue
		}
		op.Params = append(op.Params, shopenapi.Param{
			Name: q.Name, In: "query", Type: "string", Example: q.Value, Description: q.Description,
		})
	}
	for _, h := range r.Headers {
		if !h.Enabled || strings.TrimSpace(h.Name) == "" {
			continue
		}
		op.Params = append(op.Params, shopenapi.Param{
			Name: h.Name, In: "header", Type: "string", Example: h.Value, Description: h.Description,
		})
	}
	if r.RespDoc != nil {
		code := "200"
		if r.RespDoc.Status > 0 {
			code = strconv.Itoa(r.RespDoc.Status)
		}
		op.Responses = append(op.Responses, shopenapi.Response{
			Code: code, Description: "OK", ExampleJSON: r.RespDoc.Sample,
		})
	}
	return op
}
