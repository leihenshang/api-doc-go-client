package collection

import (
	"strings"

	share "github.com/leihenshang/api-doc-go-share/collection"
)

// Defaults 当前集合级公共请求参数（未配置时返回 nil）。
// 返回值是副本：调用方修改不会影响集合内存与磁盘内容。
func (c *Collection) Defaults() *share.Defaults {
	if c.defaults == nil {
		return nil
	}
	out := &share.Defaults{
		Headers: append([]KV(nil), c.defaults.Headers...),
	}
	if c.defaults.Auth != nil {
		cp := *c.defaults.Auth
		out.Auth = &cp
	}
	return out
}

// SetDefaults 写集合级公共请求参数（nil 或全空 = 清除并删掉清单里的 defaults 段）。
func (c *Collection) SetDefaults(d *share.Defaults) error {
	c.defaults = normalizeDefaults(d)
	return c.writeManifest()
}

// normalizeDefaults 去掉完全空白的请求头行（名与值都为空），保留禁用行（enabled=false 表示不发送）；
// 认证类型为空、none 或 inherit（集合默认自身无处可继承）视为未配置。返回 nil 表示整段可删除。
func normalizeDefaults(d *share.Defaults) *share.Defaults {
	if d == nil {
		return nil
	}
	out := &share.Defaults{}
	for _, kv := range d.Headers {
		if strings.TrimSpace(kv.Name) == "" && strings.TrimSpace(kv.Value) == "" {
			continue
		}
		out.Headers = append(out.Headers, kv)
	}
	if d.Auth != nil {
		switch strings.TrimSpace(d.Auth.Type) {
		case "", "none", "inherit":
		default:
			cp := *d.Auth
			out.Auth = &cp
		}
	}
	if len(out.Headers) == 0 && out.Auth == nil {
		return nil
	}
	return out
}

// MergeDefaults 返回应用集合级公共参数后的请求副本（不修改入参）。
//   - 请求头：集合级在前，请求同名（大小写不敏感）覆盖；集合级 enabled=false 的行不加入。
//   - 认证：请求未指定（nil、空 type 或 inherit）且集合配置了认证时取集合默认；
//     none/basic/bearer/apikey 均以请求为准。
func MergeDefaults(r *Request, d *share.Defaults) *Request {
	if r == nil || d == nil {
		return r
	}
	out := *r
	out.Headers = mergeHeaders(d.Headers, r.Headers)
	if d.Auth != nil && (r.Auth == nil || strings.TrimSpace(r.Auth.Type) == "" || r.Auth.Type == "inherit") {
		cp := *d.Auth
		out.Auth = &cp
	}
	return &out
}

// mergeHeaders 合并集合级与请求头：同名以请求为准（含禁用态，便于单条请求关掉公共头）。
func mergeHeaders(base, req []KV) []KV {
	if len(base) == 0 {
		return req
	}
	out := make([]KV, 0, len(base)+len(req))
	index := make(map[string]int, len(base)+len(req))
	for _, kv := range base {
		name := strings.TrimSpace(kv.Name)
		if !kv.Enabled || name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := index[key]; ok {
			continue
		}
		index[key] = len(out)
		out = append(out, kv)
	}
	for _, kv := range req {
		key := strings.ToLower(strings.TrimSpace(kv.Name))
		if i, ok := index[key]; ok {
			out[i] = kv
			continue
		}
		if key == "" {
			out = append(out, kv)
			continue
		}
		index[key] = len(out)
		out = append(out, kv)
	}
	return out
}
