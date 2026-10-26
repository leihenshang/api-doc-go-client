//go:build !(dev || devbuild)

package tray

// tooltip / 菜单文案（正式版）。dev 版见 tray_text_dev.go。
// 不接 i18n：托盘菜单由后端渲染，无法用前端语言包；中文为主语言。
const (
	tooltip     = "api-doc-go"
	menuShow    = "显示主窗口"
	menuShowTip = "显示主窗口"
	menuQuit    = "退出"
	menuQuitTip = "退出程序"
)
