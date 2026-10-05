// Package config 客户端全局设置：落 <用户配置目录>/api-doc-client/config.json。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

const (
	appDir       = "api-doc-client"
	fileName     = "config.json"
	defaultTTLS  = 30
	defaultMaxRD = 5
	defaultLimit = 200

	// 数值上限：只有下限兜底是不够的 —— 超大的 timeoutSec 在换算成 time.Duration 时
	// 会 int64 溢出（变成负值，请求立刻超时），超大的 historyLimit 会让历史文件无界增长。
	maxTimeoutSec   = 600
	maxRedirects    = 20
	maxHistoryLimit = 5000

	// defaultRespSize 响应区默认占比（%）。
	defaultRespSize = 44

	// MCP 服务默认值：默认不启用（用户显式开），只监听本机。
	defaultMCPAddr = "127.0.0.1"
	defaultMCPPort = 8189
	minMCPPort     = 1024
	maxMCPPort     = 65535
)

// Settings 全局设置：请求级 settings 未覆盖时生效。
type Settings struct {
	InsecureSSL     bool    `json:"insecureSsl"` // 跳过 TLS 证书校验（自签/内网证书）
	TimeoutSec      int     `json:"timeoutSec"`
	FollowRedirects bool    `json:"followRedirects"`
	MaxRedirects    int     `json:"maxRedirects"`
	PersistCookies  bool    `json:"persistCookies"`
	HistoryLimit    int     `json:"historyLimit"`
	UIScale         float64 `json:"uiScale"`        // 界面缩放倍率（1 = 100%）
	ResponseLayout  string  `json:"responseLayout"` // 响应区位置：right | bottom
	ResponseSize    int     `json:"responseSize"`   // 响应区占比（%）：right 时为宽度、bottom 时为高度，可拖动调整
	Theme           string  `json:"theme"`          // 主题：light | dark（前端切换后经 SaveSettings 落盘）
	ProxyURL        string  `json:"proxyUrl"`       // HTTP(S) 代理，如 http://127.0.0.1:7890；空 = 直连
	AutoSave        bool    `json:"autoSave"`       // 编辑后自动写盘：默认关（手动保存模式）；关时仅靠 Ctrl+S / 关闭页签 / 保存所有 落盘
	// MCP 客户端内嵌的 MCP 服务设置（供外部 AI 工具读写集合；见《MCP服务使用手册.md》）。
	// 单独一个分组而不是塞进网络/本地 —— 它的生命周期与其它设置不同（要起停一个监听服务）。
	MCP MCPConfig `json:"mcp"`
}

// MCPConfig 内嵌 MCP HTTP 服务的配置（客户端内起一个 streamable HTTP 服务）。
type MCPConfig struct {
	Enabled  bool   `json:"enabled"`  // 是否启用；关闭时不监听任何端口
	Addr     string `json:"addr"`     // 监听地址：127.0.0.1（本机）| 0.0.0.0（跨主机/WSL/局域网）
	Port     int    `json:"port"`     // 监听端口（1024–65535）
	Token    string `json:"token"`    // Bearer 令牌；启用且为空时自动生成并落盘
	ReadOnly bool   `json:"readOnly"` // 只读模式：不注册创建/修改/删除类工具（默认开，联网暴露时更安全）
	// AllowOrigins 允许的浏览器来源（Origin 白名单，逗号分隔在界面里填）。
	// 带 Origin 的请求必须命中白名单才放行 —— MCP 规范的 DNS rebinding 防护；
	// 非浏览器客户端（Claude Desktop / cursor / 命令行）不带 Origin，不受影响。
	AllowOrigins []string `json:"allowOrigins"`
}

const (
	// LayoutRight / LayoutBottom 响应区布局取值。
	LayoutRight  = "right"
	LayoutBottom = "bottom"

	// ThemeLight / ThemeDark 主题取值。
	ThemeLight = "light"
	ThemeDark  = "dark"

	minUIScale = 0.75
	maxUIScale = 3.0

	// 响应区占比上下限：留出请求区可视空间，避免拖到 0 后无法拖回。
	MinRespSize = 20
	MaxRespSize = 80
)

// Default 默认设置：跟随重定向、30s 超时、持久化 Cookie、保留 200 条历史、响应区在右侧。
func Default() Settings {
	return Settings{
		TimeoutSec:      defaultTTLS,
		FollowRedirects: true,
		MaxRedirects:    defaultMaxRD,
		PersistCookies:  true,
		HistoryLimit:    defaultLimit,
		UIScale:         1,
		ResponseLayout:  LayoutRight,
		ResponseSize:    defaultRespSize,
		Theme:           ThemeLight,
		MCP: MCPConfig{
			Enabled:  false,
			Addr:     defaultMCPAddr,
			Port:     defaultMCPPort,
			ReadOnly: true, // 联网暴露时默认只读：AI 不能删请求/改集合
		},
	}
}

// IsDark 是否暗色主题（窗口底色等原生侧需要）。
func (s Settings) IsDark() bool {
	return s.Theme == ThemeDark
}

// Normalize 把缺失/非法值补成默认值，保证下游拿到的都可用。
func (s Settings) Normalize() Settings {
	def := Default()
	if s.TimeoutSec <= 0 {
		s.TimeoutSec = def.TimeoutSec
	}
	if s.TimeoutSec > maxTimeoutSec {
		s.TimeoutSec = maxTimeoutSec
	}
	if s.MaxRedirects <= 0 {
		s.MaxRedirects = def.MaxRedirects
	}
	if s.MaxRedirects > maxRedirects {
		s.MaxRedirects = maxRedirects
	}
	if s.HistoryLimit <= 0 {
		s.HistoryLimit = def.HistoryLimit
	}
	if s.HistoryLimit > maxHistoryLimit {
		s.HistoryLimit = maxHistoryLimit
	}
	if s.UIScale < minUIScale || s.UIScale > maxUIScale {
		s.UIScale = def.UIScale
	}
	if s.ResponseLayout != LayoutBottom {
		s.ResponseLayout = LayoutRight
	}
	if s.ResponseSize < MinRespSize || s.ResponseSize > MaxRespSize {
		s.ResponseSize = def.ResponseSize
	}
	// 未知主题一律回落浅色（旧版本配置文件里没有该字段也走这里）
	if s.Theme != ThemeDark {
		s.Theme = ThemeLight
	}
	s.MCP = s.MCP.normalize(def.MCP)
	return s
}

// normalize 归一化 MCP 分组：地址兜底、端口夹到合法区间、来源去空去重。
func (m MCPConfig) normalize(def MCPConfig) MCPConfig {
	if strings.TrimSpace(m.Addr) == "" {
		m.Addr = def.Addr
	}
	// 只保留可用的监听地址：空/非法一律回落默认（0.0.0.0 与 :: 都算合法）
	if a := strings.TrimSpace(m.Addr); a != "0.0.0.0" && a != "::" && net.ParseIP(a) == nil {
		m.Addr = def.Addr
	}
	if m.Port < minMCPPort || m.Port > maxMCPPort {
		m.Port = def.Port
	}
	var origins []string
	seen := make(map[string]bool, len(m.AllowOrigins))
	for _, o := range m.AllowOrigins {
		o = strings.TrimSpace(o)
		if o == "" || seen[o] {
			continue
		}
		seen[o] = true
		origins = append(origins, o)
	}
	m.AllowOrigins = origins // 空列表归一为 nil：否则 save/load 往返后 [] ≠ nil（DeepEqual 敏感）
	return m
}

// EnvDirOverride 配置目录覆盖（测试 / devserver 用）：设为绝对路径时 Dir() 直接返回它。
//
// 为什么需要：os.UserConfigDir() 在 Windows 只认 %AppData%、在类 Unix 只认 XDG_CONFIG_HOME，
// 测试靠 Setenv 隔离时极易漏一边（Windows 上就会写进用户真实配置目录），
// 而这个变量在两端都生效，是唯一可靠的隔离开关。
const EnvDirOverride = "API_DOC_CONFIG_DIR"

// Dir 客户端配置目录（config.json / cookies.json / history.jsonl 都放这里）。
func Dir() (string, error) {
	if override := strings.TrimSpace(os.Getenv(EnvDirOverride)); override != "" {
		return filepath.Clean(override), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("定位用户配置目录: %w", err)
	}
	return filepath.Join(base, appDir), nil
}

// Path 配置文件完整路径。
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// Load 读取全局设置；文件不存在时返回默认值。
func Load() (Settings, error) {
	path, err := Path()
	if err != nil {
		return Default(), err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Default(), fmt.Errorf("读取全局设置: %w", err)
	}
	// 从默认值解码：缺字段时保留默认，而非被零值覆盖
	out := Default()
	if err := json.Unmarshal(data, &out); err != nil {
		return Default(), fmt.Errorf("解析全局设置: %w", err)
	}
	return out.Normalize(), nil
}

// Save 写入全局设置。
func Save(s Settings) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建配置目录: %w", err)
	}
	data, err := json.MarshalIndent(s.Normalize(), "", "  ")
	if err != nil {
		return fmt.Errorf("序列化全局设置: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("写入全局设置: %w", err)
	}
	return nil
}
