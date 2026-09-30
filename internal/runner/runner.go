// Package runner 请求执行器：把（编辑中的）请求草稿按环境变量渲染后真实发送。
package runner

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/varx"
)

const (
	defaultTimeout = 30 * time.Second
	defaultMaxRD   = 5
	maxBodySize    = 10 << 20 // 10MB
)

// Options 发送策略：来自全局设置，可被请求级 settings 覆盖。
type Options struct {
	InsecureSSL     bool
	Timeout         time.Duration
	FollowRedirects bool
	MaxRedirects    int
	Jar             http.CookieJar
	ProxyURL        string // HTTP(S) 代理；空 = 直连
}

// DefaultOptions 全局设置未就绪时的兜底策略。
func DefaultOptions() Options {
	return Options{Timeout: defaultTimeout, FollowRedirects: true, MaxRedirects: defaultMaxRD}
}

// NewOptions 由设置值合成策略（集中默认值，避免各调用点重复搬运字段）；
// timeoutSec / maxRedirects <= 0 时取默认值。
func NewOptions(insecureSSL bool, timeoutSec int, followRedirects bool, maxRedirects int, jar http.CookieJar, proxyURL string) Options {
	o := DefaultOptions()
	o.InsecureSSL = insecureSSL
	o.FollowRedirects = followRedirects
	o.Jar = jar
	o.ProxyURL = proxyURL
	if timeoutSec > 0 {
		o.Timeout = time.Duration(timeoutSec) * time.Second
	}
	if maxRedirects > 0 {
		o.MaxRedirects = maxRedirects
	}
	return o
}

// Result 一次真实请求的结果；Binary 为真时 Body 是 base64 编码。
// Script 为脚本/断言阶段产物（无脚本时为 nil）。
type Result struct {
	URL         string          `json:"url"`
	Status      int             `json:"status"`
	Proto       string          `json:"proto"`
	TimeMS      int64           `json:"timeMs"`
	Size        int             `json:"size"`
	ContentType string          `json:"contentType"`
	Binary      bool            `json:"binary"`
	Headers     []collection.KV `json:"headers"`
	Body        string          `json:"body"`
	Script      any             `json:"script,omitempty"`
}

// Send 渲染并发送请求。vars 为已选环境的变量（已合并 secret 与内置变量）。
// ctx 用于取消（UI 取消发送）与超时；nil 时按 Background 处理，仅受 Options.Timeout 约束。
func Send(ctx context.Context, r collection.Request, vars map[string]string, opts Options) (*Result, error) {
	opts = mergeOptions(opts, r.Settings)
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	target, err := buildURL(r, vars)
	if err != nil {
		return nil, err
	}
	req, err := buildRequest(ctx, r, vars, target)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	resp, err := newClient(opts).Do(req)
	if err != nil {
		return nil, sendError(ctx, err)
	}
	defer resp.Body.Close()
	return readResult(resp, target.String(), time.Since(start))
}

// sendError 把 context 取消/超时译成用户可读文案，其余原样包装。
func sendError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(ctx.Err(), context.Canceled):
		return errors.New("请求已取消")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("请求超时: %w", err)
	default:
		return fmt.Errorf("发送请求: %w", err)
	}
}

// mergeOptions 补齐零值并用请求级 settings 覆盖。
func mergeOptions(o Options, s *collection.RequestSettings) Options {
	if o.Timeout <= 0 {
		o.Timeout = defaultTimeout
	}
	if o.MaxRedirects <= 0 {
		o.MaxRedirects = defaultMaxRD
	}
	if s == nil {
		return o
	}
	if s.TimeoutSec > 0 {
		o.Timeout = time.Duration(s.TimeoutSec) * time.Second
	}
	if s.MaxRedirects > 0 {
		o.MaxRedirects = s.MaxRedirects
	}
	if s.FollowRedirects != nil {
		o.FollowRedirects = *s.FollowRedirects
	}
	if s.InsecureSSL != nil {
		o.InsecureSSL = *s.InsecureSSL
	}
	return o
}

func buildURL(r collection.Request, vars map[string]string) (*url.URL, error) {
	raw, _ := varx.Resolve(strings.TrimSpace(r.URL), vars)
	if raw == "" {
		return nil, fmt.Errorf("URL 为空")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("URL 无法解析: %w", err)
	}
	q := u.Query()
	for _, p := range resolveKV(r.Params, vars) {
		q.Set(p.Name, p.Value)
	}
	u.RawQuery = q.Encode()
	return u, nil
}

func buildRequest(ctx context.Context, r collection.Request, vars map[string]string, u *url.URL) (*http.Request, error) {
	method := strings.ToUpper(strings.TrimSpace(r.Method))
	if method == "" {
		method = http.MethodGet
	}
	body, contentType, err := buildBody(r.Body, vars)
	if err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return nil, fmt.Errorf("构造请求: %w", err)
	}
	for _, h := range resolveKV(r.Headers, vars) {
		req.Header.Set(h.Name, h.Value)
	}
	if contentType != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", contentType)
	}
	applyAuth(req, r.Auth, vars)
	return req, nil
}

// buildBody 返回请求体与默认 Content-Type（nil 表示无请求体）。
func buildBody(b collection.Body, vars map[string]string) ([]byte, string, error) {
	switch strings.ToLower(b.Type) {
	case "json":
		raw, _ := varx.Resolve(b.Raw, vars)
		return []byte(raw), "application/json", nil
	case "text":
		raw, _ := varx.Resolve(b.Raw, vars)
		return []byte(raw), "text/plain; charset=utf-8", nil
	case "form":
		form := url.Values{}
		for _, kv := range resolveKV(b.Form, vars) {
			form.Set(kv.Name, kv.Value)
		}
		return []byte(form.Encode()), "application/x-www-form-urlencoded", nil
	case "multipart":
		return multipartBody(b.Form, vars)
	}
	return nil, "", nil
}

func multipartBody(items []collection.KV, vars map[string]string) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, kv := range resolveKV(items, vars) {
		if strings.EqualFold(kv.Type, "file") {
			// 文件 part：value 是路径（支持 {{变量}}），读盘后按 CreateFormFile 写入
			path := strings.TrimSpace(kv.Value)
			if path == "" {
				return nil, "", fmt.Errorf("multipart 文件字段 %q 缺少路径", kv.Name)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, "", fmt.Errorf("读取上传文件 %q: %w", path, err)
			}
			part, err := w.CreateFormFile(kv.Name, filepath.Base(path))
			if err != nil {
				return nil, "", fmt.Errorf("创建文件字段 %q: %w", kv.Name, err)
			}
			if _, err := part.Write(data); err != nil {
				return nil, "", fmt.Errorf("写入文件字段 %q: %w", kv.Name, err)
			}
			continue
		}
		if err := w.WriteField(kv.Name, kv.Value); err != nil {
			return nil, "", fmt.Errorf("写入 multipart 字段: %w", err)
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", fmt.Errorf("结束 multipart: %w", err)
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

// resolveKV 渲染变量并过滤掉未启用/无名的行。
func resolveKV(items []collection.KV, vars map[string]string) []collection.KV {
	out := make([]collection.KV, 0, len(items))
	for _, kv := range items {
		if !kv.Enabled || kv.Name == "" {
			continue
		}
		name, _ := varx.Resolve(kv.Name, vars)
		val, _ := varx.Resolve(kv.Value, vars)
		out = append(out, collection.KV{Name: name, Value: val, Enabled: true, Type: kv.Type})
	}
	return out
}

// applyAuth 按认证类型注入凭据；未知类型（如 Bruno 的 inherit）视为不启用。
func applyAuth(req *http.Request, a *collection.Auth, vars map[string]string) {
	if a == nil {
		return
	}
	switch strings.ToLower(a.Type) {
	case "basic":
		user, _ := varx.Resolve(a.Username, vars)
		pass, _ := varx.Resolve(a.Password, vars)
		req.SetBasicAuth(user, pass)
	case "bearer":
		if token, _ := varx.Resolve(a.Token, vars); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	case "apikey":
		key, _ := varx.Resolve(a.Key, vars)
		val, _ := varx.Resolve(a.Value, vars)
		if key == "" {
			return
		}
		if strings.EqualFold(a.In, "query") {
			q := req.URL.Query()
			q.Set(key, val)
			req.URL.RawQuery = q.Encode()
			return
		}
		req.Header.Set(key, val)
	}
}

func newClient(o Options) *http.Client {
	client := &http.Client{Timeout: o.Timeout, Jar: o.Jar}
	tr := &http.Transport{}
	if o.InsecureSSL {
		// 用户显式开启：跳过证书校验（自签/内网证书场景）
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402
	}
	if strings.TrimSpace(o.ProxyURL) != "" {
		if u, err := url.Parse(strings.TrimSpace(o.ProxyURL)); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	client.Transport = tr
	client.CheckRedirect = func(_ *http.Request, via []*http.Request) error {
		if !o.FollowRedirects {
			return http.ErrUseLastResponse
		}
		if len(via) >= o.MaxRedirects {
			return fmt.Errorf("重定向超过 %d 次", o.MaxRedirects)
		}
		return nil
	}
	return client
}

func readResult(resp *http.Response, target string, elapsed time.Duration) (*Result, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return nil, fmt.Errorf("读取响应体: %w", err)
	}
	ct := resp.Header.Get("Content-Type")
	binary := isBinary(ct, body)
	text := string(body)
	if binary {
		text = base64.StdEncoding.EncodeToString(body)
	}
	return &Result{
		URL: target, Status: resp.StatusCode, Proto: resp.Proto,
		TimeMS: elapsed.Milliseconds(), Size: len(body),
		ContentType: ct, Binary: binary, Headers: headerList(resp.Header), Body: text,
	}, nil
}

var binaryPrefixes = []string{
	"image/", "audio/", "video/", "font/", "application/pdf",
	"application/zip", "application/gzip", "application/octet-stream",
	"application/vnd.openxmlformats", "application/x-tar", "application/x-7z",
}

var textPrefixes = []string{
	"text/", "application/json", "application/xml", "application/javascript",
	"application/x-www-form-urlencoded", "application/graphql", "application/x-ndjson",
}

// isBinary 先看 Content-Type，未表态时再按 UTF-8 合法性判断。
func isBinary(contentType string, body []byte) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	for _, p := range binaryPrefixes {
		if strings.HasPrefix(ct, p) {
			return true
		}
	}
	for _, p := range textPrefixes {
		if strings.HasPrefix(ct, p) {
			return false
		}
	}
	return !utf8.Valid(body)
}

func headerList(h http.Header) []collection.KV {
	out := make([]collection.KV, 0, len(h))
	for k, vs := range h {
		sort.Strings(vs)
		out = append(out, collection.KV{Name: k, Value: strings.Join(vs, ", "), Enabled: true})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
