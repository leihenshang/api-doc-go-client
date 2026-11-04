package app

// U5：只读打开分享链接。
//
// 设计取舍（已与产品确认）：
//   - 只读浏览**不落盘**、不建集合、不参与同步 —— 分享是服务端项目的快照，
//     落到本地会让「本地集合」和「分享来源」两套数据打架；
//   - 支持「一键复制 cURL」与「直接发送」：分享的用途就是给人试接口，
//     只读不能发的话价值少一半；发送不写本地历史（不属于任何集合）。
//
// 服务端接口：GET <base>/share/<token>（免登录），返回
// { code, msg, data: { project, groups, apis, docs, remark, expire_time } }。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/runner"
)

// ShareBundle 分享只读数据（字段名对齐服务端 ShareView 的 JSON）。
type ShareBundle struct {
	Project  ShareProject `json:"project"`
	Groups   []ShareGroup `json:"groups"`
	Apis     []ShareApi   `json:"apis"`
	Docs     []ShareDoc   `json:"docs"`
	Remark   string       `json:"remark"`
	ExpireAt string       `json:"expire_time,omitempty"`
	// BaseURL 分享所在的服务器根地址（回显给前端，便于展示来源）。
	BaseURL string `json:"-"`
}

// ShareProject 分享页展示的项目信息。
type ShareProject struct {
	ID          uint64 `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

// ShareGroup 分享里的分组（只用到树展示需要的字段）。
type ShareGroup struct {
	ID       uint64 `json:"id"`
	PID      uint64 `json:"p_id"`
	Title    string `json:"title"`
	Type     int8   `json:"type"`
	Priority int    `json:"priority"`
}

// ShareApi 分享里的一条接口（服务端 model.Api 的 JSON 字段子集）。
type ShareApi struct {
	ID                    uint64 `json:"id"`
	GroupID               uint64 `json:"group_id"`
	Priority              int    `json:"priority"`
	ApiName               string `json:"api_name"`
	HttpMethodType        string `json:"http_method_type"`
	URL                   string `json:"url"`
	Description           string `json:"description"`
	HttpRequestSummary    string `json:"http_request_summary"`
	HttpRequestParams     string `json:"http_request_params"`
	HttpRequestHeader     string `json:"http_request_header"`
	HttpRequestBodyType   string `json:"http_request_body_type"`
	HttpRequestBody       string `json:"http_request_body"`
	HttpRequestBodyForm   string `json:"http_request_body_form"`
	HttpRequestBodyDescs  string `json:"http_request_body_descs"`
	HttpRequestBodyFields string `json:"http_request_body_fields"`
	HttpAuth              string `json:"http_auth"`
	HttpScript            string `json:"http_script"`
	ScriptVars            string `json:"script_vars"`
	ScriptAsserts         string `json:"script_asserts"`
	HttpReturnType        string `json:"http_return_type"`
	HttpReturnParams      string `json:"http_return_params"`
	HttpReturnSample      string `json:"http_return_sample"`
	HttpReturnContentType string `json:"http_return_content_type"`
	Status                int8   `json:"status"`
	Version               string `json:"version"`
}

// ShareDoc 分享里的一篇文档。
type ShareDoc struct {
	ID       uint64 `json:"id"`
	GroupID  uint64 `json:"group_id"`
	Priority int    `json:"priority"`
	Title    string `json:"title"`
	Content  string `json:"content"`
	Icon     string `json:"icon"`
}

// FetchShare 解析分享链接并拉取只读数据。
//
// 接受的输入：完整的 `http(s)://host[:port]/share/<token>`（Web 分享页地址），
// 或 `host[:port]/share/<token>`（省略协议，按 http 处理）。纯 token 无法确定服务器，直接报错。
func (a *App) FetchShare(shareURL string) (*ShareBundle, error) {
	base, token, err := parseShareURL(shareURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, base+"/share/"+url.PathEscape(token), nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接分享服务器失败：%w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	var env struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("分享服务器返回的不是 JSON（HTTP %d）", resp.StatusCode)
	}
	if env.Code != 200 || len(env.Data) == 0 {
		if env.Msg == "" {
			env.Msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("%s", env.Msg)
	}
	var b ShareBundle
	if err := json.Unmarshal(env.Data, &b); err != nil {
		return nil, fmt.Errorf("分享数据解析失败：%w", err)
	}
	b.BaseURL = base
	return &b, nil
}

// SendSharedRequest 用分享里的接口发一次请求。
//
// 与本地 SendRequest 的差别：**不写本地历史**（这条请求不属于任何集合）、
// 不接受取消（没有页签承载「发送中」状态），其余链路（变量解析、脚本、断言）保持一致。
func (a *App) SendSharedRequest(api ShareApi, envName string) (*runner.Result, error) {
	r := shareAPIToRequest(api)
	if r.URL == "" {
		return nil, fmt.Errorf("接口地址为空，无法发送")
	}
	// 发送链路与本地一致：cookie jar 必须先就绪（runner 里用它装载/回写 Cookie）。
	a.ensureJar()
	// 分享场景通常没有打开集合：拿不到环境变量就退化为「只有请求自带变量」。
	vars := map[string]string{}
	if v, err := a.envVars(envName); err == nil {
		vars = v
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return a.executeRequest(ctx, r, vars)
}

// parseShareURL 从分享链接里拆出服务器根地址与令牌。
func parseShareURL(raw string) (string, string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", "", fmt.Errorf("请输入分享链接")
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", "", fmt.Errorf("分享链接格式不正确：%s", raw)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	idx := -1
	for i, p := range parts {
		if p == "share" {
			idx = i
		}
	}
	if idx < 0 || idx+1 >= len(parts) || strings.TrimSpace(parts[idx+1]) == "" {
		return "", "", fmt.Errorf("链接里没有分享令牌（应形如 http://host/share/<token>）")
	}
	base := u.Scheme + "://" + u.Host
	return base, parts[idx+1], nil
}

// shareAPIToRequest 把分享接口转成可发送的请求（服务端各列是 JSON 文本）。
func shareAPIToRequest(a ShareApi) *collection.Request {
	r := &collection.Request{
		UID:     fmt.Sprintf("share-%d", a.ID),
		Name:    a.ApiName,
		Method:  strings.ToUpper(strings.TrimSpace(a.HttpMethodType)),
		URL:     a.URL,
		Summary: a.HttpRequestSummary,
		Docs:    a.Description,
	}
	r.Params = decodeJSONList[collection.KV](a.HttpRequestParams)
	r.Headers = decodeJSONList[collection.KV](a.HttpRequestHeader)
	r.Body = collection.Body{
		Type:   a.HttpRequestBodyType,
		Raw:    a.HttpRequestBody,
		Form:   decodeJSONList[collection.KV](a.HttpRequestBodyForm),
		Fields: decodeJSONList[collection.BodyField](a.HttpRequestBodyFields),
	}
	if a.HttpRequestBodyDescs != "" {
		_ = json.Unmarshal([]byte(a.HttpRequestBodyDescs), &r.Body.Descs)
	}
	if a.HttpAuth != "" {
		var au collection.Auth
		if json.Unmarshal([]byte(a.HttpAuth), &au) == nil && au.Type != "" {
			r.Auth = &au
		}
	}
	if a.HttpScript != "" {
		var sc collection.ScriptBlock
		if json.Unmarshal([]byte(a.HttpScript), &sc) == nil {
			r.Script = &sc
		}
	}
	if a.ScriptVars != "" {
		r.VarsPreRequest = decodeJSONList[collection.ScriptVar](a.ScriptVars)
	}
	if a.ScriptAsserts != "" {
		r.Asserts = decodeJSONList[collection.ScriptAssert](a.ScriptAsserts)
	}
	if a.HttpReturnType != "" || a.HttpReturnSample != "" || a.HttpReturnParams != "" {
		rd := &collection.ResponseDoc{
			Type:        a.HttpReturnType,
			Sample:      a.HttpReturnSample,
			ContentType: a.HttpReturnContentType,
		}
		rd.Params = decodeJSONList[collection.ResponseParam](a.HttpReturnParams)
		r.RespDoc = rd
	}
	return r
}

// decodeJSONList 宽松解析 JSON 数组：空串/非法 JSON 一律返回 nil（分享列允许为空）。
func decodeJSONList[T any](raw string) []T {
	s := strings.TrimSpace(raw)
	if s == "" || s == "null" {
		return nil
	}
	var out []T
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}
