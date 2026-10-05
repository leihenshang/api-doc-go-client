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
	ListDocs() ([]*collection.DocEntry, error)
	CreateDoc(name string) (*collection.DocEntry, error)
	SaveDoc(d *collection.DocEntry) error
}

// NewAppFunc 构造项目级运行时的工厂。
type NewAppFunc func() ProjectApp
