package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"runtime"
	"strings"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/mocksrv"
	"api-doc-go-client/internal/proto"
	"api-doc-go-client/internal/syncengine"
)

// 多工作目录（单树多根）的集合表。
//
// 设计：App 从「一个 coll 槽位」改成「一张按**工作目录**索引的表 + 一个活动根」。
// 所有**集合级**导出方法仍只经 requireCollection() 解析集合，因此这次改造不需要改动
// 那 60 多个方法的签名（也就不会牵动 frontend/wailsjs 的生成绑定与 devserver 反射桥的
// 参数个数校验）；需要显式指定「非活动根」的动作（列根、切换、关闭、重载、跨根搜索/移动）
// 才走新增的带 root 参数方法。
//
// 身份用「规范化绝对路径」而不是集合 uid：uid 写在集合清单里，两个工作目录完全可能是
// 同一份集合的拷贝（uid 相同、路径不同 —— e2e 夹具每次拷贝种子就是这种情况），
// 用 uid 当表键会互相覆盖，也会让 session/历史 无法区分。root key 在 Windows 上忽略大小写。

// errNoCollection 未打开任何工作目录时的统一错误（沿用既有文案）。
var errNoCollection = errors.New("尚未打开集合目录")

// rootKey 工作目录的标识：规范化后的绝对路径（Windows 上忽略大小写）。
func rootKey(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	dir = filepath.Clean(dir)
	if runtime.GOOS == "windows" {
		dir = strings.ToLower(dir)
	}
	return dir
}

// shortHash 8 字节 hex 的稳定短哈希：给索引键这类需要落成文件名的场景用
// （字符集满足集合层的索引名校验，长度也不会超）。
func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

// collEntry 一个已打开的工作目录（集合）及其**只属于它**的会话级状态。
//
// 为什么这些字段必须跟着目录走：以前 App 只开一个集合，Mock / 同步令牌 / 同步状态 /
// 定时同步信号 / 定义编译缓存都直接挂在 App 上。多根并存后如果继续共用，会出现
// 「在 A 启动的 Mock 去回放 B 的示例」「切到 B 把 A 的 PAT 覆盖掉」
// 「B 的停止信号把 A 的定时同步关掉」「A 导入的定义把 B 的缓存清空」。
type collEntry struct {
	coll *collection.Collection
	// info 最近一次打开/重载得到的概要：侧栏根行直接用它，避免「列根」时全量 walk 磁盘
	info *collection.CollectionInfo

	protoCache *proto.Cache

	// syncToken 当前会话的 PAT（不落盘；与该集合内的 .sync.json 绑定一一对应）
	syncToken string
	// syncStatus 状态栏同步状态（按根各自记录）
	syncStatus SyncStatus
	// syncStop 定时同步停止信号（按根各自停止）
	syncStop chan struct{}
}

// ---------- 表访问（以下 *Locked 方法要求调用方持 a.mu） ----------

// activeEntryLocked 活动根条目；没有打开任何目录时返回 nil。
func (a *App) activeEntryLocked() *collEntry {
	if a.activeRoot == "" {
		return nil
	}
	return a.colls[a.activeRoot]
}

// entryLocked 按 root 取条目；root 为空 = 活动根。
func (a *App) entryLocked(root string) *collEntry {
	if strings.TrimSpace(root) == "" {
		return a.activeEntryLocked()
	}
	return a.colls[rootKey(root)]
}

// entryOfLocked 按集合指针身份找条目（同步/编译缓存这类已经持有 *Collection 的场景用）。
func (a *App) entryOfLocked(c *collection.Collection) *collEntry {
	for _, e := range a.colls {
		if e.coll == c {
			return e
		}
	}
	return nil
}

// ---- 供调用方使用的无锁封装 ----

// collOf 解析集合：root 为空 = 活动集合。
func (a *App) collOf(root string) (*collection.Collection, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	e := a.entryLocked(root)
	if e == nil {
		return nil, errNoCollection
	}
	return e.coll, nil
}

// requireCollection 取活动集合。所有集合级导出方法都经它解析 ——
// 换句话说，「多根并存」在 Go 侧只改了这一个解析入口。
func (a *App) requireCollection() (*collection.Collection, error) {
	return a.collOf("")
}

// activeRootID 活动工作目录的标识（历史归属、在途发送键、草稿取消都要用）。
func (a *App) activeRootID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.activeRoot
}

// refreshInfo 重新扫描集合并把概要写回缓存（打开后 / 外部改动重载 / 手动重载）。
func (a *App) refreshInfo(c *collection.Collection) (*collection.CollectionInfo, error) {
	info, err := c.Info()
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	if e := a.entryOfLocked(c); e != nil {
		e.info = info
	}
	a.mu.Unlock()
	return info, nil
}

// removeFromOrder 从打开顺序表里去掉一个 root（保持其余顺序）。
func removeFromOrder(list []string, root string) []string {
	out := make([]string, 0, len(list))
	for _, x := range list {
		if x != root {
			out = append(out, x)
		}
	}
	return out
}

// ---------- 每根的编译缓存 / 同步状态 ----------

// protoCacheOf 取该集合的 gRPC 定义编译缓存（惰性创建）。
// 按集合隔离：A 导入定义时只失效 A 的缓存，不会让 B 白编译一遍。
func (a *App) protoCacheOf(c *collection.Collection) *proto.Cache {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e := a.entryOfLocked(c); e != nil {
		if e.protoCache == nil {
			e.protoCache = proto.NewCache()
		}
		return e.protoCache
	}
	// 未登记的集合（理论上不会发生）：退化成一次性缓存，保证不 panic、结果依然正确
	return proto.NewCache()
}

// setSyncToken 记住该集合的会话 PAT（不落盘）。
func (a *App) setSyncToken(c *collection.Collection, token string) {
	a.mu.Lock()
	if e := a.entryOfLocked(c); e != nil {
		e.syncToken = token
	}
	a.mu.Unlock()
}

// syncTokenOf 读该集合的会话 PAT。
func (a *App) syncTokenOf(c *collection.Collection) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e := a.entryOfLocked(c); e != nil {
		return e.syncToken
	}
	return ""
}

// syncStatusOf 读该集合的同步状态快照。
func (a *App) syncStatusOf(c *collection.Collection) SyncStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e := a.entryOfLocked(c); e != nil {
		return e.syncStatus
	}
	return SyncStatus{}
}

// updateSyncStatus 更新该集合的同步状态（fn 在锁内被调用）。
func (a *App) updateSyncStatus(c *collection.Collection, fn func(st *SyncStatus)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e := a.entryOfLocked(c); e != nil {
		fn(&e.syncStatus)
	}
}

// ---------- 多根管理（导出方法，供前端 / 反射桥调用） ----------

// CollectionSummary 侧栏「根行」需要的概要：集合信息 + 该根的会话态。
type CollectionSummary struct {
	// Root 工作目录标识（规范化绝对路径；Windows 忽略大小写）。
	// 前端所有「按根」的调用与会话存储键都用它 —— 集合 uid 不能当身份（拷贝出来的
	// 两个目录 uid 相同）。
	Root string                     `json:"root"`
	Info *collection.CollectionInfo `json:"info"`
	// Active 是否为当前活动根（用户操作哪个根，它就是活动根）
	Active bool `json:"active"`
	// ReadOnly 同步镜像：该根下所有写操作都会被拒（界面据此置灰）
	ReadOnly bool `json:"readOnly"`
	// Linked 已关联服务端项目
	Linked bool `json:"linked"`
	// Mocking/MockPort 本地 Mock 正在服务该根时的状态
	Mocking  bool `json:"mocking"`
	MockPort int  `json:"mockPort,omitempty"`
}

// ListCollections 列出全部已打开的工作目录（按打开顺序），用于侧栏多根平铺。
func (a *App) ListCollections() ([]*CollectionSummary, error) {
	a.mu.Lock()
	items := make([]*CollectionSummary, 0, len(a.collOrder))
	roots := make([]string, 0, len(a.collOrder))
	dirs := make([]string, 0, len(a.collOrder))
	for _, root := range a.collOrder {
		e := a.colls[root]
		if e == nil || e.info == nil {
			continue
		}
		items = append(items, &CollectionSummary{
			Root: root, Info: e.info, Active: root == a.activeRoot,
		})
		roots = append(roots, root)
		dirs = append(dirs, e.coll.Dir)
	}
	mockRoot, mock := a.mockRoot, a.mock
	a.mu.Unlock()

	// 绑定状态要读 .sync.json（磁盘 IO）：放到锁外，失败按未绑定处理
	for i, dir := range dirs {
		if b, err := syncengine.LoadBind(dir); err == nil {
			items[i].Linked = b.Linked
			items[i].ReadOnly = b.Linked && b.Mode == syncengine.ModeMirror
		}
	}
	if mock != nil {
		st := mock.Status()
		for i, root := range roots {
			if root == mockRoot {
				items[i].Mocking = st.Running
				items[i].MockPort = st.Port
			}
		}
	}
	return items, nil
}

// ActiveCollection 当前活动根的标识（空串 = 未打开任何目录）。
func (a *App) ActiveCollection() string { return a.activeRootID() }

// SetActiveCollection 切换活动根（前端在用户点击某个根时调用）。root 传 ListCollections 里的 Root。
//
// 为什么需要显式切换：集合级方法（读请求、保存、发送、环境、同步…）都作用于活动根，
// 这样 60 多个既有方法的签名与前端绑定都不用动；跨根动作走带 root 参数的新方法。
func (a *App) SetActiveCollection(root string) error {
	if strings.TrimSpace(root) == "" {
		return errors.New("工作目录标识为空")
	}
	key := rootKey(root)
	a.mu.Lock()
	if _, ok := a.colls[key]; !ok {
		a.mu.Unlock()
		return errors.New("该工作目录未打开")
	}
	a.activeRoot = key
	mcpOn := a.settings.MCP.Enabled
	a.mu.Unlock()
	a.afterActiveChange(mcpOn)
	return nil
}

// afterActiveChange 活动根变化后的收尾：内嵌 MCP 的项目根由「活动集合的父目录」推导，需要重建服务。
func (a *App) afterActiveChange(mcpOn bool) {
	if mcpOn {
		a.applyMCP("")
	}
}

// CloseCollection 关闭一个工作目录（其它根不受影响）：停监听、停定时同步、停属于它的 Mock，
// 并释放索引句柄；索引文件保留（下次打开可直接复用）。
func (a *App) CloseCollection(root string) error {
	if strings.TrimSpace(root) == "" {
		return errors.New("工作目录标识为空")
	}
	key := rootKey(root)
	a.mu.Lock()
	e, ok := a.colls[key]
	if !ok {
		a.mu.Unlock()
		return errors.New("该工作目录未打开")
	}
	delete(a.colls, key)
	a.collOrder = removeFromOrder(a.collOrder, key)
	if a.activeRoot == key {
		if n := len(a.collOrder); n > 0 {
			a.activeRoot = a.collOrder[n-1]
		} else {
			a.activeRoot = ""
		}
	}
	var mock *mocksrv.Server
	if a.mockRoot == key {
		mock, a.mock, a.mockRoot = a.mock, nil, ""
	}
	if e.syncStop != nil {
		close(e.syncStop)
		e.syncStop = nil
	}
	mcpOn := a.settings.MCP.Enabled
	a.mu.Unlock()

	// Close = 停监听 + 关索引句柄（索引文件本身保留，下次打开可直接复用）
	_ = e.coll.Close()
	if mock != nil {
		_ = mock.Stop()
	}
	if mcpOn {
		a.applyMCP("")
	}
	return nil
}

// ReloadCollectionOf 重载指定工作目录（外部改动 / git 操作后）；root 为空 = 活动根。
func (a *App) ReloadCollectionOf(root string) (*collection.CollectionInfo, error) {
	c, err := a.collOf(root)
	if err != nil {
		return nil, err
	}
	_ = c.RebuildIndex()
	return a.refreshInfo(c)
}

// closeAllCollections 退出前收尾：停掉所有根的监听、索引句柄、定时同步与 Mock。
func (a *App) closeAllCollections() {
	a.mu.Lock()
	colls := make([]*collection.Collection, 0, len(a.colls))
	for _, e := range a.colls {
		colls = append(colls, e.coll)
		if e.syncStop != nil {
			close(e.syncStop)
			e.syncStop = nil
		}
	}
	mock := a.mock
	a.mock, a.mockRoot = nil, ""
	a.colls = map[string]*collEntry{}
	a.collOrder = nil
	a.activeRoot = ""
	a.mu.Unlock()

	for _, c := range colls {
		_ = c.Close() // 停监听 + 关 sqlite 句柄（Windows 上不释放会让目录删不掉）
	}
	if mock != nil {
		_ = mock.Stop()
	}
}
