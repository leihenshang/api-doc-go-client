//go:build dev || devbuild

package config

// 开发版命名空间（与正式版隔离，便于 prod / dev 并存调试）。
const (
	// appDir 用户配置目录名：独立于正式版，避免并发读写覆盖同一份 config.json。
	appDir = "api-doc-client-dev"
	// defaultMCPPort MCP 服务默认端口：与正式版 8189 错开，避免两版同时开 MCP 抢占。
	defaultMCPPort = 8190
)
