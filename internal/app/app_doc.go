package app

// 文档条目（P2-8：从 app.go 按子域拆出）。

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	urlpkg "net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/syncengine"

	runtime "github.com/wailsapp/wails/v2/pkg/runtime"
)

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
// CreateDoc 新建文档条目（folder 为空 = docs/ 默认位；B19 起可为任意分组目录）。
func (a *App) CreateDoc(name, folder string) (*collection.DocEntry, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	return c.CreateDoc(name, "", folder)
}

// CreateDocEx 新建文档（MCP create_doc）：冲突策略由调用方决定（默认 error 在 MCP 侧）。
// 前端「新建文档」仍走 CreateDoc（重名自动 -2，历史行为）。
func (a *App) CreateDocEx(name, uid, folder, onConflict string) (*collection.DocEntry, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	return c.CreateDocEx(name, uid, folder, onConflict)
}

// PlanCreateDoc 只做校验/冲突判定，不落盘（MCP create_doc 的 dryRun）。
func (a *App) PlanCreateDoc(name, uid, folder, onConflict string) (*collection.DocEntry, string, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, "", err
	}
	return c.PlanCreateDoc(name, uid, folder, onConflict)
}

// CreateDocWithOpts 同 CreateDocEx，但可显式指定落盘文件名（MCP create_doc 的 fileName）。
func (a *App) CreateDocWithOpts(o collection.CreateDocOpts) (*collection.DocEntry, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	return c.CreateDocWithOpts(o)
}

// PlanCreateDocWithOpts 同 PlanCreateDoc，但可显式指定落盘文件名（MCP create_doc dryRun）。
func (a *App) PlanCreateDocWithOpts(o collection.CreateDocOpts) (*collection.DocEntry, string, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, "", err
	}
	return c.PlanCreateDocWithOpts(o)
}

// SaveDoc 保存文档。
func (a *App) SaveDoc(d *collection.DocEntry) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.SaveDoc(d)
}

// SaveDocExpectHash 带期望哈希保存文档（B19）：磁盘 hash 与 expectHash 不一致时
// 返回以 [conflict] 开头的错误，供文档页签复用请求那套「冲突条」交互。
func (a *App) SaveDocExpectHash(d *collection.DocEntry, expectHash string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.SaveDocChecked(d, expectHash)
}

// DocFileHash 文档文件的当前磁盘 hash（B19：文档页签保存前/外部改动比对用）。
func (a *App) DocFileHash(uid string) string {
	c, err := a.requireCollection()
	if err != nil {
		return ""
	}
	return c.DocFileHash(uid)
}

// FileHash 任意集合内相对路径的内容 hash（不存在返回空串），供 MCP 写前校验。
func (a *App) FileHash(rel string) string {
	c, err := a.requireCollection()
	if err != nil {
		return ""
	}
	return c.FileHashOf(rel)
}

// DocHashes 一次扫描取全部文档 hash（MCP list_docs 批量用，避免逐篇重扫）。
func (a *App) DocHashes() (map[string]string, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	return c.DocHashes()
}

// DeleteDoc 删除文档。
func (a *App) DeleteDoc(uid string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.DeleteDoc(uid)
}

// MoveDoc 把文档移动到目标分组目录（destFolder 空 = 根；B19）。
func (a *App) MoveDoc(uid, destFolder string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.MoveDoc(uid, destFolder)
}

// RenameDoc 改文档显示名（只改 frontmatter name，文件名不变；B19）。
func (a *App) RenameDoc(uid, name string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.RenameDoc(uid, name)
}

// SyncDocAssets 拉取文档引用的附件到本地 assets/（R9）。
//
// 预留 API：文档条目在客户端尚无 UI 入口（见《客户端代码分析问题》F-03，
// DocEditor.vue 是孤儿组件），所以本方法目前只在自动生成的 wailsjs 绑定里暴露，
// 没有前端/内部调用方。保留实现而非删除的原因：它是 fetchAsset/assetURL
// 「附件只允许从绑定服务端同源下载」这条安全约束的唯一入口，删掉会让该约束失去载体。
// 待文档 UI 上线后由文档页或设置页触发。
//
// 只下载「绑定服务端的同源地址」（相对路径 /uploads/xxx 按服务端地址补全）：
// 文档正文既可能来自本地、也可能由同步服务端下发，若允许任意 http(s) 地址，
// 一个恶意文档就能把用户的访问令牌（Bearer）送给第三方域名，同时构成 SSRF。
// 非同源地址一律跳过（计入 skipped，不报错，避免一条坏链接中断整次同步）。
func (a *App) SyncDocAssets(token string) (int, error) {
	c, err := a.requireCollection()
	if err != nil {
		return 0, err
	}
	b, err := syncengine.LoadBind(c.Dir)
	if err != nil || !b.Linked {
		return 0, errors.New("未关联服务端或缺少令牌")
	}
	tok := token
	if tok == "" {
		tok = a.syncTokenOf(c)
	}
	if tok == "" {
		tok = b.Token
	}
	if tok == "" {
		return 0, errors.New("未关联服务端或缺少令牌")
	}
	return syncDocAssets(c, b.ServerURL, tok)
}

// syncDocAssets 把集合内文档引用的同源图片镜像到 assets/（内容寻址，已存在即跳过）。
// E19 离线映射核心：由 App.SyncDocAssets 门面与 runSyncFor 成功轮后的 best-effort 共用。
// 单条失败不影响其余附件；整体错误（如令牌失效）上抛由调用方决定是否忽略。
func syncDocAssets(c *collection.Collection, serverURL, token string) (int, error) {
	docs, err := c.ListDocs()
	if err != nil {
		return 0, err
	}
	client := &http.Client{Timeout: assetFetchTimeout}
	count := 0
	for _, d := range docs {
		for _, u := range collection.DocImageURLs(d.Content) {
			n, ferr := fetchAsset(client, c, serverURL, token, u)
			if ferr != nil {
				continue // 单条失败不影响其余附件
			}
			count += n
		}
	}
	return count, nil
}

const (
	// assetFetchTimeout 附件下载超时（此前用 http.DefaultClient：无超时，坏链接会挂死）。
	assetFetchTimeout = 60 * time.Second
	// maxAssetSize 单个附件上限（此前无上限，超大响应会把内存吃光）。
	maxAssetSize = 32 << 20 // 32MB
)

// fetchAsset 下载单个附件并保存到 assets/；非服务端同源地址直接跳过。
func fetchAsset(client *http.Client, c *collection.Collection, serverURL, token, rawURL string) (int, error) {
	full, err := assetURL(serverURL, rawURL)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequest(http.MethodGet, full, nil)
	if err != nil {
		return 0, err
	}
	// 令牌只在同源（assetURL 已保证）时附加
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAssetSize))
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

// assetURL 把文档里的图片地址规范化成可下载的完整 URL：
// 仅接受「相对路径（按服务端补全）」或「与服务端同源的绝对地址」，其余返回错误。
func assetURL(serverURL, rawURL string) (string, error) {
	raw := strings.TrimSpace(rawURL)
	if raw == "" {
		return "", errors.New("空地址")
	}
	// 协议相对地址（//host/path）指向的是别的站点，必须在「相对路径」分支之前挡掉
	if strings.HasPrefix(raw, "//") {
		return "", errors.New("非同源地址已跳过")
	}
	base, err := urlpkg.Parse(strings.TrimSpace(serverURL))
	if err != nil || base.Host == "" ||
		(base.Scheme != "http" && base.Scheme != "https") {
		return "", errors.New("服务端地址无效")
	}
	if strings.HasPrefix(raw, "/") {
		return strings.TrimRight(base.String(), "/") + raw, nil
	}
	target, err := urlpkg.Parse(raw)
	if err != nil {
		return "", err
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return "", fmt.Errorf("不支持的协议: %s", target.Scheme)
	}
	if !strings.EqualFold(base.Scheme, target.Scheme) || !strings.EqualFold(base.Host, target.Host) {
		return "", errors.New("非同源地址已跳过")
	}
	return target.String(), nil
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

// CreateRequestFromDraftEx 按 uid/onConflict 落盘草稿（MCP create_request 用；前端仍走 suffix 旧行为）。
func (a *App) CreateRequestFromDraftEx(folder, name string, r *collection.Request, uid, onConflict string) (*collection.Request, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	if err := a.checkGrpcSavable(r); err != nil {
		return nil, err
	}
	return c.CreateRequestFromDraftEx(folder, name, r, uid, onConflict)
}

// PlanCreateRequest 只做冲突判定与身份/路径分配，不写盘（MCP dryRun）。
func (a *App) PlanCreateRequest(folder, name string, r *collection.Request, uid, onConflict string) (*collection.Request, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	if err := a.checkGrpcSavable(r); err != nil {
		return nil, err
	}
	return c.PlanCreateRequest(folder, name, r, uid, onConflict)
}

// CreateRequestWithOpts 同 CreateRequestFromDraftEx，但可显式指定落盘文件名（MCP create_request 的 fileName）。
func (a *App) CreateRequestWithOpts(o collection.RequestCreateOpts) (*collection.Request, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	if err := a.checkGrpcSavable(o.Src); err != nil {
		return nil, err
	}
	return c.CreateRequestWithOpts(o)
}

// PlanCreateRequestWithOpts 同 PlanCreateRequest，但可显式指定落盘文件名（MCP dryRun）。
func (a *App) PlanCreateRequestWithOpts(o collection.RequestCreateOpts) (*collection.Request, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	if err := a.checkGrpcSavable(o.Src); err != nil {
		return nil, err
	}
	return c.PlanCreateRequestWithOpts(o)
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

// DeleteFolderTree 递归删除分组（整棵进 .trash，可在回收站恢复）。
func (a *App) DeleteFolderTree(uid string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.DeleteFolderTree(uid)
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

// ArrangeNode 拖动排序：把节点放到目标分组的指定位置（beforeUID 空 = 同类型末尾）。
// 与 MoveRequest / MoveDoc / MoveFolder 的区别：后者只改上级，本方法还会按拖放位置重排同层 seq。
func (a *App) ArrangeNode(uid, destFolder, beforeUID string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.ArrangeNode(uid, destFolder, beforeUID)
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
	// 以磁盘索引里的真实路径为准：请求可能已被移动（集合树里拖动改上级目录、弹窗「移动到…」、
	// 另一个客户端窗口、或外部编辑器），而调用方手里的 path 还是旧的 —— 直接写会**在旧位置又写出一份**。
	// 新建的请求不在索引里（ReadRequest 报错），此时保持调用方给的路径。
	if cur, err := c.ReadRequest(r.UID); err == nil && cur.Path != "" && cur.Path != r.Path {
		r.Path = cur.Path
	}
	return c.SaveRequest(r)
}

// ResponseDocInput 更新请求响应文档的入参（响应字段 UI → 请求模型）。
type ResponseDocInput struct {
	UID    string                     `json:"uid"`
	Type   string                     `json:"type"`
	Params []collection.ResponseParam `json:"params"`
	Sample string                     `json:"sample"`
	// ContentType 响应内容类型（json/text/xml/...）：编辑字段/数据时必须一并带回来，
	// 否则接口重建 ResponseDoc 时会丢掉它（同步键 retContentType 也会跟着丢）。
	ContentType string `json:"contentType,omitempty"`
	// Status/Headers：更新字段时的响应快照（状态码/响应头），随 respdoc 一并落盘（item 8）。
	Status  int             `json:"status,omitempty"`
	Headers []collection.KV `json:"headers,omitempty"`
}

// UpdateResponseDoc 把响应字段表与响应样例写入请求的 RespDoc 并落盘（文件即真相源，随后可同步）。
//
// 以磁盘上的最新请求为底打补丁，避免响应面板拿着编辑中的旧请求整体覆盖编辑器尚未保存的改动。
// gRPC 请求不承载响应文档（toFile / SyncExtra 均不带 respdoc 段）。
func (a *App) UpdateResponseDoc(in ResponseDocInput) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	if strings.TrimSpace(in.UID) == "" {
		return errors.New("缺少请求 uid")
	}
	r, err := c.ReadRequest(in.UID)
	if err != nil {
		return err
	}
	if r.IsGRPC() {
		return errors.New("gRPC 请求暂不支持响应文档")
	}
	// 只有 type/params/sample/contentType/快照全空才算「清空响应文档」；
	// 仅选了响应类型（如 xml）而还没填字段/数据时也必须保留，否则重读时会被当空段丢弃、
	// 界面回退成默认 json（响应类型显示错乱的根因）。
	if strings.TrimSpace(in.Type) == "" && len(in.Params) == 0 && strings.TrimSpace(in.Sample) == "" &&
		strings.TrimSpace(in.ContentType) == "" && in.Status == 0 && len(in.Headers) == 0 {
		r.RespDoc = nil
	} else {
		r.RespDoc = &collection.ResponseDoc{Type: in.Type, Params: in.Params, Sample: in.Sample, ContentType: in.ContentType, Status: in.Status, Headers: in.Headers}
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
	} else if ctx := a.appCtx(); ctx != nil {
		picked, err := runtime.SaveFileDialog(ctx, runtime.SaveDialogOptions{
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
