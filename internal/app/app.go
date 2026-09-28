package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/config"
	"api-doc-go-client/internal/cookiejar"
	"api-doc-go-client/internal/history"
	"api-doc-go-client/internal/runner"
	"api-doc-go-client/internal/varx"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App 暴露给前端的全部方法（Wails Bind；devserver 亦通过反射调用同一实现）。
// 约定：集合目录（文件）是真相源；前端只通过这些方法读写。
type App struct {
	ctx         context.Context
	mu          sync.Mutex
	coll        *collection.Collection
	headlessDir string // devserver 模式：PickDirectory 直接返回该目录
	settings    config.Settings
	jar         *cookiejar.Jar // 惰性创建的 Cookie 罐
}

func NewApp() *App {
	a := &App{settings: config.Default()}
	if s, err := config.Load(); err == nil {
		a.settings = s
	}
	return a
}

// startup Wails 生命周期。
// Startup Wails 生命周期钩子（必须导出：OnStartup 引用跨包方法）。
func (a *App) Startup(ctx context.Context) { a.ctx = ctx }

// SetHeadlessDir 供 devserver（无对话框环境）使用。
func (a *App) SetHeadlessDir(dir string) { a.headlessDir = dir }

func (a *App) requireCollection() (*collection.Collection, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.coll == nil {
		return nil, errors.New("尚未打开集合目录")
	}
	return a.coll, nil
}

// PickDirectory 选择集合目录（桌面 = 系统对话框；devserver = 启动参数）。
func (a *App) PickDirectory() (string, error) {
	if a.headlessDir != "" {
		return a.headlessDir, nil
	}
	if a.ctx == nil {
		return "", errors.New("应用未就绪")
	}
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "选择集合目录"})
}

// OpenCollection 打开（不存在则初始化）集合并返回概要（树 + 环境）。
func (a *App) OpenCollection(dir string) (*collection.CollectionInfo, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("目录为空")
	}
	c, err := collection.Open(dir)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.coll = c
	a.mu.Unlock()
	return c.Info()
}

// ReloadCollection 重新扫描集合（外部改动 / git 操作后）。
func (a *App) ReloadCollection() (*collection.CollectionInfo, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	return c.Info()
}

func (a *App) CreateRequest(folder, name, method string) (*collection.Request, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	return c.CreateRequest(folder, name, method)
}

// CreateFolder 在 parent（相对路径，空 = 根）下创建分组。
func (a *App) CreateFolder(parent, name string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.CreateFolder(parent, name)
}

func (a *App) RenameFolder(uid, name string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.RenameFolder(uid, name)
}

func (a *App) DeleteFolder(uid string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.DeleteFolder(uid)
}

func (a *App) RenameRequest(uid, name string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.RenameRequest(uid, name)
}

func (a *App) ReadRequest(uid string) (*collection.Request, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	return c.ReadRequest(uid)
}

func (a *App) SaveRequest(r *collection.Request) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.SaveRequest(r)
}

func (a *App) DeleteRequest(uid string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.DeleteRequest(uid)
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

func (a *App) ListEnvs() ([]collection.Env, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	return c.ListEnvs()
}

func (a *App) SaveEnv(env *collection.Env) error {
	if env == nil {
		return errors.New("环境为空")
	}
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.SaveEnv(*env)
}

func (a *App) DeleteEnv(name string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.DeleteEnv(name)
}

// envVars 取指定环境的变量表（含 secret 合并与内置动态变量）。
// 未选择环境时只给内置动态变量：不再把全部环境混在一起，避免意外命中其它环境的值。
func (a *App) envVars(envName string) (map[string]string, error) {
	vars := map[string]string{}
	if envName != "" {
		c, err := a.requireCollection()
		if err != nil {
			return nil, err
		}
		envs, err := c.ListEnvs()
		if err != nil {
			return nil, err
		}
		for _, e := range envs {
			if e.Name != envName {
				continue
			}
			for _, v := range e.Vars {
				if v.Enabled && v.Name != "" {
					vars[v.Name] = v.Value
				}
			}
		}
	}
	for k, v := range varx.Builtins() {
		vars[k] = v
	}
	return vars, nil
}

// SendRequest 按 envName 渲染并发送请求草稿，同时记入本地历史。
func (a *App) SendRequest(r *collection.Request, envName string) (*runner.Result, error) {
	if r == nil {
		return nil, errors.New("请求为空")
	}
	vars, err := a.envVars(envName)
	if err != nil {
		return nil, err
	}
	a.ensureJar()
	res, sendErr := runner.Send(*r, vars, a.sendOptions())
	a.recordHistory(r, res, sendErr)
	if sendErr != nil {
		return nil, sendErr
	}
	return res, nil
}

// sendOptions 把全局设置 + Cookie 罐合成为发送策略。
func (a *App) sendOptions() runner.Options {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.settings
	return runner.NewOptions(s.InsecureSSL, s.TimeoutSec, s.FollowRedirects, s.MaxRedirects, a.jar)
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

// recordHistory 落一条历史：失败也记，便于回溯排查。
func (a *App) recordHistory(r *collection.Request, res *runner.Result, sendErr error) {
	a.mu.Lock()
	limit := a.settings.HistoryLimit
	a.mu.Unlock()

	entry := history.Entry{
		Time: time.Now().UnixMilli(), UID: r.UID, Name: r.Name,
		Method: strings.ToUpper(r.Method), URL: r.URL,
	}
	switch {
	case sendErr != nil:
		entry.Error = sendErr.Error()
	case res != nil:
		entry.URL, entry.Status = res.URL, res.Status
		entry.TimeMS, entry.Size = res.TimeMS, res.Size
	}
	_ = history.Append(entry, limit)
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

// SaveSettings 保存全局设置；Cookie 持久化开关变化时重建 Cookie 罐，主题变化时同步窗口底色。
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
	themeChanged := a.settings.Theme != norm.Theme
	a.settings = norm
	if jarChanged {
		a.jar = nil
	}
	a.mu.Unlock()
	if themeChanged {
		a.applyWindowTheme(norm.Theme)
	}
	return nil
}

// ListHistory 最近的发送记录（新 → 旧）。
func (a *App) ListHistory(limit int) ([]history.Entry, error) {
	return history.List(limit)
}

// ClearHistory 清空发送历史。
func (a *App) ClearHistory() error {
	return history.Clear()
}

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

// ResolveResult 文本渲染预览。
type ResolveResult struct {
	Text    string   `json:"text"`
	Missing []string `json:"missing"`
}

// ResolveText 把文本按当前环境渲染（用于 URL 预览与缺失变量告警）。
func (a *App) ResolveText(text, envName string) (*ResolveResult, error) {
	vars, err := a.envVars(envName)
	if err != nil {
		return nil, err
	}
	out, missing := varx.Resolve(text, vars)
	return &ResolveResult{Text: out, Missing: missing}, nil
}

// ---- 自绘标题栏的窗口控制（无边框窗口下替代系统装饰）----
// ctx 为空表示非桌面运行环境（devserver），此时静默忽略，避免 runtime 空指针。

// WindowMinimise 最小化窗口。
func (a *App) WindowMinimise() {
	if a.ctx == nil {
		return
	}
	runtime.WindowMinimise(a.ctx)
}

// WindowToggleMaximise 最大化 / 还原窗口。
func (a *App) WindowToggleMaximise() {
	if a.ctx == nil {
		return
	}
	runtime.WindowToggleMaximise(a.ctx)
}

// WindowIsMaximised 当前是否最大化（标题栏按钮切换图标用）。
func (a *App) WindowIsMaximised() bool {
	if a.ctx == nil {
		return false
	}
	return runtime.WindowIsMaximised(a.ctx)
}

// Quit 退出应用。
func (a *App) Quit() {
	if a.ctx == nil {
		return
	}
	runtime.Quit(a.ctx)
}
