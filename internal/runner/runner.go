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
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/varx"
)

const (
	defaultTimeout = 30 * time.Second
	defaultMaxRD   = 5
	maxBodySize    = 10 << 20 // 10MB
	// maxUploadSize multipart 文件字段的单文件上限：避免把超大文件整份读进内存。
	maxUploadSize = 100 << 20 // 100MB
	// maxTimeoutSec 超时上限：秒数换算成 time.Duration 前必须夹住，否则 int 溢出成负值。
	maxTimeoutSec = 600
	// maxRedirectsCap 请求级 maxRedirects 上限（防止病态配置把 UI 卡在跳转链上）。
	maxRedirectsCap = 50
)

// clampTimeoutSec 秒 → Duration，带上下限（<=0 取默认，超上限夹到 maxTimeoutSec）。
func clampTimeoutSec(sec int) time.Duration {
	switch {
	case sec <= 0:
		return defaultTimeout
	case sec > maxTimeoutSec:
		return maxTimeoutSec * time.Second
	default:
		return time.Duration(sec) * time.Second
	}
}

// clampRedirects 重定向次数上下限（<=0 取默认）。
func clampRedirects(n int) int {
	switch {
	case n <= 0:
		return defaultMaxRD
	case n > maxRedirectsCap:
		return maxRedirectsCap
	default:
		return n
	}
}

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
	o.Timeout = clampTimeoutSec(timeoutSec)
	o.MaxRedirects = clampRedirects(maxRedirects)
	return o
}

// Result 一次真实请求的结果；Binary 为真时 Body 是 base64 编码。
// Script 为脚本/断言阶段产物（无脚本时为 nil）。
type Result struct {
	URL    string `json:"url"`
	Status int    `json:"status"`
	Proto  string `json:"proto"`
	TimeMS int64  `json:"timeMs"`
	// Size 返回给界面的响应体字节数（被截断时是截断后的长度）
	Size int `json:"size"`
	// SentSize 发送的请求体字节数（gRPC 为序列化后的消息大小；HTTP 暂未采集，为 0 时不展示）
	SentSize    int             `json:"sentSize,omitempty"`
	ContentType string          `json:"contentType"`
	Binary      bool            `json:"binary"`
	Headers     []collection.KV `json:"headers"`
	// Truncated 响应体超过 maxBodySize 被截断（界面必须提示，否则会以为拿到了全部内容）
	Truncated bool `json:"truncated,omitempty"`
	// FullSize 响应体真实大小（仅在可判定时给出：截断场景取 Content-Length，未知为 0）
	FullSize int `json:"fullSize,omitempty"`
	// Warnings 发送过程中的非致命告警（如跨域重定向丢弃了凭据头）
	Warnings []string `json:"warnings,omitempty"`
	// Trailers gRPC 尾元数据（HTTP 请求为 nil）
	Trailers []collection.KV `json:"trailers,omitempty"`
	Body     string          `json:"body"`
	Script   any             `json:"script,omitempty"`
}

// Send 渲染并发送请求。vars 为已选环境的变量（已合并 secret 与内置变量）。
// ctx 用于取消（UI 取消发送）与超时；nil 时按 Background 处理，仅受 Options.Timeout 约束。
func Send(ctx context.Context, r collection.Request, vars map[string]string, opts Options) (*Result, error) {
	opts = MergeOptions(opts, r.Settings)
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
	warn := &warningList{}
	client := newClient(opts, warn)
	defer client.CloseIdleConnections() // 每次发送都是独立 Transport：用完即回收空闲连接
	resp, err := client.Do(req)
	if err != nil {
		return nil, sendError(ctx, err)
	}
	defer resp.Body.Close()
	return readResult(resp, target.String(), time.Since(start), warn)
}

// warningList 并发安全的告警收集器：重定向钩子在 transport 的 goroutine 里执行，
// 与读取结果的调用方不是同一个 goroutine。
type warningList struct {
	mu   sync.Mutex
	msgs []string
}

// maxWarnings 单次请求最多回传的告警条数（避免病态重定向链刷屏）。
const maxWarnings = 5

func (w *warningList) add(msg string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.msgs) >= maxWarnings {
		return
	}
	w.msgs = append(w.msgs, msg)
}

func (w *warningList) list() []string {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.msgs...)
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

// MergeOptions 补齐零值并用请求级 settings 覆盖（HTTP 与 gRPC 共用同一套规则）。
// 请求级数值同样夹上下限：它们来自集合文件，属于不可信输入。
func MergeOptions(o Options, s *collection.RequestSettings) Options {
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
		o.Timeout = clampTimeoutSec(s.TimeoutSec)
	}
	if s.MaxRedirects > 0 {
		o.MaxRedirects = clampRedirects(s.MaxRedirects)
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
		if !validHeaderName(h.Name) {
			return nil, fmt.Errorf("请求头名称非法（含控制字符或以非法字符开头）: %q", h.Name)
		}
		if !validHeaderValue(h.Value) {
			return nil, fmt.Errorf("请求头 %s 的值含换行或空字符，已拒绝（防头注入）", h.Name)
		}
		req.Header.Set(h.Name, h.Value)
	}
	switch {
	case contentType == "":
	case strings.HasPrefix(contentType, "multipart/form-data"):
		// multipart 的 Content-Type 带 boundary，只能在发送时生成：请求里若手写了
		// `Content-Type: application/json` 之类的显式值，必须用派生值覆盖，
		// 否则服务端拿不到 boundary（表单整个解析失败）。
		req.Header.Set("Content-Type", contentType)
	case req.Header.Get("Content-Type") == "":
		req.Header.Set("Content-Type", contentType)
	}
	if err := applyAuth(req, r.Auth, vars); err != nil {
		return nil, err
	}
	return req, nil
}

// headerNameRe RFC 7230 token：请求头字段名的合法字符集。
var headerNameRe = regexp.MustCompile("^[!#$%&'*+\\-.^_`|~0-9A-Za-z]+$")

func validHeaderName(name string) bool { return headerNameRe.MatchString(name) }

// validHeaderValue 头部值不得含 CR/LF/NUL（否则可注入额外头或拆分请求）。
func validHeaderValue(v string) bool { return !strings.ContainsAny(v, "\r\n\x00") }

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
			data, err := readUploadFile(path)
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

// readUploadFile 读取 multipart 上传文件：只接受普通文件且不超过 maxUploadSize。
//
// 路径来自请求文件（集合是纯文本、可能来自不可信仓库或同步服务端），因此这里必须
// 挡住「目录/设备文件」与「超大文件整份读进内存」两种情况；路径本身仍由用户显式填写
// （选择上传文件走系统对话框），不额外限制目录 —— 否则会破坏既有使用方式。
func readUploadFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("是目录，不能作为上传文件")
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("不是普通文件（可能是设备/管道）")
	}
	if info.Size() > maxUploadSize {
		return nil, fmt.Errorf("文件 %s 超过 %d MB 上限", filepath.Base(path), maxUploadSize>>20)
	}
	return os.ReadFile(path)
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
// API Key 的键名来自集合文件，同样按「头名/头值」规则校验，避免异常键名直接写进请求。
func applyAuth(req *http.Request, a *collection.Auth, vars map[string]string) error {
	if a == nil {
		return nil
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
			return nil
		}
		if strings.EqualFold(a.In, "query") {
			q := req.URL.Query()
			q.Set(key, val)
			req.URL.RawQuery = q.Encode()
			return nil
		}
		if !validHeaderName(key) {
			return fmt.Errorf("API Key 头名称非法: %q", key)
		}
		if !validHeaderValue(val) {
			return fmt.Errorf("API Key 头 %s 的值含换行或空字符，已拒绝", key)
		}
		req.Header.Set(key, val)
	}
	return nil
}

func newClient(o Options, warn *warningList) *http.Client {
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
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if !o.FollowRedirects {
			return http.ErrUseLastResponse
		}
		// via 是「已经发出过」的请求列表：len(via) 即已发生的重定向次数，
		// 因此 > MaxRedirects 才表示「这一次会超出上限」（写 >= 会少跟一次）。
		if len(via) > o.MaxRedirects {
			return fmt.Errorf("重定向超过 %d 次", o.MaxRedirects)
		}
		stripCrossHostCredentials(req, via, warn)
		return nil
	}
	return client
}

// stripCrossHostCredentials 跨主机（或 https → http 降级）重定向时丢弃凭据类请求头。
//
// 标准库只会自动剥离 Authorization/Cookie，自定义的 X-Api-Key / X-Auth-Token 等
// 会被原样带到重定向目标，等于把密钥交给第三方域名。这里按名字启发式匹配后删除，
// 并把这件事作为告警回传界面（用户能看到「凭据没跟过去」）。
func stripCrossHostCredentials(req *http.Request, via []*http.Request, warn *warningList) {
	if len(via) == 0 {
		return
	}
	prev := via[len(via)-1]
	if prev == nil || prev.URL == nil || req.URL == nil {
		return
	}
	downgrade := prev.URL.Scheme == "https" && req.URL.Scheme != "https"
	if prev.URL.Host == req.URL.Host && !downgrade {
		return
	}
	var dropped []string
	for name := range req.Header {
		if !isSensitiveHeader(name) {
			continue
		}
		req.Header.Del(name)
		dropped = append(dropped, name)
	}
	if len(dropped) == 0 {
		return
	}
	sort.Strings(dropped)
	warn.add(fmt.Sprintf("重定向到 %s 时已丢弃凭据类请求头（%s）", req.URL.Host, strings.Join(dropped, ", ")))
}

// sensitiveHeaderKeywords 疑似凭据的头部名关键字（小写包含匹配）。
var sensitiveHeaderKeywords = []string{
	"token", "secret", "api-key", "api_key", "apikey", "access-key", "accesskey",
	"password", "passwd", "credential", "session", "auth",
}

func isSensitiveHeader(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "authorization", "cookie", "cookie2", "proxy-authorization", "www-authenticate":
		return true
	}
	for _, kw := range sensitiveHeaderKeywords {
		if strings.Contains(n, kw) {
			return true
		}
	}
	return false
}

func readResult(resp *http.Response, target string, elapsed time.Duration, warn *warningList) (*Result, error) {
	// 多读 1 字节用于判定「是否被截断」：不能把超限的响应静默当成完整内容。
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize+1))
	if err != nil {
		return nil, fmt.Errorf("读取响应体: %w", err)
	}
	truncated := len(body) > maxBodySize
	fullSize := 0
	if truncated {
		body = body[:maxBodySize]
		fullSize = maxBodySize
		if n := resp.ContentLength; n > 0 {
			fullSize = int(n)
		}
		warn.add(fmt.Sprintf("响应体超过 %d MB，已截断（仅展示前 %d MB）", maxBodySize>>20, maxBodySize>>20))
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
		Truncated: truncated, FullSize: fullSize, Warnings: warn.list(),
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
