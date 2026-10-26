//go:build dev || devbuild

package tray

// tooltip / 菜单文案（开发版，标题带 -dev 便于与正式版区分）。
const (
	tooltip     = "api-doc-go-dev"
	menuShow    = "显示主窗口（Dev）"
	menuShowTip = "显示主窗口"
	menuQuit    = "退出（Dev）"
	menuQuitTip = "退出程序"
)
