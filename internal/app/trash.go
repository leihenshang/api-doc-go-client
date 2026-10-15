package app

import (
	"strings"

	"api-doc-go-client/internal/collection"
)

// ---------- 回收站（item 20 / 21）----------
//
// 删除走「进 .trash」而非物理删除；列表/预览/彻底删除/恢复都按 root 定位（空 = 活动根）。

// trashColl 按 root 取集合（空 = 活动根）。
func (a *App) trashColl(root string) (*collection.Collection, error) {
	if strings.TrimSpace(root) == "" {
		return a.requireCollection()
	}
	return a.collOf(root)
}

// ListTrashFor 列出指定工作目录（空 = 活动根）的回收站条目，新→旧。
func (a *App) ListTrashFor(root string) ([]collection.TrashEntry, error) {
	c, err := a.trashColl(root)
	if err != nil {
		return nil, err
	}
	return c.ListTrash()
}

// PreviewTrashFor 预览回收站条目（item 21）：文件给内容（截断），目录给文件清单。
func (a *App) PreviewTrashFor(root, file string) (*collection.TrashPreview, error) {
	c, err := a.trashColl(root)
	if err != nil {
		return nil, err
	}
	return c.PreviewTrash(file)
}

// DeleteFromTrash 彻底删除一条回收站记录（item 21；不可恢复，前端必须先二次确认）。
func (a *App) DeleteFromTrash(file, root string) error {
	c, err := a.trashColl(root)
	if err != nil {
		return err
	}
	return c.RemoveTrash(file)
}

// RestoreTrash 恢复一条回收站记录：targetRoot 空 = 恢复到原工作区原路径；
// 否则复制到指定（已打开）工作区的同一相对路径（目标重名拒绝），成功后从源回收站移除。
// 返回恢复后的相对路径（相对目标集合）。
func (a *App) RestoreTrash(file, root, targetRoot string) (string, error) {
	src, err := a.trashColl(root)
	if err != nil {
		return "", err
	}
	srcRoot := strings.TrimSpace(root)
	if srcRoot == "" {
		srcRoot = a.activeRootID()
	}
	if t := strings.TrimSpace(targetRoot); t == "" || t == srcRoot {
		return src.RestoreTrash(file, "")
	}
	dst, err := a.collOf(targetRoot)
	if err != nil {
		return "", err
	}
	e, err := src.TrashInfo(file)
	if err != nil {
		return "", err
	}
	got, err := dst.ImportFromAbs(src.TrashAbs(file), collection.TrashRestoreRel(e, ""))
	if err != nil {
		return "", err
	}
	if err := src.RemoveTrash(file); err != nil {
		return got, err
	}
	return got, nil
}
