//go:build !(windows && dev)

// 非「Windows + 开发版」组合的空实现：正式版与非 Windows 平台不换图标
// （exe 图标由打包期 syso 决定，Linux/macOS 的 dev 区分靠标题气泡）。
package devicon

// Apply 空实现。
func Apply(string) {}
