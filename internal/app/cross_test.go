package app

import (
	"os"
	"path/filepath"
	"testing"

	"api-doc-go-client/internal/collection"
)

// folderUID 在树里按显示名找分组 uid（测试辅助）。
func folderUID(nodes []*collection.Node, name string) string {
	for _, n := range nodes {
		if n.Type == "folder" && n.Name == name {
			return n.UID
		}
		if got := folderUID(n.Children, name); got != "" {
			return got
		}
	}
	return ""
}

// requestUID 在树里按显示名找请求 uid。
func requestUID(nodes []*collection.Node, name string) string {
	for _, n := range nodes {
		if n.Type == "request" && n.Name == name {
			return n.UID
		}
		if got := requestUID(n.Children, name); got != "" {
			return got
		}
	}
	return ""
}

// mustTree 取活动根的树（用例辅助）。
func mustTree(t *testing.T, core *App) []*collection.Node {
	t.Helper()
	info, err := core.ReloadCollection()
	if err != nil {
		t.Fatalf("ReloadCollection: %v", err)
	}
	return info.Tree
}

// 跨工作目录移动分组：整棵镜像过去（目标侧新 uid）、空分组也跟过去、源分组整棵进 .trash；
// 目标已有同名分组时预检拒绝且源一个字都不动。
func TestMoveFolderAcrossCollections(t *testing.T) {
	isolateConfigDir(t)
	core := NewApp()
	alpha := openRoot(t, core, "alpha")
	beta := openRoot(t, core, "beta")
	aRoot, bRoot := rootKey(alpha), rootKey(beta)

	// ---- alpha：铺一棵两级分组（含两个请求 + 一个空分组）----
	if err := core.SetActiveCollection(aRoot); err != nil {
		t.Fatalf("切 alpha: %v", err)
	}
	for _, f := range []struct{ parent, name string }{
		{"", "用户"}, {"用户", "管理"}, {"用户", "空组"},
	} {
		if err := core.CreateFolder(f.parent, f.name); err != nil {
			t.Fatalf("建分组 %s/%s: %v", f.parent, f.name, err)
		}
	}
	ping, err := core.CreateRequest("用户", "ping", "GET")
	if err != nil {
		t.Fatalf("建请求 ping: %v", err)
	}
	ban, err := core.CreateRequest("用户/管理", "ban", "POST")
	if err != nil {
		t.Fatalf("建请求 ban: %v", err)
	}

	// ---- beta：先占一个同名分组，验证预检 ----
	if err := core.SetActiveCollection(bRoot); err != nil {
		t.Fatalf("切 beta: %v", err)
	}
	if err := core.CreateFolder("", "用户"); err != nil {
		t.Fatalf("beta 建 用户: %v", err)
	}
	betaUserUID := folderUID(mustTree(t, core), "用户")
	if betaUserUID == "" {
		t.Fatalf("beta 树里应有「用户」分组")
	}

	if err := core.SetActiveCollection(aRoot); err != nil {
		t.Fatalf("切回 alpha: %v", err)
	}
	srcUID := folderUID(mustTree(t, core), "用户")
	if srcUID == "" {
		t.Fatalf("alpha 树里应有「用户」分组")
	}
	if _, err := core.MoveFolderToCollection(aRoot, srcUID, bRoot, ""); err == nil {
		t.Fatalf("目标下已有同名分组时应拒绝")
	}
	// 预检失败必须一个字都不改
	if _, err := os.Stat(filepath.Join(alpha, "用户", "ping.yml")); err != nil {
		t.Fatalf("预检失败不应改动源: %v", err)
	}
	if _, err := os.Stat(filepath.Join(beta, "用户", "ping.yml")); err == nil {
		t.Fatalf("预检失败不应写入目标")
	}

	// ---- 挪走 beta 的同名空分组，正式搬 ----
	if err := core.SetActiveCollection(bRoot); err != nil {
		t.Fatalf("切 beta: %v", err)
	}
	if err := core.DeleteFolder(betaUserUID); err != nil {
		t.Fatalf("删 beta 的占位分组: %v", err)
	}
	if err := core.SetActiveCollection(aRoot); err != nil {
		t.Fatalf("切回 alpha: %v", err)
	}

	moved, err := core.MoveFolderToCollection(aRoot, srcUID, bRoot, "")
	if err != nil {
		t.Fatalf("移动分组: %v", err)
	}
	// 返回值是**源**请求 uid：界面据此清掉源根里失效的标签
	if len(moved) != 2 {
		t.Fatalf("应返回两个被搬走的源请求 uid，实际 %v", moved)
	}
	got := map[string]bool{moved[0]: true, moved[1]: true}
	if !got[ping.UID] || !got[ban.UID] {
		t.Fatalf("返回的 uid 应是源侧的 ping/ban：%v", moved)
	}

	// ---- 源：分组消失，整棵进 .trash（可找回）----
	if _, err := os.Stat(filepath.Join(alpha, "用户")); !os.IsNotExist(err) {
		t.Fatalf("源分组应已移走: %v", err)
	}
	trash, err := os.ReadDir(filepath.Join(alpha, ".trash"))
	if err != nil || len(trash) < 3 {
		t.Fatalf("源分组应整棵进 .trash（用户/管理/空组）: %v / %d", err, len(trash))
	}

	// ---- 目标：结构 + 内容 + 空分组都在 ----
	for _, rel := range []string{
		filepath.Join("用户", "ping.yml"),
		filepath.Join("用户", "管理", "ban.yml"),
		filepath.Join("用户", "空组", "folder.yml"),
	} {
		if _, err := os.Stat(filepath.Join(beta, rel)); err != nil {
			t.Fatalf("目标应有 %s: %v", rel, err)
		}
	}
	destColl, err := core.collOf(bRoot)
	if err != nil {
		t.Fatalf("取目标集合: %v", err)
	}
	destInfo, err := destColl.Info()
	if err != nil {
		t.Fatalf("目标 Info: %v", err)
	}
	if !hasRequest(destInfo.Tree, "ping") || !hasRequest(destInfo.Tree, "ban") {
		t.Fatalf("目标树里应有两个请求")
	}
	if folderUID(destInfo.Tree, "空组") == "" {
		t.Fatalf("空分组也应跟过去")
	}
	// 请求是目标侧重建的：uid 必须重新分配（否则两个目录会出现同一个 uid）
	if uid := requestUID(destInfo.Tree, "ping"); uid == "" || uid == ping.UID {
		t.Fatalf("目标请求应拿到新 uid（源 %s，目标 %s）", ping.UID, uid)
	}

	// ---- 移动集合根：拒绝（根不是树节点，按「分组不存在」处理）----
	if _, err := core.MoveFolderToCollection(aRoot, "", bRoot, ""); err == nil {
		t.Fatalf("空 uid 应拒绝")
	}
	if _, err := core.MoveFolderToCollection(aRoot, "no-such-uid", bRoot, ""); err == nil {
		t.Fatalf("不存在的分组应拒绝")
	}
	// 同一个根：拒绝
	if _, err := core.MoveFolderToCollection(aRoot, srcUID, aRoot, ""); err == nil {
		t.Fatalf("目标与源相同应拒绝")
	}
}

// 回滚原语：已搬走的请求能搬回原目录、刚在目标侧建出来的空目录能删掉。
// 跨根搬分组中途失败时就是靠这两步把状态收回去（源结构未动，所以放得回原位）。
func TestCrossMoveRollbackPrimitives(t *testing.T) {
	isolateConfigDir(t)
	core := NewApp()
	alpha := openRoot(t, core, "alpha")
	beta := openRoot(t, core, "beta")
	aRoot, bRoot := rootKey(alpha), rootKey(beta)

	if err := core.SetActiveCollection(aRoot); err != nil {
		t.Fatalf("切 alpha: %v", err)
	}
	if err := core.CreateFolder("", "组"); err != nil {
		t.Fatalf("建分组 组: %v", err)
	}
	req, err := core.CreateRequest("组", "ping", "GET")
	if err != nil {
		t.Fatalf("建请求 ping: %v", err)
	}
	src, err := core.collOf(aRoot)
	if err != nil {
		t.Fatalf("取源集合: %v", err)
	}
	dest, err := core.collOf(bRoot)
	if err != nil {
		t.Fatalf("取目标集合: %v", err)
	}

	// 模拟 mirror 阶段的产物：目标侧同名空分组 + 已搬过去的一个请求
	if err := dest.CreateFolder("", "组"); err != nil {
		t.Fatalf("目标建分组: %v", err)
	}
	newUID, err := core.moveRequestAcross(src, dest, req.UID, "组")
	if err != nil {
		t.Fatalf("moveRequestAcross: %v", err)
	}
	if _, err := os.Stat(filepath.Join(alpha, "组", "ping.yml")); !os.IsNotExist(err) {
		t.Fatalf("源请求应已搬走: %v", err)
	}

	// 回滚：请求搬回源目录（新 uid，但内容照旧），目标侧刚建的空目录删掉
	if err := core.rollbackMovedRequests(src, dest, []movedRequest{
		{srcUID: req.UID, destUID: newUID, srcDir: "组"},
	}); err != nil {
		t.Fatalf("rollbackMovedRequests: %v", err)
	}
	rollbackCreatedFolders(dest, []string{"组"})

	if _, err := os.Stat(filepath.Join(alpha, "组", "ping.yml")); err != nil {
		t.Fatalf("请求应已搬回源目录: %v", err)
	}
	if _, err := os.Stat(filepath.Join(beta, "组")); !os.IsNotExist(err) {
		t.Fatalf("刚建的目标空目录应被删掉: %v", err)
	}
}
