package collection

import share "github.com/leihenshang/api-doc-go-share/collection"

// 集合文件的格式定义、命名规则与排序规则来自共享包 github.com/leihenshang/api-doc-go-share/collection：
// 这些是「两端必须一致」的定义与算法，直接引用；本包只保留客户端自己的内存模型与 IO。
type (
	KV              = share.KV
	Body            = share.Body
	Auth            = share.Auth
	RequestSettings = share.RequestSettings
	Var             = share.Var
	Node            = share.Node

	requestFile = share.RequestFile // 请求文件（磁盘）
	folderFile  = share.FolderFile  // 分组描述文件（磁盘）
	envFile     = share.EnvFile
	secretsFile = share.SecretsFile
	manifest    = share.Manifest

	GrpcBlock = share.GRPCBlock // grpc 段（磁盘）
	GrpcTLS   = share.GRPCTLS   // grpc 段的连接安全设置
	// GrpcDefault 集合级默认 gRPC 定义（清单里的 grpc 段，P8）
	GrpcDefault = share.GRPCDefault
)

// 请求类型（磁盘 info.type）。HTTP 用 http 段，gRPC 用 grpc 段。
const (
	TypeHTTP = "http"
	TypeGRPC = "grpc"
)

// MethodGRPC gRPC 请求在 Method 字段上的取值。
// 索引、历史、搜索、同步都按「Method + URL」工作，gRPC 用协议名 + grpc:// 地址保持既有能力可用。
const MethodGRPC = "GRPC"

// GrpcURL 合成 gRPC 请求的地址（grpc://host:port/包.服务/方法）。
// 与 runner 侧的调用路径同形，便于在索引/历史/搜索里一眼看出目标。
func GrpcURL(target, service, method string) string {
	if target == "" {
		return ""
	}
	path := service
	if method != "" {
		path += "/" + method
	}
	return "grpc://" + target + "/" + path
}

// Request 一条接口的内存形态（带前端 JSON 契约；磁盘形态见共享包的 RequestFile）。
type Request struct {
	UID      string           `json:"uid"`
	Name     string           `json:"name"`
	Seq      int              `json:"seq"`
	Path     string           `json:"path"`
	Method   string           `json:"method"`
	URL      string           `json:"url"`
	Params   []KV             `json:"params"`
	Headers  []KV             `json:"headers"`
	Body     Body             `json:"body"`
	Auth     *Auth            `json:"auth,omitempty"`
	Settings *RequestSettings `json:"settings,omitempty"`
	// GRPC gRPC 请求段（非 nil 即 gRPC 请求）；HTTP 请求为 nil。
	// Method 固定为 MethodGRPC、URL 为派生值（grpc://目标/服务/方法），不落盘。
	GRPC    *GrpcBlock `json:"grpc,omitempty"`
	Docs    string     `json:"docs"`
	BaseRev int64      `json:"baseRev"` // 预留：同步基线（条目版本号）
	// ExpectHash 冲突检测：客户端带上「我读到这份文件时的内容哈希」（index 的 hash）。
	// 磁盘上已不一致（别的编辑器 / 另一个客户端窗口 / 同步写回）就拒写，避免静默覆盖。
	// 只在内存契约里传，不落盘（toFile 不含该字段）。
	ExpectHash string `json:"expectHash,omitempty"`
	// 脚本与断言（Bruno 超集；磁盘上落在顶层 vars/script/assert，经 Extra 往返）
	VarsPreRequest []ScriptVar    `json:"varsPreRequest,omitempty"`
	Script         *ScriptBlock   `json:"script,omitempty"`
	Asserts        []ScriptAssert `json:"asserts,omitempty"`
}

// IsGRPC 是否 gRPC 请求（内存里以 grpc 段是否存在为准）。
func (r Request) IsGRPC() bool { return r.GRPC != nil }

// ScriptVar vars.pre-request 的一行。
type ScriptVar struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Enabled bool   `json:"enabled"`
}

// ScriptBlock script 段（pre-request / post-response 源码）。
type ScriptBlock struct {
	PreRequest   string `json:"preRequest,omitempty"`
	PostResponse string `json:"postResponse,omitempty"`
}

// ScriptAssert assert 段的一条。
type ScriptAssert struct {
	Name string `json:"name,omitempty"`
	Expr string `json:"expr"`
}

// Env 环境：文件名（不含扩展名）即环境名。
type Env struct {
	Name string `json:"name"`
	Vars []Var  `json:"vars"`
}

// CollectionInfo 打开的集合概要（含树与环境）。
type CollectionInfo struct {
	Dir  string  `json:"dir"`
	Name string  `json:"name"`
	UID  string  `json:"uid"`
	Tree []*Node `json:"tree"`
	Envs []Env   `json:"envs"`
}
