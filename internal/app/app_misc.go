package app

// 导入 / 响应示例 / 代码生成 / 导出 / 窗口控制（P2-8：从 app.go 按子域拆出）。

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	urlpkg "net/url"
	"os"
	"path/filepath"
	"strings"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/config"
	"api-doc-go-client/internal/cookiejar"
	"api-doc-go-client/internal/runner"
	"api-doc-go-client/internal/script"
	"api-doc-go-client/internal/varx"
	codegen "github.com/leihenshang/api-doc-go-share/codegen"
	runtime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ---- 导入（Postman / OpenAPI，共享内核见 share 的 postman / openapi 包）----

// ImportCollection 把外部数据导入到 parent 分组下
// （format：postman / openapi / apidocgo；Bruno 走 ImportBrunoDir 选目录）。
func (a *App) ImportCollection(parent, format, text string) (*collection.ImportSummary, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "postman":
		return c.ImportPostman(parent, []byte(text))
	case "openapi":
		return c.ImportOpenAPI(parent, []byte(text))
	case "apidocgo":
		// 本客户端导出的集合整包（目录 + 请求 + 文档），无损回灌
		return c.ImportBundle(parent, []byte(text))
	case "bruno":
		return nil, fmt.Errorf("Bruno 请使用 ImportBrunoDir 选择目录导入")
	}
	return nil, fmt.Errorf("不支持的导入格式：%s（仅支持 postman / openapi / apidocgo）", format)
}

// ImportBrunoDir 导入原生 Bruno 集合目录（按本客户端格式解析）。
func (a *App) ImportBrunoDir(srcDir string) (*collection.ImportSummary, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(srcDir) == "" {
		return nil, errors.New("目录为空")
	}
	return c.ImportBruno(srcDir)
}

// ---- 保存的响应示例（Bruno 的 Save Response）----

// ListResponseExamples 某请求已保存的响应示例（新 → 旧）。
func (a *App) ListResponseExamples(reqUID string) ([]*collection.ResponseExample, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	return c.ListResponseExamples(reqUID)
}

// SaveResponseExample 把一次真实响应存为示例：请求快照取当前草稿，落集合的 examples/。
func (a *App) SaveResponseExample(r *collection.Request, name string, res *runner.Result) (*collection.ResponseExample, error) {
	if r == nil || res == nil {
		return nil, errors.New("请求或响应为空")
	}
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	req := collection.ExampleRequest{
		Method: strings.ToUpper(r.Method), URL: r.URL, Headers: r.Headers, Body: r.Body,
	}
	example := collection.ExampleResponse{
		Status: res.Status, Proto: res.Proto, TimeMS: res.TimeMS, Size: res.Size,
		ContentType: res.ContentType, Binary: res.Binary, Headers: res.Headers, Body: res.Body,
	}
	return c.SaveResponseExample(r.UID, name, req, example)
}

// DeleteResponseExample 删除示例（移入 .trash/ 可人工找回）。
func (a *App) DeleteResponseExample(reqUID, exampleUID string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.DeleteResponseExample(reqUID, exampleUID)
}

// 环境变量读写与并发保护见同包 env.go（ListEnvs / SaveEnvChecked / RenameEnv / EnvFileHash / SaveEnv / DeleteEnv / envVars）。

// kvMap 把 KV 行摊成脚本可用的对象（同名后者覆盖前者）。
func kvMap(rows []collection.KV) map[string]string {
	if len(rows) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.Name] = row.Value
	}
	return out
}

// grpcCodeOf gRPC 响应的状态码（HTTP 响应返回 0，脚本里用 res.isGrpc 区分）。
func grpcCodeOf(res *runner.Result) int {
	if res != nil && res.Proto == "gRPC" {
		return res.Status
	}
	return 0
}

// ScriptResult 脚本与断言阶段的产物（随响应返回前端）。
type ScriptResult struct {
	Vars        map[string]string     `json:"vars,omitempty"`
	Asserts     []script.AssertResult `json:"asserts,omitempty"`
	ScriptError string                `json:"scriptError,omitempty"`
}

// SendRequest 按 envName 渲染并发送请求草稿，同时记入本地历史。
// 流程：vars.pre-request + script.pre-request → 发送 → script.post-response + assert。
// 可通过 CancelSend(uid) 取消在途发送（UI 发送中的取消按钮）。
func (a *App) SendRequest(r *collection.Request, envName string) (*runner.Result, error) {
	if r == nil {
		return nil, errors.New("请求为空")
	}
	vars, err := a.envVars(envName)
	if err != nil {
		return nil, err
	}
	a.ensureJar()

	ctx, cancel := context.WithCancel(context.Background())
	// 在途发送按「工作目录 + 请求 uid」登记：多根并存时不同目录里出现同名 uid 也不会互相取消
	it := &inflightSend{cancel: cancel}
	a.registerSend(a.activeRootID(), r.UID, it)
	defer func() {
		a.unregisterSend(it)
		cancel()
	}()

	// 前置：vars 赋值 + pre-request 脚本（脚本可覆盖环境变量）
	scr := script.Scripts{}
	if r.Script != nil {
		scr = script.Scripts{PreRequest: r.Script.PreRequest, PostResponse: r.Script.PostResponse}
	}
	sess := script.New(vars, scr)
	preVars := make([]script.Var, 0, len(r.VarsPreRequest))
	for _, v := range r.VarsPreRequest {
		preVars = append(preVars, script.Var{Name: v.Name, Value: v.Value, Enabled: v.Enabled})
	}
	pre := sess.RunPreRequest(preVars)
	// vars.pre-request 的 value 允许 {{变量}}：用合并后再解析一次
	sendVars := sess.Vars()
	for k, v := range pre.Vars {
		if resolved, _ := varx.Resolve(v, sendVars); resolved != v {
			sendVars[k] = resolved
			sess.SetVar(k, resolved)
		}
	}

	res, sendErr := a.executeRequest(ctx, r, sendVars)

	// 后置：post-response 脚本 + 断言
	var scriptOut *script.RunResult
	if res != nil && sendErr == nil {
		scriptOut = sess.RunPostResponse(&script.Response{
			Status:       res.Status,
			Headers:      kvMap(res.Headers),
			Body:         script.ParseBody(res.Body),
			BodyText:     res.Body,
			ResponseTime: res.TimeMS,
			ContentType:  res.ContentType,
			// gRPC 语义（G10.2）：res.status = gRPC code、res.grpcCode 同值、res.trailers 为尾元数据
			IsGRPC:   res.Proto == "gRPC",
			GRPCCode: grpcCodeOf(res),
			Trailers: kvMap(res.Trailers),
		}, toScriptAsserts(r.Asserts))
	} else if pre.ScriptError != "" || len(pre.Vars) > 0 {
		scriptOut = pre
	}

	a.recordHistory(r, res, sendErr)
	if sendErr != nil {
		return nil, sendErr
	}
	if scriptOut != nil {
		res.Script = &ScriptResult{
			Vars:        scriptOut.Vars,
			Asserts:     scriptOut.Asserts,
			ScriptError: scriptOut.ScriptError,
		}
	} else if pre.ScriptError != "" {
		res.Script = &ScriptResult{ScriptError: pre.ScriptError}
	}
	return res, nil
}

func toScriptAsserts(list []collection.ScriptAssert) []script.Assert {
	if len(list) == 0 {
		return nil
	}
	out := make([]script.Assert, 0, len(list))
	for _, a := range list {
		out = append(out, script.Assert{Name: a.Name, Expr: a.Expr})
	}
	return out
}

// executeRequest 按协议分派：HTTP 走 runner.Send，gRPC 走 runner.SendGRPC（见 grpc.go）。
func (a *App) executeRequest(ctx context.Context, r *collection.Request, vars map[string]string) (*runner.Result, error) {
	if r.IsGRPC() {
		return a.sendGRPC(ctx, r, vars)
	}
	return runner.Send(ctx, *r, vars, a.sendOptions())
}

// registerSend 登记在途发送：同一「工作目录 + 请求 uid」重发先取消上一次；uid 为空的草稿单独登记（互不取消）。
func (a *App) registerSend(root, uid string, it *inflightSend) {
	it.root, it.uid = root, uid
	a.mu.Lock()
	defer a.mu.Unlock()
	if uid == "" {
		a.draftSends = append(a.draftSends, it)
		return
	}
	if a.sendCancels == nil {
		a.sendCancels = map[string]*inflightSend{}
	}
	key := sendKey(root, uid)
	if prev, ok := a.sendCancels[key]; ok {
		prev.cancel()
	}
	a.sendCancels[key] = it
}

// unregisterSend 发送结束时移除登记（指针身份比对：不误删同一 uid 的新一轮发送）。
func (a *App) unregisterSend(it *inflightSend) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if it.uid != "" {
		if key := sendKey(it.root, it.uid); a.sendCancels[key] == it {
			delete(a.sendCancels, key)
		}
		return
	}
	for i, x := range a.draftSends {
		if x == it {
			a.draftSends = append(a.draftSends[:i], a.draftSends[i+1:]...)
			return
		}
	}
}

// sendKey 在途发送登记键：工作目录标识 + 请求 uid（目录标识是路径，用 \n 分隔避免歧义）。
func sendKey(root, uid string) string { return root + "\n" + uid }

// CancelSend 取消按 uid 标识的在途发送；uid 为空表示取消活动根下「未落盘草稿」的发送。
//
// uid 非空时优先命中活动根，其次再在所有已打开的根里找（前端拿到的是请求 uid，
// 可能来自非活动根）：按「目录+uid」登记、按 uid 匹配，既不会误取消别的目录里的同名 uid，
// 也不会因为用户在这期间切了根而取消不掉。
func (a *App) CancelSend(uid string) {
	a.mu.Lock()
	var targets []*inflightSend
	if uid == "" {
		keep := a.draftSends[:0]
		for _, d := range a.draftSends {
			if d.root == a.activeRoot {
				targets = append(targets, d)
			} else {
				keep = append(keep, d)
			}
		}
		a.draftSends = keep
	} else if it, ok := a.sendCancels[sendKey(a.activeRoot, uid)]; ok {
		// 优先取消活动根里的那一条：多根并存时两个目录完全可能有同名 uid（整目录拷贝）
		targets = append(targets, it)
		delete(a.sendCancels, sendKey(a.activeRoot, uid))
	} else {
		suffix := "\n" + uid
		for k, it := range a.sendCancels {
			if strings.HasSuffix(k, suffix) {
				targets = append(targets, it)
				delete(a.sendCancels, k)
			}
		}
	}
	a.mu.Unlock()
	for _, it := range targets {
		it.cancel()
	}
}

// sendOptions 把全局设置 + Cookie 罐合成为发送策略。
func (a *App) sendOptions() runner.Options {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.settings
	return runner.NewOptions(runner.OptionsInput{
		InsecureSSL:     s.InsecureSSL,
		TimeoutSec:      s.TimeoutSec,
		FollowRedirects: s.FollowRedirects,
		MaxRedirects:    s.MaxRedirects,
		Jar:             a.jar,
		ProxyURL:        s.ProxyURL,
	})
}

// ensureJar 惰性创建 Cookie 罐（是否落盘由全局设置决定）。
func (a *App) ensureJar() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.jar != nil {
		return
	}
	jar, err := cookiejar.New(a.settings.PersistCookies)
	if err == nil {
		a.jar = jar
	}
}

// GetSettings 读取全局设置（以磁盘为准）。
func (a *App) GetSettings() (*config.Settings, error) {
	s, err := config.Load()
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.settings = s
	a.mu.Unlock()
	return &s, nil
}

// SaveSettings 保存全局设置；Cookie 持久化开关变化时重建 Cookie 罐。
func (a *App) SaveSettings(s *config.Settings) error {
	if s == nil {
		return errors.New("设置为空")
	}
	norm := s.Normalize()
	if err := config.Save(norm); err != nil {
		return err
	}
	a.mu.Lock()
	jarChanged := a.settings.PersistCookies != norm.PersistCookies
	a.settings = norm
	if jarChanged {
		a.jar = nil
	}
	a.mu.Unlock()
	// MCP 分区改动后重启内嵌服务（地址/端口/令牌/只读/开关都走这里）
	a.applyMCP("")
	return nil
}

// ---------- 回收站逻辑见同包 trash.go（trashColl / ListTrashFor / PreviewTrashFor / DeleteFromTrash / RestoreTrash）----------

// ListCookies 当前 Cookie 罐内容。
func (a *App) ListCookies() ([]cookiejar.Info, error) {
	a.ensureJar()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.jar == nil {
		return []cookiejar.Info{}, nil
	}
	return a.jar.List(), nil
}

// ClearCookies 清空 Cookie 罐（含磁盘）。
func (a *App) ClearCookies() error {
	a.ensureJar()
	a.mu.Lock()
	jar := a.jar
	a.mu.Unlock()
	if jar == nil {
		return nil
	}
	return jar.Clear()
}

// DeleteCookie 删除单条 Cookie（Cookie 管理面板的行内删除）。
func (a *App) DeleteCookie(domain, name, path string) error {
	a.ensureJar()
	a.mu.Lock()
	jar := a.jar
	a.mu.Unlock()
	if jar == nil {
		return nil
	}
	return jar.Delete(domain, name, path)
}

// ResolveResult 文本渲染预览。
type ResolveResult struct {
	Text    string   `json:"text"`
	Missing []string `json:"missing"`
	// Values 文本里引用到的、已定义变量的取值（名字 → 值）：地址栏悬停变量时展示。
	// 未定义的变量不出现（见 Missing），敏感变量在此为明文，展示掩码由前端按环境里的 secret 标记决定。
	Values map[string]string `json:"values"`
}

// ResolveText 把文本按当前环境渲染（用于地址栏变量悬停提示与缺失变量告警）。
func (a *App) ResolveText(text, envName string) (*ResolveResult, error) {
	vars, err := a.envVars(envName)
	if err != nil {
		return nil, err
	}
	out, missing := varx.Resolve(text, vars)
	return &ResolveResult{Text: out, Missing: missing, Values: varx.CollectValues(text, vars)}, nil
}

// ---- 代码生成（H6）----

// GenerateCode 生成请求代码片段：HTTP 支持 curl / fetch / axios / go / python，
// gRPC 只支持 grpcurl（G11.5）。变量按当前环境渲染；body / 消息按类型序列化为字符串。
func (a *App) GenerateCode(lang, envName string, r *collection.Request) (string, error) {
	if r == nil {
		return "", errors.New("请求为空")
	}
	if r.IsGRPC() {
		return a.generateGrpcCode(lang, envName, r)
	}
	l, ok := codegen.ParseLang(lang)
	if !ok {
		return "", fmt.Errorf("不支持的代码语言：%s", lang)
	}
	vars, err := a.envVars(envName)
	if err != nil {
		return "", err
	}
	url, _ := varx.Resolve(strings.TrimSpace(r.URL), vars)
	// 查询参数拼进 URL
	if u, err := urlpkg.Parse(url); err == nil {
		q := u.Query()
		for _, p := range r.Params {
			if !p.Enabled || p.Name == "" {
				continue
			}
			name, _ := varx.Resolve(p.Name, vars)
			val, _ := varx.Resolve(p.Value, vars)
			q.Set(name, val)
		}
		u.RawQuery = q.Encode()
		url = u.String()
	}

	headers := make([]codegen.KV, 0, len(r.Headers))
	for _, h := range r.Headers {
		if !h.Enabled || h.Name == "" {
			continue
		}
		name, _ := varx.Resolve(h.Name, vars)
		val, _ := varx.Resolve(h.Value, vars)
		headers = append(headers, codegen.KV{Name: name, Value: val})
	}
	// 认证头
	if r.Auth != nil {
		switch strings.ToLower(r.Auth.Type) {
		case "basic":
			user, _ := varx.Resolve(r.Auth.Username, vars)
			pass, _ := varx.Resolve(r.Auth.Password, vars)
			raw := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
			headers = append(headers, codegen.KV{Name: "Authorization", Value: "Basic " + raw})
		case "bearer":
			if tok, _ := varx.Resolve(r.Auth.Token, vars); tok != "" {
				headers = append(headers, codegen.KV{Name: "Authorization", Value: "Bearer " + tok})
			}
		case "apikey":
			key, _ := varx.Resolve(r.Auth.Key, vars)
			val, _ := varx.Resolve(r.Auth.Value, vars)
			switch {
			case key == "":
			// in=query 的 API Key 是拼进地址的（与 runner.applyAuth 一致）：
			// 之前这里既不生成头也不拼 query，代码片段与真实请求不一致（少了密钥）。
			case strings.EqualFold(r.Auth.In, "query"):
				url = appendQueryParam(url, key, val)
			default:
				headers = append(headers, codegen.KV{Name: key, Value: val})
			}
		}
	}

	body := renderBodyForCode(r.Body, vars)
	return codegen.Snippet(l, codegen.Request{
		Method:  strings.ToUpper(strings.TrimSpace(r.Method)),
		URL:     url,
		Headers: headers,
		Body:    body,
	})
}

// appendQueryParam 往 URL 上追加一个查询参数（URL 解析失败时退回字符串拼接）。
func appendQueryParam(raw, key, val string) string {
	u, err := urlpkg.Parse(raw)
	if err != nil {
		sep := "?"
		if strings.Contains(raw, "?") {
			sep = "&"
		}
		return raw + sep + urlpkg.QueryEscape(key) + "=" + urlpkg.QueryEscape(val)
	}
	q := u.Query()
	q.Set(key, val)
	u.RawQuery = q.Encode()
	return u.String()
}

// renderBodyForCode 把请求体序列化成可放进代码片段的字符串。
func renderBodyForCode(b collection.Body, vars map[string]string) string {
	switch strings.ToLower(b.Type) {
	case "json", "text":
		out, _ := varx.Resolve(b.Raw, vars)
		return out
	case "form":
		form := urlpkg.Values{}
		for _, kv := range b.Form {
			if !kv.Enabled || kv.Name == "" {
				continue
			}
			name, _ := varx.Resolve(kv.Name, vars)
			val, _ := varx.Resolve(kv.Value, vars)
			form.Set(name, val)
		}
		return form.Encode()
	case "multipart":
		// 代码片段只给字段清单提示（真实文件读盘不适合塞进片段）
		var parts []string
		for _, kv := range b.Form {
			if !kv.Enabled || kv.Name == "" {
				continue
			}
			name, _ := varx.Resolve(kv.Name, vars)
			if strings.EqualFold(kv.Type, "file") {
				parts = append(parts, name+"=@"+kv.Value)
			} else {
				val, _ := varx.Resolve(kv.Value, vars)
				parts = append(parts, name+"="+val)
			}
		}
		return strings.Join(parts, "&")
	}
	return ""
}

// ---- 导出（H10）----

// ExportDoc 导出集合（format：markdown | html | apidocgo）。
// desktop 弹「另存为」；headless 落 headlessDir。返回保存路径（取消时为空串）。
func (a *App) ExportDoc(format, defaultName string) (string, error) {
	c, err := a.requireCollection()
	if err != nil {
		return "", err
	}
	var content string
	var raw []byte
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "markdown", "md":
		content, err = c.ExportMarkdown()
	case "html":
		content, err = c.ExportHTML()
	case "apidocgo":
		// 集合整包（目录 + 请求 + 文档），可被其他 api-doc-go 客户端直接导入
		raw, err = c.ExportBundle()
	default:
		return "", fmt.Errorf("不支持的导出格式：%s（仅支持 markdown / html / apidocgo）", format)
	}
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(defaultName)
	if name == "" {
		name = c.Name + ".md"
		switch {
		case strings.EqualFold(format, "html"):
			name = c.Name + ".html"
		case strings.EqualFold(format, "apidocgo"):
			name = c.Name + ".apidocgo.json"
		}
	}
	var path string
	if a.headlessDir != "" {
		path = filepath.Join(a.headlessDir, filepath.Base(name))
	} else if ctx := a.appCtx(); ctx != nil {
		picked, err := runtime.SaveFileDialog(ctx, runtime.SaveDialogOptions{
			Title:           "导出",
			DefaultFilename: name,
		})
		if err != nil {
			return "", err
		}
		if picked == "" {
			return "", nil
		}
		path = picked
	} else {
		return "", errors.New("应用未就绪")
	}
	if raw == nil {
		raw = []byte(content)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", fmt.Errorf("写入导出文件: %w", err)
	}
	return path, nil
}

// ---- 自绘标题栏的窗口控制（无边框窗口下替代系统装饰）----
// ctx 为空表示非桌面运行环境（devserver），此时静默忽略，避免 runtime 空指针。

// WindowMinimise 最小化窗口。
func (a *App) WindowMinimise() {
	ctx := a.appCtx()
	if ctx == nil {
		return
	}
	runtime.WindowMinimise(ctx)
}

// WindowToggleMaximise 最大化 / 还原窗口。
func (a *App) WindowToggleMaximise() {
	ctx := a.appCtx()
	if ctx == nil {
		return
	}
	runtime.WindowToggleMaximise(ctx)
}

// WindowIsMaximised 当前是否最大化（标题栏按钮切换图标用）。
func (a *App) WindowIsMaximised() bool {
	ctx := a.appCtx()
	if ctx == nil {
		return false
	}
	return runtime.WindowIsMaximised(ctx)
}

// Quit 退出应用。
func (a *App) Quit() {
	ctx := a.appCtx()
	if ctx == nil {
		return
	}
	runtime.Quit(ctx)
}
