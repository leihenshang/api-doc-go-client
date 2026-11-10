package collection

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// CloneFolder 递归复制分组（含子分组 / 请求 / 文档），返回副本的显示名。
//
// 只有**顶层**分组改名加「副本N」；内部条目的名字保持不变 —— 用户要的是
// 「把这组东西复制一份」，不是把每个请求都改名叫「xx 副本1」。
//
// 所有条目（分组 / 请求 / 文档）都换新 uid：uid 是集合内的身份，复制出两份同样的
// uid 会让同步、收藏、选中态全部指向错的那一个。
//
// 保留目录（examples / assets / environments / .trash …）不复制：它们是运行产物
// 或本地状态，复制过去只会造出两套同样 uid 的示例与响应记录。非结构化附件
// （图片等）同理不进副本 —— 集合树只认 .yml / .md。
func (c *Collection) CloneFolder(uid string) (string, error) {
	res, err := c.scan()
	if err != nil {
		return "", err
	}
	srcDir, ok := c.dirByUID(res, uid)
	if !ok {
		return "", errNotFound
	}
	parent := parentDir(srcDir)
	base := res.nameByDir[srcDir]
	if base == "" {
		base = filepath.Base(srcDir)
	}
	name, err := c.nextCloneName(parent, base)
	if err != nil {
		return "", err
	}

	// 显示名不同不代表目录名不同（sanitize 后可能撞车）：目录另做一次唯一化。
	baseDir := sanitizeFileName(name)
	dstDir := joinRel(parent, baseDir)
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(c.Dir, filepath.FromSlash(dstDir))); os.IsNotExist(err) {
			break
		}
		dstDir = joinRel(parent, fmt.Sprintf("%s-%d", baseDir, i))
	}

	absDst := filepath.Join(c.Dir, filepath.FromSlash(dstDir))
	if err := os.MkdirAll(absDst, 0o755); err != nil {
		return "", err
	}
	// 顶层分组：新 uid + 副本名
	if err := c.ensureFolderYMLWithUID(dstDir, name, uuid.NewString()); err != nil {
		_ = os.RemoveAll(absDst)
		return "", err
	}
	if err := c.copySubtree(res, srcDir, dstDir); err != nil {
		// 半成品不留：否则树上会多出一个只有一半内容的分组，用户还得手工删。
		_ = os.RemoveAll(absDst)
		return "", err
	}
	return name, nil
}

// copySubtree 把 srcDir 下的子分组 / 请求 / 文档写入 dstDir（路径前缀替换，文件名不变）。
func (c *Collection) copySubtree(res *scanResult, srcDir, dstDir string) error {
	// 子分组：先父后子（按层级深度排序），父目录不存在时子分组写不进去
	subs := make([]string, 0, len(res.nameByDir))
	for dir := range res.nameByDir {
		if dir != srcDir && inSubtree(dir, srcDir) {
			subs = append(subs, dir)
		}
	}
	sort.Slice(subs, func(i, j int) bool { return strings.Count(subs[i], "/") < strings.Count(subs[j], "/") })
	for _, dir := range subs {
		dst := dstDir + strings.TrimPrefix(dir, srcDir)
		if err := os.MkdirAll(filepath.Join(c.Dir, filepath.FromSlash(dst)), 0o755); err != nil {
			return err
		}
		if err := c.ensureFolderYMLWithUID(dst, res.nameByDir[dir], uuid.NewString()); err != nil {
			return err
		}
	}

	for _, r := range res.reqs {
		if !inSubtree(folderOfPath(r.Path), srcDir) {
			continue
		}
		cp := cloneRequest(r)
		cp.UID = uuid.NewString()
		cp.Path = clonePath(dstDir, srcDir, r.Path)
		cp.ExpectHash = ""
		if err := c.SaveRequest(cp); err != nil {
			return err
		}
	}

	for _, d := range res.docs {
		if !inSubtree(folderOfPath(d.Path), srcDir) {
			continue
		}
		cp := *d
		cp.UID = uuid.NewString()
		cp.Path = clonePath(dstDir, srcDir, d.Path)
		if err := c.SaveDoc(&cp); err != nil {
			return err
		}
	}
	return nil
}

// clonePath 源路径在副本里的对应路径：目录前缀替换，文件名保持。
func clonePath(dstDir, srcDir, srcPath string) string {
	sub := strings.TrimPrefix(folderOfPath(srcPath), srcDir)
	return joinRel(dstDir+sub, filepath.Base(srcPath))
}

// inSubtree dir 是否就是 root 或位于其下（不看内容，只看路径）。
func inSubtree(dir, root string) bool {
	return dir == root || strings.HasPrefix(dir, root+"/")
}

// parentDir 相对目录的上一层（根 = 空串）。
func parentDir(dir string) string {
	if i := strings.LastIndex(dir, "/"); i >= 0 {
		return dir[:i]
	}
	return ""
}

// dirByUID 分组 uid → 相对目录。
func (c *Collection) dirByUID(res *scanResult, uid string) (string, bool) {
	for dir, u := range res.uidByDir {
		if u == uid {
			return dir, true
		}
	}
	return "", false
}
