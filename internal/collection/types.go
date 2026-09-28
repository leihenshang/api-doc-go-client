package collection

import "gopkg.in/yaml.v3"

// KV 请求参数 / 请求头 / 表单的一行。
// Description 为客户端参数表「说明」列，空值不落盘（omitempty），不影响既有文件与 Bruno 互操作。
type KV struct {
	Name        string `yaml:"name" json:"name"`
	Value       string `yaml:"value" json:"value"`
	Enabled     bool   `yaml:"enabled" json:"enabled"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// Body 请求体：none / json / text / form（urlencoded）/ multipart。
type Body struct {
	Type string `yaml:"type" json:"type"`
	Raw  string `yaml:"raw,omitempty" json:"raw"` // json / text 的原始内容
	Form []KV   `yaml:"form,omitempty" json:"form"`
}

// Auth 认证配置：none | basic | bearer | apikey；值支持 {{变量}}。
type Auth struct {
	Type     string `yaml:"type,omitempty" json:"type,omitempty"`
	Username string `yaml:"username,omitempty" json:"username,omitempty"`
	Password string `yaml:"password,omitempty" json:"password,omitempty"`
	Token    string `yaml:"token,omitempty" json:"token,omitempty"`
	Key      string `yaml:"key,omitempty" json:"key,omitempty"`
	Value    string `yaml:"value,omitempty" json:"value,omitempty"`
	In       string `yaml:"in,omitempty" json:"in,omitempty"` // apikey 位置：header | query
}

// UnmarshalYAML 兼容 Bruno 的标量写法（如 auth: inherit）。
func (a *Auth) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		a.Type = node.Value
		return nil
	}
	type plain Auth
	var tmp plain
	if err := node.Decode(&tmp); err != nil {
		return err
	}
	*a = Auth(tmp)
	return nil
}

// MarshalYAML 只有类型时回写标量，保持 Bruno 原样（如 auth: inherit）。
func (a Auth) MarshalYAML() (any, error) {
	if a.Username == "" && a.Password == "" && a.Token == "" && a.Key == "" && a.Value == "" && a.In == "" {
		return a.Type, nil
	}
	type plain Auth
	return plain(a), nil
}

// RequestSettings 请求级覆盖项：未设置（零值）时沿用全局设置。
type RequestSettings struct {
	TimeoutSec      int   `yaml:"timeout,omitempty" json:"timeoutSec,omitempty"`
	FollowRedirects *bool `yaml:"followRedirects,omitempty" json:"followRedirects,omitempty"`
	MaxRedirects    int   `yaml:"maxRedirects,omitempty" json:"maxRedirects,omitempty"`
	InsecureSSL     *bool `yaml:"insecureSsl,omitempty" json:"insecureSsl,omitempty"`
	EncodeURL       bool  `yaml:"encodeUrl,omitempty" json:"encodeUrl,omitempty"`
}

// Request 一条接口（对应集合里的一个 .yml 文件）。
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

// Var 环境变量。
type Var struct {
	Name    string `yaml:"name" json:"name"`
	Value   string `yaml:"value" json:"value"`
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Secret  bool   `yaml:"secret" json:"secret"`
}

// Env 环境：文件名（不含扩展名）即环境名。
type Env struct {
	Name string `json:"name"`
	Vars []Var  `json:"vars"`
}

// Node 集合树的节点。
type Node struct {
	Type     string  `json:"type"` // folder | request
	UID      string  `json:"uid"`
	Name     string  `json:"name"`
	Path     string  `json:"path"`
	Method   string  `json:"method,omitempty"`
	Children []*Node `json:"children,omitempty"`
}

// CollectionInfo 打开的集合概要（含树与环境）。
type CollectionInfo struct {
	Dir  string  `json:"dir"`
	Name string  `json:"name"`
	UID  string  `json:"uid"`
	Tree []*Node `json:"tree"`
	Envs []Env   `json:"envs"`
}
