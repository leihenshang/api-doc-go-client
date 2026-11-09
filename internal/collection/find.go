package collection

import (
	"os"
	"path/filepath"
	"strings"

	share "github.com/leihenshang/api-doc-go-share/collection"
)

// FindByUID 在 parent 的**一级子目录**里找 uid 匹配的集合目录，返回其绝对路径；找不到返回空串。
//
// 用途：集合目录被改名 / 在同一父目录内移动后，启动恢复手里只有旧路径，靠
// opencollection.yml 里的 meta.uid 把同一个集合找回来。
//
// 为什么只扫一层：改名与整理通常发生在同级；无限递归会在用户把集合放在家目录这类
// 大目录下时变成一次全盘遍历。移到别的父目录时找不到，退回「提示用户手动打开」。
func FindByUID(parent, uid string) (string, error) {
	uid = strings.TrimSpace(uid)
	parent = strings.TrimSpace(parent)
	if uid == "" || parent == "" {
		return "", nil
	}
	absParent, err := filepath.Abs(parent)
	if err != nil {
		return "", nil
	}
	ents, err := os.ReadDir(absParent)
	if err != nil {
		// 父目录读不了 = 找不到。这不是调用方的错误，别把「改名」变成一次报错。
		return "", nil
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(absParent, e.Name())
		data, err := os.ReadFile(filepath.Join(dir, "opencollection.yml"))
		if err != nil {
			continue
		}
		m, err := share.DecodeManifest(data)
		if err != nil {
			continue
		}
		if m.Meta.UID == uid {
			return dir, nil
		}
	}
	return "", nil
}
