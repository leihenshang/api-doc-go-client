package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/history"
)

// openRoot 建一个临时工作目录并作为「根」打开（devserver 语义：不走系统对话框）。
func openRoot(t *testing.T, core *App, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("建目录 %s: %v", name, err)
	}
	if _, err := core.OpenCollection(dir); err != nil {
		t.Fatalf("打开 %s: %v", name, err)
	}
	// 用例结束先释放索引句柄再让 TempDir 清理（Windows 上句柄占住时会删不掉；
	// TempDir 的清理在更早注册，按 LIFO 会在本回调之后执行）
	t.Cleanup(core.closeAllCollections)
	return dir
}

// hasRequest 树里是否存在某个名字的请求。
func hasRequest(nodes []*collection.Node, name string) bool {
	for _, n := range nodes {
		if n.Type == "request" && n.Name == name {
			return true
		}
		if hasRequest(n.Children, name) {
			return true
		}
	}
	return false
}

// 多根并存：打开/列根/幂等/活动根路由/关闭，互不干扰。
func TestMultiCollectionOpenListAndIsolation(t *testing.T) {
	isolateConfigDir(t)
	core := NewApp()
	alpha := openRoot(t, core, "alpha")
	beta := openRoot(t, core, "beta")

	list, err := core.ListCollections()
	if err != nil {
		t.Fatalf("ListCollections: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("应有 2 个根，实际 %d", len(list))
	}
	if list[0].Info.Dir != alpha || list[1].Info.Dir != beta {
		t.Fatalf("根顺序应为打开顺序: %s / %s", list[0].Info.Dir, list[1].Info.Dir)
	}
	if list[0].Active || !list[1].Active {
		t.Fatalf("活动根应为最后打开的 beta")
	}
	alphaRoot, betaRoot := list[0].Root, list[1].Root

	// 重复打开同一目录 = 幂等（不新增根），并置为活动根
	if _, err := core.OpenCollection(alpha); err != nil {
		t.Fatalf("重复打开 alpha: %v", err)
	}
	list, _ = core.ListCollections()
	if len(list) != 2 {
		t.Fatalf("重复打开不应新增根，实际 %d", len(list))
	}
	if !list[0].Active || core.ActiveCollection() != alphaRoot {
		t.Fatalf("重复打开应把 alpha 置为活动根")
	}

	// 集合级方法作用于活动根：请求落在 alpha 下，不落到 beta
	req, err := core.CreateRequest("", "ping", "GET")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(alpha, req.Path)); err != nil {
		t.Fatalf("请求应落在 alpha: %v", err)
	}
	if _, err := os.Stat(filepath.Join(beta, req.Path)); err == nil {
		t.Fatalf("请求不应落到 beta")
	}

	// 切到 beta：树里看不到 alpha 的请求；重载指定根不影响活动根
	if err := core.SetActiveCollection(betaRoot); err != nil {
		t.Fatalf("SetActiveCollection: %v", err)
	}
	if core.ActiveCollection() != betaRoot {
		t.Fatalf("活动根应为 beta")
	}
	info, err := core.ReloadCollection()
	if err != nil {
		t.Fatalf("ReloadCollection: %v", err)
	}
	if hasRequest(info.Tree, "ping") {
		t.Fatalf("beta 的树里不应出现 alpha 的请求")
	}
	alphaInfo, err := core.ReloadCollectionOf(alphaRoot)
	if err != nil {
		t.Fatalf("ReloadCollectionOf(alpha): %v", err)
	}
	if !hasRequest(alphaInfo.Tree, "ping") {
		t.Fatalf("alpha 的树里应有 ping: %+v", alphaInfo.Tree)
	}
	if core.ActiveCollection() != betaRoot {
		t.Fatalf("按 root 重载不应改变活动根")
	}

	// 关闭非活动根：其余根照常工作
	if err := core.CloseCollection(alphaRoot); err != nil {
		t.Fatalf("CloseCollection(alpha): %v", err)
	}
	list, _ = core.ListCollections()
	if len(list) != 1 || list[0].Root != betaRoot || !list[0].Active {
		t.Fatalf("关掉 alpha 后应只剩 beta 且仍是活动根: %+v", list)
	}
	if _, err := core.CreateRequest("", "pong", "GET"); err != nil {
		t.Fatalf("关掉一个根后其余根应可用: %v", err)
	}

	// 关闭活动根：活动根回退到剩余根；全关后集合级方法报「尚未打开」
	if err := core.CloseCollection(betaRoot); err != nil {
		t.Fatalf("CloseCollection(beta): %v", err)
	}
	if core.ActiveCollection() != "" {
		t.Fatalf("全关后活动根应为空")
	}
	if _, err := core.CreateRequest("", "x", "GET"); err == nil {
		t.Fatalf("没有打开任何根时应报错")
	}
	if _, err := core.ListCollections(); err != nil {
		t.Fatalf("ListCollections 在空表时不应报错: %v", err)
	}
}

// 历史按工作目录隔离：切到哪个根只看哪个根的记录；升级前无归属的旧记录两边都可见。
func TestMultiCollectionHistoryIsolation(t *testing.T) {
	isolateConfigDir(t)
	core := NewApp()
	openRoot(t, core, "alpha")
	openRoot(t, core, "beta")
	list, _ := core.ListCollections()
	alphaRoot, betaRoot := list[0].Root, list[1].Root

	// 升级前的旧记录（无归属）
	if err := history.Append(history.Entry{URL: "/legacy", Method: "GET"}, 50); err != nil {
		t.Fatalf("Append legacy: %v", err)
	}

	// alpha 里发一次（直接走记录入口，避免真的发请求）
	if err := core.SetActiveCollection(alphaRoot); err != nil {
		t.Fatalf("切 alpha: %v", err)
	}
	core.recordHistory(&collection.Request{UID: "u1", Name: "ping", Method: "get", URL: "http://a/x"}, nil, errors.New("失败"))

	items, err := core.ListHistory(0)
	if err != nil {
		t.Fatalf("ListHistory(alpha): %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("alpha 应看到 1 条自身 + 1 条旧记录，实际 %d", len(items))
	}
	for _, e := range items {
		if e.URL == "http://b/x" {
			t.Fatalf("alpha 不应看到 beta 的记录: %+v", e)
		}
	}
	if items[0].Root != alphaRoot {
		t.Fatalf("新记录应带 alpha 的 uid: %+v", items[0])
	}

	if err := core.SetActiveCollection(betaRoot); err != nil {
		t.Fatalf("切 beta: %v", err)
	}
	core.recordHistory(&collection.Request{UID: "u2", Name: "pong", Method: "get", URL: "http://b/x"}, nil, errors.New("失败"))
	items, _ = core.ListHistory(0)
	if len(items) != 2 {
		t.Fatalf("beta 应看到 1 条自身 + 1 条旧记录，实际 %d", len(items))
	}
	if items[0].Root != betaRoot {
		t.Fatalf("beta 的新记录应带 beta 的 uid: %+v", items[0])
	}

	// 清空只影响当前根（无归属的旧记录随任一次按目录清空一起清掉）：beta 清掉自己 1 条 + 旧记录，
	// alpha 自己的那条必须留着 —— 否则「按目录隔离」就退化成全局清空了。
	if err := core.ClearHistory(); err != nil {
		t.Fatalf("ClearHistory: %v", err)
	}
	items, _ = core.ListHistory(0)
	if len(items) != 0 {
		t.Fatalf("beta 的历史应已清空: %+v", items)
	}
	if err := core.SetActiveCollection(alphaRoot); err != nil {
		t.Fatalf("切回 alpha: %v", err)
	}
	items, _ = core.ListHistory(0)
	if len(items) != 1 || items[0].URL != "http://a/x" {
		t.Fatalf("清 beta 不应动 alpha 自己的记录: %+v", items)
	}
}

// 取消在途发送按集合归属：两个根出现同名 uid 时只取消活动根的那一条；草稿只取消活动根的。
func TestCancelSendScopedByCollection(t *testing.T) {
	isolateConfigDir(t)
	core := NewApp()
	openRoot(t, core, "alpha")
	openRoot(t, core, "beta")
	list, _ := core.ListCollections()
	alphaRoot, betaRoot := list[0].Root, list[1].Root

	cancelled := map[string]bool{}
	newIt := func(tag string) *inflightSend {
		return &inflightSend{cancel: func() { cancelled[tag] = true }}
	}
	a1, b1 := newIt("alpha-u1"), newIt("beta-u1")
	core.registerSend(alphaRoot, "u1", a1)
	core.registerSend(betaRoot, "u1", b1)

	// 活动根是 beta：取消 u1 只应命中 beta 那条（alpha 里同名 uid 的那条不受影响）
	core.CancelSend("u1")
	if !cancelled["beta-u1"] || cancelled["alpha-u1"] {
		t.Fatalf("应只取消活动根(beta)的在途发送: %+v", cancelled)
	}
	// 切到 alpha 再取消：这次命中 alpha 那条
	if err := core.SetActiveCollection(alphaRoot); err != nil {
		t.Fatalf("切 alpha: %v", err)
	}
	core.CancelSend("u1")
	if !cancelled["alpha-u1"] {
		t.Fatalf("切到 alpha 后应能取消 alpha 的在途发送: %+v", cancelled)
	}

	// 草稿发送（uid 为空）：只取消活动根的
	delete(cancelled, "alpha-draft")
	delete(cancelled, "beta-draft")
	da, db := newIt("alpha-draft"), newIt("beta-draft")
	core.registerSend(alphaRoot, "", da)
	core.registerSend(betaRoot, "", db)
	core.CancelSend("")
	if !cancelled["alpha-draft"] || cancelled["beta-draft"] {
		t.Fatalf("草稿取消只应命中活动根(alpha): %+v", cancelled)
	}
	// 收尾：把还在登记的发送注销掉，避免影响其它断言
	core.unregisterSend(db)
}

// 外部改动事件必须带集合身份：多根并存时前端据此只重载发生改动的那一根。
func TestWatchEventCarriesCollectionUID(t *testing.T) {
	isolateConfigDir(t)
	core := NewApp()
	alpha := openRoot(t, core, "alpha")
	beta := openRoot(t, core, "beta")
	list, _ := core.ListCollections()
	alphaRoot, betaRoot := list[0].Root, list[1].Root

	ch, unsub := core.SubscribeEvents()
	defer unsub()

	// 打开根的那一刻监听会先抖出一次「目录刚被创建」的事件（Windows 上异步投递），
	// 它不是我们要验证的「外部改动」，先排空噪声再制造改动。
	drain := time.After(800 * time.Millisecond)
draining:
	for {
		select {
		case <-ch:
		case <-drain:
			break draining
		}
	}

	// 往「非活动根」写文件：事件应带 alpha 的 uid（活动根是 beta）
	sub := filepath.Join(alpha, "api")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("建目录: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "folder.yml"), []byte("info:\n  name: api\n"), 0o644); err != nil {
		t.Fatalf("写文件: %v", err)
	}
	if _, err := os.Stat(filepath.Join(beta, "api")); err == nil {
		t.Fatalf("改动只应写在 alpha 下")
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case ev := <-ch:
			got, _ := ev["root"].(string)
			if got != alphaRoot {
				// 其它根/别的时机的残留事件跳过：最终仍要求见到 alpha 自己的事件
				continue
			}
			if _, ok := ev["paths"]; !ok {
				t.Fatalf("事件应带 paths: %+v", ev)
			}
			if core.ActiveCollection() != betaRoot {
				t.Fatalf("上报事件不应改变活动根")
			}
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Fatalf("10s 内未收到 alpha 的外部改动事件（活动根 beta=%s）", betaRoot)
}

// 活动根切换不会丢集合级状态：验证 SetActiveCollection 对未打开 uid 的拒绝。
func TestSetActiveCollectionRejectsUnknown(t *testing.T) {
	isolateConfigDir(t)
	core := NewApp()
	openRoot(t, core, "alpha")
	if err := core.SetActiveCollection("not-opened"); err == nil {
		t.Fatalf("切换未打开的根应报错")
	}
	if err := core.CloseCollection("not-opened"); err == nil {
		t.Fatalf("关闭未打开的根应报错")
	}
	// 传空 uid 也应报错，避免「静默把活动根清空」
	if err := core.SetActiveCollection(""); err == nil {
		t.Fatalf("空 uid 应报错")
	}
	if core.ActiveCollection() == "" {
		t.Fatalf("失败的切换不应把活动根清空")
	}
}

// 关闭根时停掉属于它的 Mock（避免守护一个已关闭集合的服务）。
func TestCloseCollectionStopsItsMock(t *testing.T) {
	isolateConfigDir(t)
	core := NewApp()
	openRoot(t, core, "alpha")
	uid := core.ActiveCollection()

	st, err := core.StartMock(0)
	if err != nil {
		t.Fatalf("StartMock: %v", err)
	}
	if !st.Running || st.Port == 0 {
		t.Fatalf("Mock 应已启动: %+v", st)
	}
	if got := core.MockStatus(); !got.Running {
		t.Fatalf("活动根的 Mock 状态应为运行中: %+v", got)
	}
	if err := core.CloseCollection(uid); err != nil {
		t.Fatalf("CloseCollection: %v", err)
	}
	if got := core.MockStatus(); got.Running {
		t.Fatalf("关掉该根后 Mock 应已停止: %+v", got)
	}
	// 幂等：再停一次不报错
	if err := core.StopMock(); err != nil {
		t.Fatalf("StopMock: %v", err)
	}
}

// 同一份集合的拷贝（清单里 uid 相同、路径不同）必须当成两个根：
// 这是「用路径而不是 uid 当身份」的原因 —— 用 uid 当键会互相覆盖，
// 也会让「关掉其中一个」把另一个一起带走。
func TestTwoRootsWithSameCollectionUID(t *testing.T) {
	isolateConfigDir(t)
	core := NewApp()
	t.Cleanup(core.closeAllCollections)
	orig := filepath.Join(t.TempDir(), "orig")
	if _, err := core.OpenCollection(orig); err != nil {
		t.Fatalf("打开 orig: %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(orig, "opencollection.yml"))
	if err != nil {
		t.Fatalf("读清单: %v", err)
	}
	copied := filepath.Join(t.TempDir(), "copied")
	if err := os.MkdirAll(copied, 0o755); err != nil {
		t.Fatalf("建拷贝目录: %v", err)
	}
	if err := os.WriteFile(filepath.Join(copied, "opencollection.yml"), manifest, 0o644); err != nil {
		t.Fatalf("写拷贝清单: %v", err)
	}
	if _, err := core.OpenCollection(copied); err != nil {
		t.Fatalf("打开拷贝目录: %v", err)
	}

	list, _ := core.ListCollections()
	if len(list) != 2 {
		t.Fatalf("同一 uid 的拷贝目录应算两个根，实际 %d", len(list))
	}
	if list[0].Info.UID != list[1].Info.UID {
		t.Fatalf("前置条件不成立：两个根应共用同一集合 uid（%s / %s）", list[0].Info.UID, list[1].Info.UID)
	}
	if list[0].Root == list[1].Root {
		t.Fatalf("两个根的标识必须区分开: %s", list[0].Root)
	}

	// 在拷贝目录里建一个请求：只应落在拷贝目录，且只能从拷贝目录搜到
	// （索引若按 uid 共用一个文件，原目录会把拷贝目录的行读出来）
	if err := core.SetActiveCollection(rootKey(copied)); err != nil {
		t.Fatalf("切到拷贝目录: %v", err)
	}
	req, err := core.CreateRequest("", "ping", "GET")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(copied, req.Path)); err != nil {
		t.Fatalf("请求应落在拷贝目录: %v", err)
	}
	hits, err := core.SearchIndex("ping", 0)
	if err != nil {
		t.Fatalf("SearchIndex(拷贝目录): %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("拷贝目录的索引里应有 ping")
	}
	if err := core.SetActiveCollection(rootKey(orig)); err != nil {
		t.Fatalf("切回 orig: %v", err)
	}
	hits, err = core.SearchIndex("ping", 0)
	if err != nil {
		t.Fatalf("SearchIndex(orig): %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("原目录的索引不应看到拷贝目录的请求: %+v", hits)
	}

	// 关掉其中一个：另一个必须还在（uid 当键时这里会把两个一起弄丢）
	if err := core.CloseCollection(rootKey(orig)); err != nil {
		t.Fatalf("CloseCollection(orig): %v", err)
	}
	list, _ = core.ListCollections()
	if len(list) != 1 || list[0].Root != rootKey(copied) {
		t.Fatalf("关掉 orig 后应只剩拷贝目录: %+v", list)
	}
	if core.ActiveCollection() != rootKey(copied) {
		t.Fatalf("活动根应回退到剩余的那个根")
	}
}

// 跨工作目录移动：目标侧重建（uid/路径由目标分配）、源文件进源集合 .trash、只读/同根被拒。
func TestMoveRequestAcrossCollections(t *testing.T) {
	isolateConfigDir(t)
	core := NewApp()
	alpha := openRoot(t, core, "alpha")
	beta := openRoot(t, core, "beta")
	alphaRoot, betaRoot := rootKey(alpha), rootKey(beta)

	if err := core.SetActiveCollection(alphaRoot); err != nil {
		t.Fatalf("切 alpha: %v", err)
	}
	req, err := core.CreateRequest("", "ping", "GET")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	srcFile := filepath.Join(alpha, req.Path)

	// 同一个根：拒绝
	if _, err := core.MoveRequestToCollection(alphaRoot, req.UID, alphaRoot, ""); err == nil {
		t.Fatalf("目标与源相同应报错")
	}
	// 未打开的根：拒绝
	if _, err := core.MoveRequestToCollection(alphaRoot, req.UID, "not-opened", ""); err == nil {
		t.Fatalf("目标未打开应报错")
	}

	newUID, err := core.MoveRequestToCollection(alphaRoot, req.UID, betaRoot, "")
	if err != nil {
		t.Fatalf("MoveRequestToCollection: %v", err)
	}
	if newUID == "" || newUID == req.UID {
		t.Fatalf("目标应重新分配 uid（旧 %s 新 %s）", req.UID, newUID)
	}
	// 源文件已移走（进 .trash，可找回）
	if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
		t.Fatalf("源文件应已移走: %v", err)
	}
	trash, err := os.ReadDir(filepath.Join(alpha, ".trash"))
	if err != nil || len(trash) == 0 {
		t.Fatalf("源文件应进 .trash: %v / %d", err, len(trash))
	}
	// 目标里有同名请求，且内容完整
	dest, err := core.collOf(betaRoot)
	if err != nil {
		t.Fatalf("取目标集合: %v", err)
	}
	moved, err := dest.ReadRequest(newUID)
	if err != nil {
		t.Fatalf("读目标请求: %v", err)
	}
	if moved.Name != "ping" || moved.Method != "GET" {
		t.Fatalf("目标请求内容不对: %+v", moved)
	}
	if !hasRequest(mustInfo(t, dest).Tree, "ping") {
		t.Fatalf("目标树里应有 ping")
	}
	// 目标集合的树/文件都在目标目录下（不会写回源目录）
	if _, err := os.Stat(filepath.Join(beta, moved.Path)); err != nil {
		t.Fatalf("目标请求文件应在目标目录里: %v", err)
	}
}

// mustInfo 取集合概要（测试辅助）。
func mustInfo(t *testing.T, c *collection.Collection) *collection.CollectionInfo {
	t.Helper()
	info, err := c.Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	return info
}
