// 同层自定义拖动排序：把节点放到目标分组的指定位置。
//
// 排序真相源是各文件自带的 seq 权重（请求/文档走 info.seq / frontmatter seq，
// 分组走 folder.yml 的 info.seq）；集合树按「分组在前，组内 seq 升序」渲染，
// 因此本文件只重编号**同类型**兄弟（分组一套 1..n，叶子一套 1..n），不交叉排序。
package collection

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ArrangeNode 把节点放到 destFolder 的顺序位置：beforeUID 非空 = 插到该节点之前，
// 空 = 插到同类型末尾。destFolder 空 = 集合根。
//
// 拖动排序与「改上级」共用一条写入路径：先移动（同目录时是 no-op），
// 再按目标目录的最终顺序给同层兄弟重编号 —— 只写 seq 真有变化的文件。
func (c *Collection) ArrangeNode(uid, destFolder, beforeUID string) error {
	if strings.TrimSpace(uid) == "" {
		return fmt.Errorf("缺少 uid")
	}
	if uid == beforeUID {
		beforeUID = ""
	}
	kind, err := c.moveForArrange(uid, destFolder)
	if err != nil {
		return err
	}
	res, err := c.scan()
	if err != nil {
		return err
	}
	kids := c.treeFromScan(res)
	if dir := normalizeRelDir(destFolder); dir != "" {
		kids = childrenOfPath(kids, dir)
		if kids == nil {
			return fmt.Errorf("目标分组不存在")
		}
	}
	groupIsFolder := kind == "folder"
	ordered := make([]*Node, 0, len(kids))
	moved := (*Node)(nil)
	for _, n := range kids {
		if n.UID == uid {
			moved = n
			continue
		}
		if (n.Type == "folder") == groupIsFolder {
			ordered = append(ordered, n)
		}
	}
	if moved == nil {
		return errNotFound
	}
	at := len(ordered)
	for i, n := range ordered {
		if n.UID == beforeUID {
			at = i
			break
		}
	}
	ordered = append(ordered, nil)
	copy(ordered[at+1:], ordered[at:])
	ordered[at] = moved
	return c.applySiblingSeq(ordered, res)
}

// normalizeRelDir 归一目标分组相对路径（"" / "." 都表示集合根）。
func normalizeRelDir(dir string) string {
	clean := filepath.ToSlash(filepath.Clean(dir))
	if clean == "." {
		return ""
	}
	return clean
}

// childrenOfPath 在树里按相对路径取分组的子节点（保持显示顺序）；path 空 = 集合根。
func childrenOfPath(nodes []*Node, path string) []*Node {
	if path == "" {
		return nodes
	}
	for _, n := range nodes {
		if n.Type != "folder" {
			continue
		}
		if n.Path == path {
			return n.Children
		}
		if len(n.Children) > 0 {
			if hit := childrenOfPath(n.Children, path); hit != nil {
				return hit
			}
		}
	}
	return nil
}

// moveForArrange 把节点搬到目标分组并返回其类型（request / doc / folder）。
// 复用既有 Move*：目标不合法（成环、同名冲突、跨目录非法路径）由它们统一报错。
func (c *Collection) moveForArrange(uid, destFolder string) (string, error) {
	res, err := c.scan()
	if err != nil {
		return "", err
	}
	if _, ok := res.reqs[uid]; ok {
		return "request", c.MoveRequest(uid, destFolder)
	}
	if _, ok := res.docs[uid]; ok {
		return "doc", c.MoveDoc(uid, destFolder)
	}
	if _, err := c.findDir(uid); err != nil {
		return "", err
	}
	return "folder", c.MoveFolder(uid, destFolder)
}

// applySiblingSeq 按给定顺序给同类型兄弟重编号（1..n），只写回 seq 真有变化的文件。
func (c *Collection) applySiblingSeq(nodes []*Node, res *scanResult) error {
	for i, n := range nodes {
		want := i + 1
		switch n.Type {
		case "folder":
			if res.seqByDir[n.Path] == want {
				continue
			}
			if err := c.setFolderSeq(n.Path, want); err != nil {
				return err
			}
		case "doc":
			d, ok := res.docs[n.UID]
			if !ok || d.Seq == want {
				continue
			}
			d.Seq = want
			if err := c.SaveDoc(d); err != nil {
				return err
			}
		default: // request（HTTP / gRPC 同走 info.seq）
			r, ok := res.reqs[n.UID]
			if !ok || r.Seq == want {
				continue
			}
			r.Seq = want
			if err := c.SaveRequest(r); err != nil {
				return err
			}
		}
	}
	return nil
}

// setFolderSeq 写回分组 folder.yml 的 info.seq（保留其它字段；缺 folder.yml 时先补一个）。
// 没有 folder.yml 的目录在树里用 "dir:<路径>" 兜底 uid —— ensureFolderYML 会补上稳定 uid。
func (c *Collection) setFolderSeq(dir string, seq int) error {
	dir = normalizeRelDir(dir)
	if dir == "" {
		return nil
	}
	// 路径来自集合内扫描结果，但仍显式校验一次：绝对路径 / ../ 一律拒绝，
	// 并确认最终落点仍在集合根内（写文件前必须做这个兜底）。
	if !safeRelPath(dir) {
		return fmt.Errorf("非法的分组路径")
	}
	full := filepath.Join(c.Dir, filepath.FromSlash(dir), "folder.yml")
	if !withinDir(c.Dir, full) {
		return fmt.Errorf("非法的分组路径")
	}
	if err := c.ensureFolderYML(dir, filepath.Base(dir)); err != nil {
		return err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return fmt.Errorf("读取分组描述: %w", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("解析分组描述: %w", err)
	}
	info, _ := doc["info"].(map[string]any)
	if info == nil {
		info = map[string]any{"type": "folder"}
	}
	info["seq"] = seq
	doc["info"] = info
	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("序列化分组描述: %w", err)
	}
	c.ignoreWriteContent(filepath.ToSlash(filepath.Join(dir, "folder.yml")), out)
	return writeFileAtomic(full, out, 0o644)
}

// safeRelPath 相对路径是否安全：非空、非绝对路径、且不指向父级目录。
func safeRelPath(rel string) bool {
	if rel == "" || filepath.IsAbs(rel) {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(rel))
	return clean != ".." && !strings.HasPrefix(clean, "../")
}

// withinDir 目标路径是否落在 base 目录内（按路径分隔符边界判断，防止 a/b 误配 a/bc）。
func withinDir(base, target string) bool {
	b, err := filepath.Abs(base)
	if err != nil {
		return false
	}
	t, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(b, t)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
