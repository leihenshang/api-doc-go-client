package collection

import share "api-doc-go-share/collection"

// 集合文件的格式定义、命名规则与排序规则来自共享包 api-doc-go-share/collection：
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
)

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
	Docs     string           `json:"docs"`
	BaseRev  int64            `json:"baseRev"` // 预留：同步基线（条目版本号）
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
