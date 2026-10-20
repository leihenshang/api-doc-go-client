//go:build !(dev || devbuild)

package keychain

// service 正式版密钥环命名空间（与开发版隔离）。
const service = "api-doc-go-client"
