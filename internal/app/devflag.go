package app

// IsDev 返回是否开发版构建（wails dev 注入 dev 标签）：
// 前端标题栏据此渲染绿色 dev 气泡，并把 document.title 换成 -dev 版本。
func (a *App) IsDev() bool { return DevBuild }
