package app

// 模块（分组）与集合级维护（MCP 容器工具）：
//   - rename_module / delete_module → app_doc.go 的 RenameFolder / DeleteFolder(Tree)（删除进 .trash）
//   - rename_project               → RenameCollection（只改显示名，目录不变）
//   - delete_module(pruneEmpty)    → PruneEmptyFolders（只删空模块目录）
//   - CloseProject                 → MCP delete_project 前释放 watcher/索引
//
// RenameFolder / DeleteFolder / DeleteFolderTree 已随前端分组菜单存在（app_doc.go），
// 这里只补 MCP 需要的「按路径找 uid」「集合级改名/清理/关闭」三组能力。

// FolderUIDByPath 相对路径（api/user）→ 分组 uid；根或不存在返回 false。
func (a *App) FolderUIDByPath(dir string) (string, bool) {
	c, err := a.requireCollection()
	if err != nil {
		return "", false
	}
	return c.FolderUIDByPath(dir)
}

// RenameCollection 改集合显示名（opencollection.yml 的 info.name）。
func (a *App) RenameCollection(name string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.RenameCollection(name)
}

// PruneEmptyFolders 清理空模块目录，返回删除数量。
func (a *App) PruneEmptyFolders() (int, error) {
	c, err := a.requireCollection()
	if err != nil {
		return 0, err
	}
	return c.PruneEmptyFolders()
}

// CloseProject 释放该实例打开的所有集合（MCP 删除项目目录前调用）。
func (a *App) CloseProject() {
	a.closeAllCollections()
}
