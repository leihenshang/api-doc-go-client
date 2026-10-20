//go:build !(dev || devbuild)

package config

// 正式版命名空间（与开发版隔离，便于 prod / dev 并存调试）。
const (
	// appDir 用户配置目录名（<UserConfigDir>/api-doc-client）。
	appDir = "api-doc-client"
	// defaultMCPPort MCP 服务默认端口；开发版用不同端口避免并存时抢占。
	defaultMCPPort = 8189
)
