package mcp

// 容器级工具（外部评审 T4）：模块（分组）与项目的改名/删除，以及空模块清理。
// 删除语义与客户端一致：模块 → 集合内 .trash；项目 → 授权根 .trash（可人工移回）。
// 所有路径都先按 FolderUIDByPath 归一（拒绝 ../），不存在给 [NOT_FOUND]。

import (
	"errors"
	"fmt"
	"strings"

	"api-doc-go-client/internal/index"
)

// RenameModule 改模块显示名（只改 folder.yml 的 info.name，目录/uid 不变）。
func (s *Service) RenameModule(project, path, name string) error {
	a, _, err := s.resolveWrite(project)
	if err != nil {
		return err
	}
	path = strings.TrimSpace(path)
	name = strings.TrimSpace(name)
	if path == "" {
		return withInvalid(errors.New("缺少 path：模块相对路径，如 api/user"))
	}
	if name == "" {
		return withInvalid(errors.New("缺少 name：新的模块名"))
	}
	uid, ok := a.FolderUIDByPath(path)
	if !ok {
		return withNotFound(fmt.Errorf("模块 %q 不存在：%s", path, moduleCandidates(a, path)))
	}
	if err := a.RenameFolder(uid, name); err != nil {
		return withInvalid(err)
	}
	return nil
}

// DeleteModule 删除模块：recursive=true 整棵进 .trash；false 仅当空模块（非空会拒绝）。
func (s *Service) DeleteModule(project, path string, recursive, dryRun bool) error {
	a, _, err := s.resolveWrite(project)
	if err != nil {
		return err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return withInvalid(errors.New("缺少 path：模块相对路径，如 api/user"))
	}
	uid, ok := a.FolderUIDByPath(path)
	if !ok {
		return withNotFound(fmt.Errorf("模块 %q 不存在：%s", path, moduleCandidates(a, path)))
	}
	if dryRun {
		return nil
	}
	if recursive {
		err = a.DeleteFolderTree(uid)
	} else {
		err = a.DeleteFolder(uid)
	}
	if err != nil {
		// 非空拒绝属于业务约束（不是内部错误）：INVALID + 原文里提示 recursive
		return withInvalid(fmt.Errorf("%w（模块内还有内容时请传 recursive=true）", err))
	}
	return nil
}

// moduleCandidates 模块候选（前 8 个路径）：NOT_FOUND 时直接给清单，省掉一次 get_project_modules。
// 按与 query（调用方给的 path）的匹配强度排序：前缀/目录命中优先，其次包含。
func moduleCandidates(a ProjectApp, query string) string {
	const maxShown = 8
	// 候选要准：刚 create_module 出来的模块可能还没进索引。
	_, _ = a.ReloadCollection()
	nodes, err := a.SearchIndex("", 0)
	if err != nil {
		return "用 get_project_modules 查看路径"
	}
	folders := make([]index.Node, 0, len(nodes))
	for _, n := range nodes {
		if n.Type == "folder" {
			folders = append(folders, n)
		}
	}
	if len(folders) == 0 {
		return "项目里还没有模块（可用 create_module 新建）"
	}
	rankNodes(folders, query)
	parts := make([]string, 0, maxShown)
	for i, n := range folders {
		if i >= maxShown {
			break
		}
		parts = append(parts, n.Path)
	}
	out := "候选：" + strings.Join(parts, "、")
	if len(folders) > maxShown {
		out += fmt.Sprintf(" 等 %d 个", len(folders))
	}
	return out
}

// RenameProject 改项目显示名（opencollection.yml 的 info.name；目录不变）。
func (s *Service) RenameProject(project, name string) error {
	a, _, err := s.resolveWrite(project)
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return withInvalid(errors.New("缺少 name：新的项目名"))
	}
	if err := a.RenameCollection(name); err != nil {
		return withInvalid(err)
	}
	// 扫描结果是缓存（list_projects 直接读它）：改完清单要刷新，否则名字还是旧的
	_ = s.reg.Reload()
	return nil
}

// DeleteProject 删除项目：confirm 必须等于项目名/目录名；目录移入授权根 .trash 后可人工恢复。
func (s *Service) DeleteProject(project, confirm string, dryRun bool) (string, error) {
	key, err := s.resolveKey(project)
	if err != nil {
		return "", withInvalid(err)
	}
	dest, err := s.reg.DeleteProject(key, confirm, dryRun)
	if err != nil {
		switch {
		case errors.Is(err, ErrProjectConfirmMismatch):
			return "", coded(CodeConflict, err)
		case strings.Contains(err.Error(), "只读授权"):
			return "", coded(CodeReadOnly, err)
		case strings.Contains(err.Error(), "找不到项目"):
			return "", coded(CodeNotFound, err)
		case strings.Contains(err.Error(), "confirm"):
			return "", withInvalid(err)
		default:
			return "", coded(CodeIO, err)
		}
	}
	return dest, nil
}

// PruneEmptyModules 清理空模块目录（只有 folder.yml、没有请求/文档/子模块），返回删除数。
func (s *Service) PruneEmptyModules(project string) (int, error) {
	a, _, err := s.resolveWrite(project)
	if err != nil {
		return 0, err
	}
	n, err := a.PruneEmptyFolders()
	if err != nil {
		return n, coded(CodeIO, err)
	}
	return n, nil
}
