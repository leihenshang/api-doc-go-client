package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	urlpkg "net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/config"
	"api-doc-go-client/internal/cookiejar"
	"api-doc-go-client/internal/history"
	"api-doc-go-client/internal/index"
	"api-doc-go-client/internal/mocksrv"
	"api-doc-go-client/internal/proto"
	"api-doc-go-client/internal/runner"
	"api-doc-go-client/internal/script"
	"api-doc-go-client/internal/syncengine"
	"api-doc-go-client/internal/varx"

	"github.com/leihenshang/api-doc-go-share/codegen"
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
	// sendCancels 按请求 uid 记录在途发送（指针身份用于避免旧请求结束时误删新请求）。
	sendCancels map[string]*inflightSend
	// mock 本地 Mock 服务（H7）
	mock *mocksrv.Server
	// syncToken 当前会话的 PAT（不落盘）
	syncToken string
	// syncStatus 状态栏同步状态
	syncStatus SyncStatus
	// syncStop 定时同步停止信号
	syncStop chan struct{}
	// protoCache gRPC 定义编译缓存：显式失效（用户导入/更新定义时 Invalidate），不做文件监听
	protoCache *proto.Cache
}

// inflightSend 一次在途发送的取消句柄。
type inflightSend struct {
	cancel context.CancelFunc
}

func NewApp() *App {
	a := &App{settings: config.Default(), protoCache: proto.NewCache()}
	if s, err := config.Load(); err == nil {
		a.settings = s
	}
	return a
}

// 初始窗口几何（逻辑像素；Wails 的 Width/Height 与 ScreenGetAll 都是逻辑像素）。
const (
	preferredWindowWidth  = 1600
	preferredWindowHeight = 1000
	// 首选尺寸四周留白下限：避免窗口贴着屏幕边缘
	windowFitMargin = 24
	// 任务栏 / Dock 的保守预留：ScreenGetAll 只给显示器矩形，不含工作区
	windowVerticalReserve = 64
)

// startup Wails 生命周期。
// Startup Wails 生命周期钩子（必须导出：OnStartup 引用跨包方法）。
func (a *App) Startup(ctx context.Context) { a.ctx = ctx }

// DomReady Wails 生命周期钩子：窗口与 WebView 就绪、首帧渲染前，把初始几何收敛到屏幕内再显示窗口。
//
// 为什么需要：Wails 的 Width/Height 是**逻辑像素**，高分屏（如 125% 缩放）下逻辑屏可能只有
// 1536×960，而首选 1600×1000 放不下 —— 系统会把它按工作区居中，于是顶部/底部被推出屏幕
// （实测：逻辑屏 1536×960 时窗口矩形 T=-33、B=945）。ScreenGetAll 返回的 Size 恰好是逻辑像素，
// 用它判断：放得下就保持首选尺寸并居中，放不下就直接最大化（Windows 侧 Wails 已按工作区裁剪客户区，
// 任务栏自动让开）。窗口先隐藏（main.go 的 StartHidden），自适应完再 Show，避免启动闪一下再跳。
func (a *App) DomReady(ctx context.Context) {
	a.ctx = ctx
	a.fitInitialWindow(ctx)
	runtime.WindowShow(ctx)
}

// fitInitialWindow 按主屏可用范围决定初始尺寸：放得下 = 首选尺寸居中，放不下 = 最大化。
func (a *App) fitInitialWindow(ctx context.Context) {
	screens, err := runtime.ScreenGetAll(ctx)
	if err != nil || len(screens) == 0 {
		return // 拿不到屏幕信息：保持 main.go 里的安全下限尺寸，交给系统摆放
	}
	screen := screens[0]
	for _, s := range screens {
		if s.IsPrimary {
			screen = s
			break
		}
	}
	if windowFitsScreen(screen.Size.Width, screen.Size.Height) {
		runtime.WindowSetSize(ctx, preferredWindowWidth, preferredWindowHeight)
		runtime.WindowCenter(ctx)
		return
	}
	runtime.WindowMaximise(ctx)
}

// windowFitsScreen 首选尺寸能否完整放进该屏幕（含任务栏预留与四周留白）。
func windowFitsScreen(screenW, screenH int) bool {
	return screenW >= preferredWindowWidth+windowFitMargin &&
		screenH >= preferredWindowHeight+windowVerticalReserve
}

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

// PickFile 选择本地文件（multipart 上传用）。
func (a *App) PickFile() (string, error) {
	if a.ctx == nil {
		return "", errors.New("应用未就绪")
	}
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "选择上传文件"})
}

// PickGrpcProtoFiles 选择 .proto 定义（可多选，带 .proto 过滤）：gRPC 请求的「导入 / 更新定义」用。
//
// 必须支持多选：一个服务的定义常拆在多个文件（service 与 message 分文件 + 同目录互相 import），
// 分开导入会因「import 依赖还没进集合」而解析失败（设计文档 D2/G2.2）。
func (a *App) PickGrpcProtoFiles() ([]string, error) {
	if a.ctx == nil {
		return nil, errors.New("应用未就绪")
	}
	return runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "选择 .proto 定义",
		Filters: []runtime.FileFilter{{DisplayName: "Proto 定义 (*.proto)", Pattern: "*.proto"}},
	})
}

// OpenCollection 打开（不存在则初始化）集合并返回概要（树 + 环境）。
func (a *App) OpenCollection(dir string) (*collection.CollectionInfo, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("目录为空")
	}
	a.mu.Lock()
	if a.coll != nil {
		a.coll.StopWatch()
	}
	a.mu.Unlock()
	c, err := collection.Open(dir)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.coll = c
	a.mu.Unlock()
	// 换集合即丢弃编译缓存：缓存按绝对路径存结果，跨集合复用没有意义（也避免同路径陈旧结果）
	a.protoCache.Invalidate()
	go a.watchLoop(c)
	return c.Info()
}

// watchLoop 把外部改动事件转发给前端（Wails EventsEmit；devserver 无 ctx 时静默）。
func (a *App) watchLoop(c *collection.Collection) {
	ch := c.WatchEvents()
	if ch == nil {
		return
	}
	for ev := range ch {
		a.mu.Lock()
		ctx := a.ctx
		still := a.coll == c
		a.mu.Unlock()
		if !still {
			return
		}
		// 同步重建索引，保证搜索结果与磁盘一致
		_ = c.RebuildIndex()
		if ctx != nil {
			runtime.EventsEmit(ctx, "collection:changed", map[string]any{"paths": ev.Paths})
		}
	}
}

// ReloadCollection 重新扫描集合（外部改动 / git 操作后）。
func (a *App) ReloadCollection() (*collection.CollectionInfo, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	_ = c.RebuildIndex()
	return c.Info()
}

// SearchIndex 本地索引搜索（C1/H4）：按标题/URL/方法/路径模糊匹配。
// limit <= 0 时取默认 200。
func (a *App) SearchIndex(q string, limit int) ([]index.Node, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 200
	}
	return c.Search(q, limit)
}

// RebuildIndex 手动重建本地索引（删库后可一键恢复）。
func (a *App) RebuildIndex() error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.RebuildIndex()
}

// ---- 本地 Mock 服务（H7）----

// StartMock 启动本地 Mock（port<=0 随机端口）。
func (a *App) StartMock(port int) (*mocksrv.Status, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	if a.mock == nil {
		a.mock = mocksrv.New(c)
	}
	m := a.mock
	a.mu.Unlock()
	return m.Start(port)
}

// StopMock 停止本地 Mock。
func (a *App) StopMock() error {
	a.mu.Lock()
	m := a.mock
	a.mu.Unlock()
	if m == nil {
		return nil
	}
	return m.Stop()
}

// MockStatus Mock 运行状态。
func (a *App) MockStatus() *mocksrv.Status {
	a.mu.Lock()
	m := a.mock
	a.mu.Unlock()
	if m == nil {
		return &mocksrv.Status{Running: false}
	}
	return m.Status()
}

// ---- 同步（C7 / I1–I6）----

// SyncBindInfo 绑定信息（不含 PAT）。
type SyncBindInfo = syncengine.BindInfo

// SyncReport 一轮同步摘要。
type SyncReport = syncengine.Report

// GetSyncBind 读取当前集合的同步绑定。
func (a *App) GetSyncBind() (*syncengine.BindInfo, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	b, err := syncengine.LoadBind(c.Dir)
	if err != nil {
		// 未绑定不是错误
		return &syncengine.BindInfo{}, nil
	}
	return &b, nil
}

// SetSyncBind 绑定/更新服务端项目（PAT 只存内存，落盘由调用方决定）。
func (a *App) SetSyncBind(serverURL string, projectID uint64, mode, token string) (*syncengine.BindInfo, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(serverURL) == "" || projectID == 0 {
		return nil, errors.New("服务端地址与项目 ID 必填")
	}
	if mode == "" {
		mode = syncengine.ModeAuto
	}
	b := syncengine.BindInfo{
		Linked: true, ServerURL: strings.TrimRight(serverURL, "/"),
		ProjectID: projectID, Mode: mode,
	}
	// 继承已有 cursor
	if prev, err := syncengine.LoadBind(c.Dir); err == nil && prev.ProjectID == projectID {
		b.Cursor = prev.Cursor
	}
	if err := syncengine.SaveBind(c.Dir, b); err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.syncToken = token
	a.mu.Unlock()
	return &b, nil
}

// UnbindSync 解绑（文件保留）。
func (a *App) UnbindSync() error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	b := syncengine.BindInfo{}
	if err := syncengine.SaveBind(c.Dir, b); err != nil {
		return err
	}
	a.mu.Lock()
	a.syncToken = ""
	a.mu.Unlock()
	return nil
}

// RunSync 执行一轮同步（先 pull 后 push）。
func (a *App) RunSync(token string) (*syncengine.Report, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	b, err := syncengine.LoadBind(c.Dir)
	if err != nil || !b.Linked {
		return nil, errors.New("当前集合未关联服务端项目")
	}
	tok := token
	if tok == "" {
		a.mu.Lock()
		tok = a.syncToken
		a.mu.Unlock()
	}
	if tok == "" {
		return nil, errors.New("缺少访问令牌（PAT）")
	}
	cli := syncengine.New(b.ServerURL, tok, syncengine.DeviceID())
	eng := &syncengine.Engine{Coll: c, Bind: b, CLI: cli}

	a.setSyncRunning(true)
	rep, err := eng.RunOne()
	a.setSyncRunning(false)

	// 回写 cursor
	b.Cursor = eng.Bind.Cursor
	_ = syncengine.SaveBind(c.Dir, b)
	// 同步后重建索引
	_ = c.RebuildIndex()

	// 记状态
	a.mu.Lock()
	if err != nil {
		a.syncStatus.LastError = err.Error()
	} else {
		a.syncStatus.LastError = ""
		a.syncStatus.LastSyncAt = time.Now().UnixMilli()
		if rep != nil {
			a.syncStatus.LastReport = rep
		}
	}
	ctx := a.ctx
	a.mu.Unlock()
	if ctx != nil && err == nil {
		runtime.EventsEmit(ctx, "collection:changed", map[string]any{"paths": []string{"sync"}})
	}
	return rep, err
}

// ---- 同步状态（T31：状态栏展示）----

// SyncStatus 状态栏用的同步状态。
type SyncStatus struct {
	Linked     bool               `json:"linked"`
	Mode       string             `json:"mode"`
	Running    bool               `json:"running"`
	LastSyncAt int64              `json:"lastSyncAt"` // Unix 毫秒
	LastError  string             `json:"lastError"`
	DirtyCount int                `json:"dirtyCount"`
	Conflicts  int                `json:"conflicts"`
	Cursor     int64              `json:"cursor"`
	LastReport *syncengine.Report `json:"lastReport,omitempty"`
}

// GetSyncStatus 当前同步状态（供状态栏轮询）。
func (a *App) GetSyncStatus() *SyncStatus {
	c, err := a.requireCollection()
	if err != nil {
		return &SyncStatus{}
	}
	b, _ := syncengine.LoadBind(c.Dir)
	dirty, _ := c.DirtyNodes()
	confs, _ := c.ListConflicts()

	a.mu.Lock()
	st := a.syncStatus
	a.mu.Unlock()

	st.Linked = b.Linked
	st.Mode = b.Mode
	st.Cursor = b.Cursor
	st.DirtyCount = len(dirty)
	st.Conflicts = len(confs)
	return &st
}

func (a *App) setSyncRunning(v bool) {
	a.mu.Lock()
	a.syncStatus.Running = v
	a.mu.Unlock()
}

// StartAutoSync 启动定时自动同步（mode=auto 时每 intervalSec 一轮）。
// token 可为空（用会话里已存的）。
func (a *App) StartAutoSync(intervalSec int) {
	if intervalSec < 30 {
		intervalSec = 120 // 默认 2 分钟
	}
	a.mu.Lock()
	if a.syncStop != nil {
		close(a.syncStop)
		a.syncStop = nil
	}
	stop := make(chan struct{})
	a.syncStop = stop
	a.mu.Unlock()

	go func() {
		t := time.NewTicker(time.Duration(intervalSec) * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				a.autoSyncOnce()
			}
		}
	}()
}

// StopAutoSync 停止定时同步。
func (a *App) StopAutoSync() {
	a.mu.Lock()
	if a.syncStop != nil {
		close(a.syncStop)
		a.syncStop = nil
	}
	a.mu.Unlock()
}

// autoSyncOnce 一轮自动同步：仅 auto 模式且已绑定、未在跑时执行。
func (a *App) autoSyncOnce() {
	a.mu.Lock()
	running := a.syncStatus.Running
	token := a.syncToken
	a.mu.Unlock()
	if running {
		return
	}
	c, err := a.requireCollection()
	if err != nil {
		return
	}
	b, err := syncengine.LoadBind(c.Dir)
	if err != nil || !b.Linked || b.Mode != syncengine.ModeAuto {
		return
	}
	if token == "" {
		return
	}
	_, _ = a.RunSync(token)
}

// ListConflicts 列出 .conflicts/ 冲突副本（I4）。
func (a *App) ListConflicts() ([]collection.ConflictItem, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	return c.ListConflicts()
}

// ResolveConflict 冲突三选一：local | remote | copy。
func (a *App) ResolveConflict(file, choice string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.ResolveConflict(file, choice)
}

// ---- 文档条目（B13）----

// ListDocs 列出全部文档条目。
func (a *App) ListDocs() ([]*collection.DocEntry, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	return c.ListDocs()
}

// ReadDoc 读一条文档。
func (a *App) ReadDoc(uid string) (*collection.DocEntry, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	return c.ReadDoc(uid)
}

// CreateDoc 新建文档条目。
func (a *App) CreateDoc(name string) (*collection.DocEntry, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	return c.CreateDoc(name, "")
}

// SaveDoc 保存文档。
func (a *App) SaveDoc(d *collection.DocEntry) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.SaveDoc(d)
}

// DeleteDoc 删除文档。
func (a *App) DeleteDoc(uid string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.DeleteDoc(uid)
}

// SyncDocAssets 拉取文档引用的附件到本地 assets/（R9）。
// 文档正文里的 /uploads/xxx 或 http(s) 图片，按需下载。
func (a *App) SyncDocAssets(token string) (int, error) {
	c, err := a.requireCollection()
	if err != nil {
		return 0, err
	}
	docs, err := c.ListDocs()
	if err != nil {
		return 0, err
	}
	a.mu.Lock()
	tok := token
	if tok == "" {
		tok = a.syncToken
	}
	b, _ := syncengine.LoadBind(c.Dir)
	a.mu.Unlock()
	if tok == "" || !b.Linked {
		return 0, errors.New("未关联服务端或缺少令牌")
	}
	count := 0
	for _, d := range docs {
		urls := collection.DocImageURLs(d.Content)
		for _, u := range urls {
			// 只拉 /uploads/ 或服务端 URL
			n, err := fetchAsset(c, b.ServerURL, tok, u)
			if err == nil {
				count += n
			}
		}
	}
	return count, nil
}

// fetchAsset 下载单个附件并保存到 assets/。
func fetchAsset(c *collection.Collection, serverURL, token, rawURL string) (int, error) {
	full := rawURL
	if strings.HasPrefix(rawURL, "/") {
		full = strings.TrimRight(serverURL, "/") + rawURL
	}
	if !strings.HasPrefix(full, "http") {
		return 0, nil
	}
	req, err := http.NewRequest(http.MethodGet, full, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	if c.HasAsset(hash) {
		return 0, nil
	}
	if _, err := c.SaveAsset(hash, data); err != nil {
		return 0, err
	}
	return 1, nil
}

func (a *App) CreateRequest(folder, name, method string) (*collection.Request, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	return c.CreateRequest(folder, name, method)
}

// CreateRequestFromDraft 把前端内存草稿（新建 tab 里编辑的内容）落盘成新请求。
// uid / 文件名 / seq / path 由集合层重新分配，前端只负责内容与目标分组。
func (a *App) CreateRequestFromDraft(folder, name string, r *collection.Request) (*collection.Request, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	if err := a.checkGrpcSavable(r); err != nil {
		return nil, err
	}
	return c.CreateRequestFromDraft(folder, name, r)
}

// ParseCurl 把一段 curl 命令解析成请求草稿（不落盘），供「导入 cURL」预览与新建 tab 预填。
func (a *App) ParseCurl(text string) (*collection.Request, error) {
	return collection.ParseCurl(text)
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

// MoveRequest 把请求移动到目标分组（destFolder 空 = 根）。
func (a *App) MoveRequest(uid, destFolder string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.MoveRequest(uid, destFolder)
}

// MoveFolder 把分组移动到目标父分组（destParent 空 = 根）。
func (a *App) MoveFolder(uid, destParent string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.MoveFolder(uid, destParent)
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
	if err := a.checkGrpcSavable(r); err != nil {
		return err
	}
	return c.SaveRequest(r)
}

// checkGrpcSavable gRPC 请求的保存前置校验（D4 / G3.5）：写了定义但解析不了就不允许写盘。
//
// 「还没导入 .proto」是允许保存的状态（空定义，§5 状态机），只有 proto 有值却编译失败才拒绝；
// 这样用户不会把一份打不开的请求存进集合，同时保留「先存草稿、稍后补定义」的自由。
// 导入是原子的（失败不落盘），所以解析失败只可能来自「定义被外部删除 / 改了内容」。
func (a *App) checkGrpcSavable(r *collection.Request) error {
	if r == nil || r.GRPC == nil || strings.TrimSpace(r.GRPC.Proto) == "" {
		return nil
	}
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	if _, err := a.compileResolved(c, r.GRPC.Proto, r.GRPC.Imports); err != nil {
		return fmt.Errorf("定义解析失败，无法保存：请先在 Schema 分段重新导入（%v）", err)
	}
	return nil
}

func (a *App) DeleteRequest(uid string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.DeleteRequest(uid)
}

// SaveResponseBody 把响应体保存到本地文件（E21）。
// binary 为真时 body 是 base64，写盘前解码；否则按文本原样写。
// desktop 走系统「另存为」对话框；headless 直接落在 headlessDir 下。
// 返回实际保存路径；用户取消对话框时返回空串且 error 为 nil。
func (a *App) SaveResponseBody(defaultName string, binary bool, body string) (string, error) {
	name := strings.TrimSpace(defaultName)
	if name == "" {
		name = "response.bin"
	}
	var path string
	if a.headlessDir != "" {
		path = filepath.Join(a.headlessDir, filepath.Base(name))
	} else if a.ctx != nil {
		picked, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
			Title:           "保存响应体",
			DefaultFilename: name,
		})
		if err != nil {
			return "", err
		}
		if picked == "" {
			return "", nil // 用户取消
		}
		path = picked
	} else {
		return "", errors.New("应用未就绪")
	}

	var data []byte
	if binary {
		decoded, err := base64.StdEncoding.DecodeString(body)
		if err != nil {
			return "", fmt.Errorf("解码二进制响应失败: %w", err)
		}
		data = decoded
	} else {
		data = []byte(body)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("写入响应文件: %w", err)
	}
	return path, nil
}

// ---- 导入（Postman / OpenAPI，共享内核见 share 的 postman / openapi 包）----

// ImportCollection 把 Postman 或 OpenAPI 文本导入到 parent 分组下（format：postman / openapi）。
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
	case "bruno":
		return nil, fmt.Errorf("Bruno 请使用 ImportBrunoDir 选择目录导入")
	}
	return nil, fmt.Errorf("不支持的导入格式：%s（仅支持 postman / openapi）", format)
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
	it := &inflightSend{cancel: cancel}
	a.mu.Lock()
	if a.sendCancels == nil {
		a.sendCancels = map[string]*inflightSend{}
	}
	if prev, ok := a.sendCancels[r.UID]; ok {
		prev.cancel() // 同一请求重发：先取消上一次
	}
	a.sendCancels[r.UID] = it
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		if a.sendCancels[r.UID] == it {
			delete(a.sendCancels, r.UID)
		}
		a.mu.Unlock()
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

// CancelSend 取消按 uid 标识的在途发送；无在途发送时为空操作。
func (a *App) CancelSend(uid string) {
	a.mu.Lock()
	it := a.sendCancels[uid]
	delete(a.sendCancels, uid)
	a.mu.Unlock()
	if it != nil {
		it.cancel()
	}
}

// sendOptions 把全局设置 + Cookie 罐合成为发送策略。
func (a *App) sendOptions() runner.Options {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.settings
	return runner.NewOptions(s.InsecureSSL, s.TimeoutSec, s.FollowRedirects, s.MaxRedirects, a.jar, s.ProxyURL)
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

// recordHistory 落一条历史：失败也记，便于回溯排查；带请求快照供原样重放。
func (a *App) recordHistory(r *collection.Request, res *runner.Result, sendErr error) {
	a.mu.Lock()
	limit := a.settings.HistoryLimit
	a.mu.Unlock()

	entry := history.Entry{
		Time: time.Now().UnixMilli(), UID: r.UID, Name: r.Name,
		Method: strings.ToUpper(r.Method), URL: r.URL,
	}
	if snap, err := json.Marshal(r); err == nil {
		entry.Request = snap
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

// ReplayHistory 用历史里的请求快照原样重发（E22）；index 为 ListHistory 的下标。
func (a *App) ReplayHistory(index int, envName string) (*runner.Result, error) {
	items, err := history.List(0)
	if err != nil {
		return nil, err
	}
	// List 返回新→旧，与前端展示一致
	if index < 0 || index >= len(items) {
		return nil, fmt.Errorf("历史记录不存在")
	}
	var r collection.Request
	if err := json.Unmarshal(items[index].Request, &r); err != nil || r.URL == "" {
		return nil, fmt.Errorf("该历史没有请求快照，无法原样重放")
	}
	return a.SendRequest(&r, envName)
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
			if key != "" && !strings.EqualFold(r.Auth.In, "query") {
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

// ExportDoc 导出集合文档（format：markdown | html）。
// desktop 弹「另存为」；headless 落 headlessDir。返回保存路径（取消时为空串）。
func (a *App) ExportDoc(format, defaultName string) (string, error) {
	c, err := a.requireCollection()
	if err != nil {
		return "", err
	}
	var content string
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "markdown", "md":
		content, err = c.ExportMarkdown()
	case "html":
		content, err = c.ExportHTML()
	default:
		return "", fmt.Errorf("不支持的导出格式：%s（仅支持 markdown / html）", format)
	}
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(defaultName)
	if name == "" {
		if strings.EqualFold(format, "html") {
			name = c.Name + ".html"
		} else {
			name = c.Name + ".md"
		}
	}
	var path string
	if a.headlessDir != "" {
		path = filepath.Join(a.headlessDir, filepath.Base(name))
	} else if a.ctx != nil {
		picked, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
			Title:           "导出文档",
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
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("写入导出文件: %w", err)
	}
	return path, nil
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
