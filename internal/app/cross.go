package app

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/syncengine"
)

// 跨工作目录移动（多根：把请求 / 分组从一个根拖到另一个根）。
//
// 语义选择：**目标侧重建 + 源侧进 .trash**，而不是移动文件：
//   - 请求与分组的 uid / 路径 / 序号都是集合内概念，跨目录后必须由目标集合重新分配，
//     否则两边会出现同一个 uid（后续索引、冲突检测、同步都会打架）；
//   - 源侧走既有 DeleteRequest / DeleteFolder → 进源集合的 .trash，可找回，与「删除」语义一致。
//
// 失败处理：单个请求是「先建后删」的原子动作（建失败 = 源未改动）；
// 分组的批量搬运按「预检 → 建结构 → 搬请求 → 删源」四步走，前两步失败时整体撤销，源保持可用。

// crossMoveTargets 解析并校验一次跨工作目录移动的两端（请求与分组移动共用）。
func (a *App) crossMoveTargets(srcRoot, destRoot string) (*collection.Collection, *collection.Collection, error) {
	if destRoot == "" {
		return nil, nil, errors.New("目标工作目录为空")
	}
	src, err := a.collOf(srcRoot)
	if err != nil {
		return nil, nil, err
	}
	dest, err := a.collOf(destRoot)
	if err != nil {
		return nil, nil, err
	}
	if src == dest {
		return nil, nil, errors.New("目标与源是同一个工作目录")
	}
	// 只读镜像（同步 mirror）根既不能写也不能删：两侧都先校验，避免「复制成功但删不掉」
	if err := a.ensureWritable(src, "源工作目录"); err != nil {
		return nil, nil, err
	}
	if err := a.ensureWritable(dest, "目标工作目录"); err != nil {
		return nil, nil, err
	}
	return src, dest, nil
}

// MoveRequestToCollection 把某个根里的请求移到另一个根（srcRoot 空 = 活动根）。
// 返回目标集合里新建请求的 uid（供界面切过去并打开）。
func (a *App) MoveRequestToCollection(srcRoot, uid, destRoot, destFolder string) (string, error) {
	if uid == "" {
		return "", errors.New("请求 uid 为空")
	}
	src, dest, err := a.crossMoveTargets(srcRoot, destRoot)
	if err != nil {
		return "", err
	}
	return a.moveRequestAcross(src, dest, uid, destFolder)
}

// moveRequestAcross 把一个请求从 src 搬到 dest 的 destDir（相对路径，空 = 根）。
//
// 原子性：目标侧建成功后才删源；删源失败时如实说明「副本已建、源没删掉」，
// 不让用户以为什么都没发生（也不假装成功）。
func (a *App) moveRequestAcross(src, dest *collection.Collection, uid, destDir string) (string, error) {
	req, err := src.ReadRequest(uid)
	if err != nil {
		return "", err
	}
	// 目标侧重建：uid / 路径 / 序号 / base_rev 由目标集合决定（内容保持）
	created, err := dest.CreateRequestFromDraft(destDir, req.Name, req)
	if err != nil {
		return "", fmt.Errorf("在目标工作目录里创建请求失败（源未改动）：%w", err)
	}
	if err := src.DeleteRequest(uid); err != nil {
		return "", fmt.Errorf("已复制到目标工作目录（新请求 %s），但删除源文件失败：%w", created.Name, err)
	}
	return created.UID, nil
}

// movedRequest 一次跨目录搬运里已完成的请求（回滚需要「原 uid / 新 uid / 原目录」）。
type movedRequest struct {
	srcUID  string
	destUID string
	srcDir  string
}

// MoveFolderToCollection 把分组**连同其下所有子分组与请求**移动到另一个根（srcRoot 空 = 活动根）。
//
// 四步（顺序是为了让每一步失败都还能收场）：
//  1. 预检：两端可写、源分组存在且不是集合根、目标分组存在、目标下没有同名分组 —— 不通过就一个字都不改；
//  2. 在目标侧镜像出整棵分组结构（只有目录 + folder.yml，最轻、可整体撤销）；
//  3. 逐个把请求搬过去（每个请求自身是原子的）；
//  4. 自底向上删掉源分组（空分组才删得掉）—— 整棵进源集合 .trash，可找回。
//
// 失败处理：第 2/3 步失败 → 把已搬的请求搬回原位、把已建的目标目录按逆序删掉，源保持可用
// （搬回去的请求会拿到新 uid，界面里对应标签需要重开）；第 4 步失败只留下源侧空目录，
// 内容已经在目标，如实回报让用户手工清理。
//
// 返回**被搬走的源请求 uid**（界面据此清掉源根里那些已失效的标签）。
func (a *App) MoveFolderToCollection(srcRoot, uid, destRoot, destParent string) ([]string, error) {
	if strings.TrimSpace(uid) == "" {
		return nil, errors.New("分组 uid 为空")
	}
	src, dest, err := a.crossMoveTargets(srcRoot, destRoot)
	if err != nil {
		return nil, err
	}

	// ---- 1. 预检 ----
	srcTree, err := src.Tree()
	if err != nil {
		return nil, err
	}
	node := findFolderByUID(srcTree, uid)
	if node == nil {
		return nil, errors.New("分组不存在或已被删除")
	}
	if node.Path == "" {
		return nil, errors.New("不能移动集合根目录")
	}
	destTree, err := dest.Tree()
	if err != nil {
		return nil, err
	}
	if destParent != "" && findFolderByPath(destTree, destParent) == nil {
		return nil, errors.New("目标分组不存在或已被删除")
	}
	// 同名就拒绝：CreateFolder 对已存在的目录是「复用」，会悄悄合并进别人的分组
	if folderByName(destTree, destParent, node.Name) != nil {
		return nil, fmt.Errorf("目标分组下已存在同名分组「%s」", node.Name)
	}

	// ---- 2. 镜像目标结构（深度优先；建完即从目标树里解出真实路径）----
	var made []string // 已建出来的目标目录（逆序回滚用）
	// destOf 源分组路径 → 目标目录：搬运阶段照它走，避免双方清理规则不同导致落到别的目录
	destOf := map[string]string{}
	var mirror func(n *collection.Node, destDir string) (string, error)
	mirror = func(n *collection.Node, destDir string) (string, error) {
		if err := dest.CreateFolder(destDir, n.Name); err != nil {
			return "", fmt.Errorf("在目标工作目录里新建分组失败（源未改动）：%w", err)
		}
		newDir := resolveCreatedDir(dest, destDir, n.Name, joinRel(destDir, path.Base(n.Path)))
		made = append(made, newDir)
		destOf[n.Path] = newDir
		for _, ch := range n.Children {
			if ch.Type != "folder" {
				continue
			}
			if _, err := mirror(ch, newDir); err != nil {
				return "", err
			}
		}
		return newDir, nil
	}
	if _, err := mirror(node, destParent); err != nil {
		rollbackCreatedFolders(dest, made)
		return nil, err
	}

	// ---- 3. 搬请求（深度优先，按 destOf 落到对应目标分组）----
	var moved []movedRequest
	var walkErr error
	var walk func(n *collection.Node)
	walk = func(n *collection.Node) {
		if walkErr != nil {
			return
		}
		destDir := destOf[n.Path]
		for _, ch := range n.Children {
			switch ch.Type {
			case "folder":
				walk(ch)
			case "request":
				newUID, err := a.moveRequestAcross(src, dest, ch.UID, destDir)
				if err != nil {
					walkErr = err
					return
				}
				moved = append(moved, movedRequest{srcUID: ch.UID, destUID: newUID, srcDir: relDirOf(ch.Path)})
			}
		}
	}
	walk(node)
	if walkErr != nil {
		// 回滚：逆序把已搬的请求搬回去（源结构此时仍完整，所以放得回原位）
		if rbErr := a.rollbackMovedRequests(src, dest, moved); rbErr != nil {
			return nil, fmt.Errorf("%v；回滚未完全成功（%v），请检查两个工作目录后手工整理", walkErr, rbErr)
		}
		rollbackCreatedFolders(dest, made)
		return nil, fmt.Errorf("%v；已回滚（%d 个请求已搬回源工作目录，uid 会变化，请重新打开对应标签）", walkErr, len(moved))
	}

	// ---- 4. 删源（自底向上：空分组才删得掉；整棵进源集合 .trash）----
	uids := movedRequestUIDs(node)
	if err := deleteFoldersBottomUp(src, node); err != nil {
		return uids, fmt.Errorf("内容已搬到目标工作目录（%s），但清理源分组失败，请手工删除：%w", dest.Name, err)
	}
	return uids, nil
}

// resolveCreatedDir 解出「刚在 parent 下建的同名分组」在目标集合里的真实相对路径。
//
// 绝大多数情况走快路径（同名进同名：CreateFolder 用的清理规则与源目录名一致）。
// 但外部创建的分组（无 folder.yml、uid 形如 dir:<路径>）显示名可能被清理规则改写，
// 这时重新问一次目标树，避免把请求落到一个「看起来对、其实另建了一份」的目录里。
func resolveCreatedDir(c *collection.Collection, parent, name, computed string) string {
	tree, err := c.Tree()
	if err != nil {
		return computed
	}
	if findFolderByPath(tree, computed) != nil {
		return computed
	}
	if n := folderByName(tree, parent, name); n != nil {
		return n.Path
	}
	return computed
}

// rollbackMovedRequests 逆序把已搬走的请求搬回源目录（源结构未动，可放回原位）。
func (a *App) rollbackMovedRequests(src, dest *collection.Collection, moved []movedRequest) error {
	for i := len(moved) - 1; i >= 0; i-- {
		if _, err := a.moveRequestAcross(dest, src, moved[i].destUID, moved[i].srcDir); err != nil {
			return err
		}
	}
	return nil
}

// rollbackCreatedFolders 按**逆序**删掉刚在目标侧建出来的空目录（先深后浅，空的才删得掉）。
// 尽力而为：残留空目录不影响内容正确性。
func rollbackCreatedFolders(dest *collection.Collection, dirs []string) {
	if len(dirs) == 0 {
		return
	}
	tree, err := dest.Tree()
	if err != nil {
		return
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		n := findFolderByPath(tree, dirs[i])
		if n == nil || n.UID == "" {
			continue
		}
		_ = dest.DeleteFolder(n.UID)
	}
}

// deleteFoldersBottomUp 自底向上删除分组（DeleteFolder 只接受空分组，故必须先删子层）。
func deleteFoldersBottomUp(c *collection.Collection, n *collection.Node) error {
	for _, ch := range n.Children {
		if ch.Type != "folder" {
			continue
		}
		if err := deleteFoldersBottomUp(c, ch); err != nil {
			return err
		}
	}
	return c.DeleteFolder(n.UID)
}

// movedRequestUIDs 收集分组子树里的请求 uid（界面用它清掉已失效的标签）。
func movedRequestUIDs(n *collection.Node) []string {
	var out []string
	var walk func(x *collection.Node)
	walk = func(x *collection.Node) {
		for _, ch := range x.Children {
			switch ch.Type {
			case "folder":
				walk(ch)
			case "request":
				out = append(out, ch.UID)
			}
		}
	}
	walk(n)
	return out
}

// ensureWritable 校验工作目录是否允许改动：只读镜像（sync mode=mirror）根拒绝写。
func (a *App) ensureWritable(c *collection.Collection, label string) error {
	b, err := syncengine.LoadBind(c.Dir)
	if err != nil || !b.Linked {
		return nil // 未关联服务端的本地集合：可写
	}
	if b.Mode == syncengine.ModeMirror {
		return fmt.Errorf("%s（%s）是只读镜像（同步模式 mirror），不能修改内容", label, c.Name)
	}
	return nil
}

// ---- 树查询（都是相对路径 / 显示名的字符串运算，不引入新的集合层 API）----

// findFolderByUID 在树里按 uid 找分组节点。
func findFolderByUID(tree []*collection.Node, uid string) *collection.Node {
	for _, n := range tree {
		if n.Type != "folder" {
			continue
		}
		if n.UID == uid {
			return n
		}
		if got := findFolderByUID(n.Children, uid); got != nil {
			return got
		}
	}
	return nil
}

// findFolderByPath 在树里按相对路径找分组节点（集合根不在树节点里，故 dir 为空返回 nil）。
func findFolderByPath(tree []*collection.Node, dir string) *collection.Node {
	if dir == "" {
		return nil
	}
	for _, n := range tree {
		if n.Type != "folder" {
			continue
		}
		if n.Path == dir {
			return n
		}
		if got := findFolderByPath(n.Children, dir); got != nil {
			return got
		}
	}
	return nil
}

// folderByName 在 dir（空 = 集合根）下按**显示名**找分组。
func folderByName(tree []*collection.Node, dir, name string) *collection.Node {
	nodes := tree
	if dir != "" {
		parent := findFolderByPath(tree, dir)
		if parent == nil {
			return nil
		}
		nodes = parent.Children
	}
	for _, n := range nodes {
		if n.Type == "folder" && n.Name == name {
			return n
		}
	}
	return nil
}

// joinRel 拼接集合内相对路径（统一 / 分隔符；父级为空直接返回子名）。
func joinRel(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "/" + name
}

// relDirOf 取请求所在目录的相对路径（根目录返回空串）。
func relDirOf(rel string) string {
	dir := path.Dir(rel)
	if dir == "." || dir == "/" {
		return ""
	}
	return dir
}
