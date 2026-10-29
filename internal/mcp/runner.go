package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"strconv"
	"strings"
	"time"

	"api-doc-go-client/internal/app"
	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/index"
	"api-doc-go-client/internal/runner"
)

// defaultNewApp 默认工厂：每个项目一个独立的 App 实例。
func defaultNewApp() ProjectApp { return app.NewApp() }

// NewToken 生成 32 位十六进制随机令牌（128 bit 熵）。
// 独立 mcpserver 的自动令牌与客户端设置页的「重新生成」共用它。
func NewToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16) // 读不到随机源时兜底
	}
	return hex.EncodeToString(b)
}

// IsLoopbackAddr 监听地址是否只在回环上（127.0.0.1 / localhost / ::1）。
// 非回环 = 同网段与 WSL 侧可达，必须配令牌。独立 mcpserver 与客户端内嵌服务共用此判定。
func IsLoopbackAddr(addr string) bool {
	switch strings.ToLower(strings.TrimSpace(addr)) {
	case "", "0.0.0.0", "::", "[::]":
		return false
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	if ip := net.ParseIP(strings.TrimSpace(addr)); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// ProjectApp 是 MCP 服务用到的集合运行时能力。
//
// 抽象成接口的原因：internal/mcp 原本直接依赖 *app.App，于是 internal/app 无法反过来
// 依赖 internal/mcp（客户端内嵌 MCP 服务会成环）。改成接口 + 工厂注入后，两处都注入
// app.NewApp：App 一次只能打开一个集合，所以多项目必须各自一个实例 —— 与独立 mcpserver 同构。
type ProjectApp interface {
	OpenCollection(dir string) (*collection.CollectionInfo, error)
	ReloadCollection() (*collection.CollectionInfo, error)
	ReadRequest(uid string) (*collection.Request, error)
	SaveRequest(r *collection.Request) error
	DeleteRequest(uid string) error
	MoveRequest(uid, folder string) error
	CreateFolder(parent, name string) error
	CreateRequest(folder, name, method string) (*collection.Request, error)
	CreateRequestFromDraft(folder, name string, r *collection.Request) (*collection.Request, error)
	ParseCurl(curl string) (*collection.Request, error)
	SearchIndex(query string, limit int) ([]index.Node, error)
	SendRequest(r *collection.Request, envName string) (*runner.Result, error)
	ListEnvs() ([]collection.Env, error)
	SaveEnv(env *collection.Env) error
	// SaveEnvChecked 带并发写保护地保存环境（expectHash 为空则不校验）。
	SaveEnvChecked(env *collection.Env, expectHash string) error
	// EnvFileHash 环境主文件内容哈希（不存在返回空串），供调用方做写前校验。
	EnvFileHash(name string) (string, error)
	// RenameEnv 环境改名（两个文件一起搬，旧文件进 .trash）；expectHash 非空时校验旧文件。
	RenameEnv(oldName, newName, expectHash string) error
	DeleteEnv(name string) error
	ListResponseExamples(reqUID string) ([]*collection.ResponseExample, error)
	SaveResponseExample(r *collection.Request, name string, res *runner.Result) (*collection.ResponseExample, error)
	// SaveResponseExampleRaw 用原始字段保存一条示例（不发送请求）；contentType 为空时按 body 推断。
	SaveResponseExampleRaw(reqUID, name string, status int, headers []collection.KV, body, contentType string) (*collection.ResponseExample, error)
	// UpdateResponseExample 局部更新示例（指针为 nil 的字段不动）；改名会同步改文件名与 path。
	UpdateResponseExample(reqUID, exampleUID string, name *string, status *int,
		headers *[]collection.KV, body, contentType *string) (*collection.ResponseExample, error)
	// DeleteResponseExample 删除示例（文件移入 .trash）。
	DeleteResponseExample(reqUID, exampleUID string) error
	// SaveDocImage 保存一张 Markdown 图片（base64，可带 data URL 前缀），
	// 返回到可直接写进正文的 URL（/doc-assets/<sha256>）。
	SaveDocImage(dataBase64 string) (string, error)
	// ---- 模块 / 集合级维护（容器工具）----
	FolderUIDByPath(dir string) (string, bool)
	RenameFolder(uid, name string) error
	DeleteFolder(uid string) error
	DeleteFolderTree(uid string) error
	RenameCollection(name string) error
	PruneEmptyFolders() (int, error)
	// CloseProject 释放该实例的集合句柄（delete_project 前调用）。
	CloseProject()
	ListDocs() ([]*collection.DocEntry, error)
	// ReadDoc 读单篇文档（含正文）；uid 不存在时返回错误。
	ReadDoc(uid string) (*collection.DocEntry, error)
	CreateDoc(name, folder string) (*collection.DocEntry, error)
	// CreateDocEx 新建文档：uid 可选（幂等），onConflict = error|suffix|overwrite。
	CreateDocEx(name, uid, folder, onConflict string) (*collection.DocEntry, error)
	// PlanCreateDoc 只做校验/冲突判定，不落盘（create_doc dryRun）。
	PlanCreateDoc(name, uid, folder, onConflict string) (*collection.DocEntry, string, error)
	SaveDoc(d *collection.DocEntry) error
	// SaveDocExpectHash 带并发写保护地保存文档（expectHash 为空则不校验）。
	SaveDocExpectHash(d *collection.DocEntry, expectHash string) error
	// DocFileHash 文档文件的当前磁盘哈希（不存在返回空串），供写前校验与 get_doc 回传。
	DocFileHash(uid string) string
	// DocHashes 一次扫描取全部文档 hash（列表批量用，避免逐篇重扫）。
	DocHashes() (map[string]string, error)
	// MoveDoc 把文档移动到目标分组目录（destFolder 为空 = 集合根）。
	MoveDoc(uid, destFolder string) error
	// RenameDoc 只改 frontmatter 里的显示名（文件名不变）。
	RenameDoc(uid, name string) error
	DeleteDoc(uid string) error
}

// NewAppFunc 构造项目级运行时的工厂。
type NewAppFunc func() ProjectApp
