package mcp

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 授权弹窗的入参脱敏（T2 §7.1）：
//   - 只取白名单键，不把完整 args 原文送到前端/屏幕；
//   - headers/params/auth/metadata 只给键名列表（值可能含令牌）；
//   - body 只给类型；curl 整段丢弃（可能内嵌 Authorization 头）；
//   - 键名疑似敏感（token/password/secret/authorization/apikey/key）时值掩码；
//     set_env_var 在 secret=true 时 value 一律掩码；
//   - 长度上限 summaryMaxBytes，超长截断并标注。

const (
	maskValue       = "••••••"
	summaryMaxBytes = 2048
)

// sensitiveKey 键名命中即视为敏感（大小写不敏感，子串匹配）。
var sensitiveKey = regexp.MustCompile(`(?i)(secret|token|password|authorization|apikey|api[_-]?key|key)`)

// scalarArgs 允许出现在摘要里的标量/简单值字段（白名单之外一律丢弃）。
var scalarArgs = map[string]bool{
	"project": true, "uid": true, "workspace": true, "name": true, "newName": true,
	"env": true, "parent": true, "folder": true, "method": true, "url": true,
	"query": true, "dirName": true, "root": true, "limit": true,
	"refresh": true, "includeRequests": true, "includeBody": true, "maxBodyBytes": true,
	"saveExample": true, "enabled": true, "secret": true, "in": true,
	"exampleName": true, "ifMatch": true, "copyFromUid": true,
}

// keyList 把 [{name:..},{name:..}] 形态的 KV 数组压缩成键名列表（顺序、去空、去重排序）。
func keyList(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// summarizeArgs 生成脱敏后的入参摘要 JSON 字符串（失败时返回保守空对象）。
func summarizeArgs(tool string, req *mcp.CallToolRequest) string {
	if req == nil || req.Params == nil || len(req.Params.Arguments) == 0 {
		return "{}"
	}
	var args map[string]any
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return "{}"
	}
	out := map[string]any{}
	secretVar := false
	if b, ok := args["secret"].(bool); ok && b {
		secretVar = true
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := args[k]
		// set_env_var 的变量值：secret 变量掩码；普通变量也不展示全文（弹窗只需知道「要写一个变量」）。
		// 放在白名单过滤之前：value 不在 scalarArgs 里（它不是普通标量，有专门语义）。
		if k == "value" {
			if secretVar {
				out[k] = maskValue
			} else {
				out[k] = "<value>"
			}
			continue
		}
		switch k {
		case "curl":
			out[k] = "<已省略：cURL 可能内嵌凭据>"
			continue
		case "headers", "params", "metadata":
			if names := keyList(v); names != nil {
				out[k] = map[string]any{"keys": names}
			}
			continue
		case "auth":
			if m, ok := v.(map[string]any); ok {
				auth := map[string]any{}
				if t, ok := m["type"].(string); ok {
					auth["type"] = t
				}
				if names := keyList(m["headers"]); names != nil {
					auth["keys"] = names // 个别来源会把 headers 挂进 auth
				}
				out[k] = auth
			}
			continue
		case "content":
			// 文档正文：可能很长、也可能含密钥（示例里的 token）。弹窗只需要知道「要写多少字」，
			// 原文一律不外送（与 curl/headers 的处理口径一致）。
			if sv, ok := v.(string); ok {
				out[k] = fmt.Sprintf("<%d 字符>", len([]rune(sv)))
			}
			continue
		case "body":
			if m, ok := v.(map[string]any); ok {
				b := map[string]any{}
				if t, ok := m["type"].(string); ok {
					b["type"] = t
				}
				out[k] = b
			}
			continue
		}
		if !scalarArgs[k] {
			continue
		}
		if _, ok := v.(string); ok && sensitiveKey.MatchString(k) {
			out[k] = maskValue
			continue
		}
		out[k] = v
	}
	// 关闭 HTML 转义：摘要要直接显示在弹窗里（<value>、URL 里的 <> 不应变成 u003c）。
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return "{}"
	}
	b := strings.TrimRight(sb.String(), "\n")
	if len(b) > summaryMaxBytes {
		b = b[:summaryMaxBytes] + "…<截断>"
	}
	return b
}
