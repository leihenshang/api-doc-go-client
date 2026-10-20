//go:build dev || devbuild

package keychain

// service 开发版密钥环命名空间：与正式版错开，并存调试时条目互不串味。
const service = "api-doc-go-client-dev"
