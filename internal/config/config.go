// Package config 客户端全局设置：落 <用户配置目录>/api-doc-client/config.json。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	appDir       = "api-doc-client"
	fileName     = "config.json"
	defaultTTLS  = 30
	defaultMaxRD = 5
	defaultLimit = 200
)

// Settings 全局设置：请求级 settings 未覆盖时生效。
type Settings struct {
	InsecureSSL     bool `json:"insecureSsl"` // 跳过 TLS 证书校验（自签/内网证书）
	TimeoutSec      int  `json:"timeoutSec"`
	FollowRedirects bool `json:"followRedirects"`
	MaxRedirects    int  `json:"maxRedirects"`
	PersistCookies  bool `json:"persistCookies"`
	HistoryLimit    int  `json:"historyLimit"`
}

// Default 默认设置：跟随重定向、30s 超时、持久化 Cookie、保留 200 条历史。
func Default() Settings {
	return Settings{
		TimeoutSec:      defaultTTLS,
		FollowRedirects: true,
		MaxRedirects:    defaultMaxRD,
		PersistCookies:  true,
		HistoryLimit:    defaultLimit,
	}
}

// Normalize 把缺失/非法值补成默认值，保证下游拿到的都可用。
func (s Settings) Normalize() Settings {
	def := Default()
	if s.TimeoutSec <= 0 {
		s.TimeoutSec = def.TimeoutSec
	}
	if s.MaxRedirects <= 0 {
		s.MaxRedirects = def.MaxRedirects
	}
	if s.HistoryLimit <= 0 {
		s.HistoryLimit = def.HistoryLimit
	}
	return s
}

// Dir 客户端配置目录（config.json / cookies.json / history.jsonl 都放这里）。
func Dir() (string, error) {
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
