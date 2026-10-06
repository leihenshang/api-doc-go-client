// Package mcp 把 api-doc-go 集合（项目）的查询与写入能力暴露给 MCP 客户端（AI 工具）。
//
// 分层约定：service/registry/fuzzy/render 不 import MCP SDK，只有 tools.go/server.go 依赖 SDK。
// 这样业务逻辑能脱离协议层单测，也便于将来复用到 CLI。
package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"api-doc-go-client/internal/collection"
	share "github.com/leihenshang/api-doc-go-share/collection"
)

// Project 一个项目 = 一个集合目录（含 opencollection.yml）。
type Project struct {
	Name string `json:"name"` // 集合名（manifest 的 info.name，缺省用目录名）
	UID  string `json:"uid"`  // 集合 uid
	Path string `json:"path"` // 相对 root 的路径（工具入参用它最直观）
	Dir  string `json:"dir"`  // 绝对路径

	// 以下统计在项目首次打开后填充（懒加载，见 Registry）
	Requests int      `json:"requests"` // 请求数
	Folders  int      `json:"folders"`  // 模块（分组）数
	Envs     []string `json:"envs"`     // 环境名列表
	Default  string   `json:"defaultEnv,omitempty"`
}

// Entry registry 扫描到的一项（可能尚未打开，也可能被跳过）。
type Entry struct {
	Project
	skipped string // 非空表示不可用（Bruno 集合 / 缺 manifest / 打开失败），原因展示给 AI
	// allowRoot 覆盖该项目的工作目录（白名单条目），writable 决定能否写
	allowRoot string
	writable  bool
}

// AllowRoot 该项目所在的工作目录（白名单里的那一条）；Writable 是否允许写。
func (e *Entry) AllowRoot() string { return e.allowRoot }

func (e *Entry) Writable() bool { return e.writable }

// Skipped 不可用原因（空 = 可用）：缺 manifest、Bruno 集合、打开失败等。
// 导出以便测试与状态展示读取（内部逻辑仍用 skipped 字段）。
func (e *Entry) Skipped() string { return e.skipped }

// Registry 管理 -root 下的多个项目，按需打开。
//
// 懒加载的原因：App.OpenCollection 会为每个集合启动一个 fsnotify 监听 goroutine，
// 若启动就打开 root 下所有集合，常驻监听数会随项目总数增长；懒加载让它与
// 「真正被用到的项目数」成正比。
type Registry struct {
	// allow 授权的工作目录（白名单）：项目只能来自这些目录，写操作还要求该项 writable
	allow []AllowDir
	// bad 配置了但不可用的目录（不存在/不是目录）：启动日志与 list_workspaces 展示，便于排查
	bad  []string
	root string

	mu      sync.RWMutex
	entries []*Entry                              // 扫描结果（按 Path 排序，顺序稳定）
	opened  map[string]ProjectApp                 // key = 绝对目录
	infos   map[string]*collection.CollectionInfo // key = 绝对目录（树与环境，随打开缓存）

	// newApp 构造项目级运行时（见 runner.go 的 ProjectApp）。为 nil 时项目无法打开。
	newApp NewAppFunc
}

// NewRegistry 扫描 root 下一级子目录，识别含 opencollection.yml 的项目。
//
// newApp 只在测试里需要替换（注入假运行时）；为 nil 时用真实的 app.NewApp。
// 这里兜底而不是要求每个调用方都传：漏传过一次（独立 mcpserver 二进制就曾因此
// 所有「打开项目」的工具报「未提供运行时工厂」），代价是整个二进制不可用。
// NewRegistry 单根便捷构造：等价于「把这一个目录按可写授权」。
//
// 保留它是为了兼容 -root 场景与既有测试：显式给了根目录，就等于用户授权了它。
// 需要只读/多目录授权时用 NewRegistryAllow。
func NewRegistry(root string, newApp NewAppFunc) (*Registry, error) {
	return NewRegistryAllow([]AllowDir{{Path: root, Writable: true}}, newApp)
}

// NewRegistryAllow 按授权的工作目录建表（空列表 = 一个目录都不授权，工具会如实回报）。
func NewRegistryAllow(allow []AllowDir, newApp NewAppFunc) (*Registry, error) {
	if newApp == nil {
		newApp = defaultNewApp
	}
	r := &Registry{
		allow:  NormalizeAllow(allow),
		opened: map[string]ProjectApp{},
		infos:  map[string]*collection.CollectionInfo{},
		newApp: newApp,
	}
	if len(r.allow) > 0 {
		r.root = r.allow[0].Path // 展示用：取第一个授权目录
	}
	if err := r.scan(); err != nil {
		return nil, err
	}
	return r, nil
}

// Root 展示用的根目录（第一个授权目录；无授权时为空串）。
func (r *Registry) Root() string { return r.root }

// Allow 当前的授权工作目录（规范化后的副本）。
func (r *Registry) Allow() []AllowDir {
	out := make([]AllowDir, len(r.allow))
	copy(out, r.allow)
	return out
}

// BadDirs 配置了但不可用的授权目录（不存在等）。
func (r *Registry) BadDirs() []string {
	out := make([]string, len(r.bad))
	copy(out, r.bad)
	return out
}

// scan 重新扫描全部授权目录（启动时与 list_projects 的 refresh=true 共用）。
//
// 每个授权目录支持两种形态：
//   - 目录自己就是集合（含 opencollection.yml）：算一个项目，Path = 目录基名；
//   - 否则扫它的一级子目录里含 opencollection.yml 的：Path = 授权目录基名/子目录名
//     （多个授权目录下可能有同名子目录，带前缀才不歧义）。
func (r *Registry) scan() error {
	var entries []*Entry
	var bad []string
	// 多个授权目录时才给 Path 加目录前缀：单根场景保持「子目录名」的既有写法（文档与用例都按它写）
	multi := len(r.allow) > 1
	for _, a := range r.allow {
		if !isDir(a.Path) {
			bad = append(bad, a.Path)
			continue
		}
		base := filepath.Base(a.Path)
		if _, err := os.Stat(filepath.Join(a.Path, "opencollection.yml")); err == nil {
			entries = append(entries, entryFor(a, a.Path, base))
			continue
		}
		items, err := os.ReadDir(a.Path)
		if err != nil {
			bad = append(bad, a.Path)
			continue
		}
		for _, it := range items {
			if !it.IsDir() {
				continue
			}
			abs := filepath.Join(a.Path, it.Name())
			path := it.Name()
			if multi {
				path = filepath.ToSlash(filepath.Join(base, it.Name()))
			}
			entries = append(entries, entryFor(a, abs, path))
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	r.mu.Lock()
	r.entries = entries
	r.bad = bad
	for dir := range r.opened {
		keep := false
		for _, e := range entries {
			if e.Dir == dir {
				keep = true
				break
			}
		}
		if !keep {
			// 目录已被删/改名/移出白名单：丢弃缓存引用（其文件监听 goroutine 随进程结束而退出）
			delete(r.opened, dir)
			delete(r.infos, dir)
		}
	}
	r.mu.Unlock()
	return nil
}

// entryFor 读一个候选目录的清单信息（只读）。
//
// 扫描刻意只读清单（ReadMeta）：不能对每个项目建索引、写清单、起 fsnotify 监听 ——
// 之前走 collection.Open 会为每个项目留下永不释放的监听句柄。
func entryFor(a AllowDir, abs, path string) *Entry {
	e := &Entry{
		Project:   Project{Name: filepath.Base(abs), Path: path, Dir: abs},
		allowRoot: a.Path,
		writable:  a.Writable,
	}
	switch {
	case collection.IsBrunoDir(abs):
		e.skipped = "这是 Bruno 集合目录，请先用客户端的「导入」功能转换"
	default:
		if _, statErr := os.Stat(filepath.Join(abs, "opencollection.yml")); statErr != nil {
			e.skipped = "缺少 opencollection.yml（不是 api-doc-go 集合）"
			return e
		}
		meta, err := collection.ReadMeta(abs)
		if err != nil {
			e.skipped = "集合清单不可用: " + err.Error()
			return e
		}
		e.Name, e.UID = meta.Name, meta.UID
	}
	return e
}

// allowFor 找覆盖 path 的授权条目；needWrite 时要求可写。
func (r *Registry) allowFor(path string, needWrite bool) (*AllowDir, error) {
	return allowedFor(r.allow, path, needWrite)
}

// Reload 强制重新扫描（新增/删除项目目录后调用）。
func (r *Registry) Reload() error { return r.scan() }

// Entries 返回扫描结果副本（未打开的项目统计为空）。
func (r *Registry) Entries() []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Entry, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, *e)
	}
	return out
}

// Find 按 uid → 目录基名 → 路径（绝对或相对 root）三种方式定位项目；歧义时报错并列候选。
func (r *Registry) Find(key string) (*Entry, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, fmt.Errorf("缺少 project 参数（用 list_projects 查看可用项目）")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	// 相对路径按每个授权目录各拼一次（多根时 Path 形如 <授权目录基名>/<子目录名>）
	absKeys := []string{key}
	if filepath.IsAbs(key) {
		if a, err := filepath.Abs(key); err == nil {
			absKeys = append(absKeys, a)
		}
	} else {
		for _, al := range r.allow {
			if p, err := filepath.Abs(filepath.Join(al.Path, filepath.FromSlash(key))); err == nil {
				absKeys = append(absKeys, p)
			}
		}
	}
	dirHit := func(dir string) bool {
		for _, k := range absKeys {
			if dir == k {
				return true
			}
		}
		return false
	}
	var byUID, byName, byPath []*Entry
	for _, e := range r.entries {
		switch {
		case e.UID != "" && e.UID == key:
			byUID = append(byUID, e)
		case e.Name == key:
			byName = append(byName, e)
		case e.Path == key || dirHit(e.Dir):
			byPath = append(byPath, e)
		}
	}
	for _, group := range [][]*Entry{byUID, byName, byPath} {
		if len(group) == 1 {
			return group[0], nil
		}
		if len(group) > 1 {
			names := make([]string, 0, len(group))
			for _, e := range group {
				names = append(names, e.Path)
			}
			return nil, fmt.Errorf("项目标识 %q 匹配到多个项目：%s；请改用 path 或 uid", key, strings.Join(names, ", "))
		}
	}
	return nil, fmt.Errorf("找不到项目 %q（用 list_projects 查看可用项目）", key)
}

// NewAppFactory 返回构造项目级运行时的工厂。
func (r *Registry) NewAppFactory() NewAppFunc { return r.newApp }

// App 打开（或复用）指定项目（只读操作可用）。
func (r *Registry) App(key string) (ProjectApp, *Entry, error) {
	e, err := r.Find(key)
	if err != nil {
		return nil, nil, err
	}
	if e.skipped != "" {
		return nil, nil, fmt.Errorf("项目 %q 不可用：%s", e.Path, e.skipped)
	}
	return r.openEntry(e)
}

// WriteApp 与 App 相同，但要求该项目的授权目录是**可写**的（写工具统一走它）。
func (r *Registry) WriteApp(key string) (ProjectApp, *Entry, error) {
	e, err := r.Find(key)
	if err != nil {
		return nil, nil, err
	}
	if e.skipped != "" {
		return nil, nil, fmt.Errorf("项目 %q 不可用：%s", e.Path, e.skipped)
	}
	if !e.writable {
		return nil, nil, fmt.Errorf("工作目录 %s 是只读授权（writable=false），不能新建/修改/删除内容", e.allowRoot)
	}
	return r.openEntry(e)
}

// openEntry 打开（或复用）条目对应的集合。
func (r *Registry) openEntry(e *Entry) (ProjectApp, *Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a, ok := r.opened[e.Dir]; ok {
		return a, e, nil
	}
	if r.newApp == nil {
		return nil, nil, fmt.Errorf("项目 %q 无法打开：未提供运行时工厂（newApp）", e.Path)
	}
	a := r.newApp()
	info, err := a.OpenCollection(e.Dir)
	if err != nil {
		return nil, nil, err
	}
	r.opened[e.Dir] = a
	r.infos[e.Dir] = info
	// 统计随首次打开一起填充，list_projects 直接读缓存
	e.UID, e.Name = info.UID, info.Name
	e.Envs, e.Default = envNames(info), ""
	if e.Default == "" && len(e.Envs) > 0 {
		e.Default = e.Envs[0]
	}
	countTree(info.Tree, &e.Requests, &e.Folders)
	return a, e, nil
}

// ensureOpen 用于只读操作：只拿到已打开的实例，未打开时返回 nil。
func (r *Registry) ensureOpen(key string) (ProjectApp, *Entry, error) {
	e, err := r.Find(key)
	if err != nil {
		return nil, nil, err
	}
	r.mu.RLock()
	a, ok := r.opened[e.Dir]
	r.mu.RUnlock()
	if !ok {
		return nil, nil, fmt.Errorf("项目 %q 尚未打开（该操作要求项目已加载）", e.Path)
	}
	return a, e, nil
}

// Info 返回已打开项目的集合概要（树 + 环境）；项目未打开时返回错误。
func (r *Registry) Info(key string) (*collection.CollectionInfo, *Entry, error) {
	_, e, err := r.ensureOpen(key)
	if err != nil {
		return nil, nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.infos[e.Dir], e, nil
}

// CreateProject 在**第一个可写的授权目录**下新建一个项目（集合目录 + manifest）。
//
// 目标目录不再由调用方随便给：以前 create_project 带 root 参数就能在任意路径下建集合（越权）。
// 需要指定具体目录时走 CreateProjectIn（同样要求在白名单内且可写）。
func (r *Registry) CreateProject(name, dirName string) (Project, error) {
	var target *AllowDir
	for i := range r.allow {
		if r.allow[i].Writable {
			target = &r.allow[i]
			break
		}
	}
	if target == nil {
		return Project{}, fmt.Errorf("没有可写的授权工作目录：请在客户端设置里添加目录并勾选可写（只读目录不能新建项目）")
	}
	return r.CreateProjectIn(target.Path, name, dirName)
}

// CreateProjectIn 在指定授权目录（必须存在且可写）下新建项目。
//
// 复用集合层的 Open 语义：目录不存在会 MkdirAll 并在写首个请求时生成 manifest；
// 这里额外要求调用方给 name（既是目录名也是 manifest 的 info.name），避免出现无名集合。
func (r *Registry) CreateProjectIn(base, name, dirName string) (Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Project{}, fmt.Errorf("缺少 name")
	}
	dirName = strings.TrimSpace(dirName)
	if dirName == "" {
		dirName = name
	}
	if !share.ValidEntryName(name) {
		return Project{}, fmt.Errorf("项目名含非法字符或为空: %q", name)
	}
	if !share.ValidEntryName(dirName) {
		return Project{}, fmt.Errorf("目录名含非法字符或为空: %q", dirName)
	}
	// 目标目录必须在白名单内且可写（双保险：dirName 已被 ValidEntryName 限制，不会带分隔符）
	if _, err := r.allowFor(base, true); err != nil {
		return Project{}, err
	}
	abs, _ := filepath.Abs(filepath.Join(base, dirName))
	if _, err := r.allowFor(abs, true); err != nil {
		return Project{}, err
	}
	rel, _ := filepath.Rel(base, abs)
	rel = filepath.ToSlash(rel)
	if _, err := os.Stat(filepath.Join(abs, "opencollection.yml")); err == nil {
		return Project{}, fmt.Errorf("项目已存在: %s", rel)
	}
	c, err := collection.Open(abs)
	if err != nil {
		return Project{}, err
	}
	// 允许项目名与目录名不同：目录名来自 dirName，manifest 的 info.name 用 name；
	// Open 只在目录不存在时建目录，这里提前落 manifest 让新项目立刻出现在列表里
	c.Name = name
	manifestErr := c.EnsureManifest()
	// 这里的实例只为「建目录 + 落清单」临时存在：索引与文件监听要立刻释放，
	// 否则每建一个项目就留下一个常驻 watcher（真正使用时由 Registry.App 再打开一份）。
	c.StopWatch()
	c.CloseIndex()
	if manifestErr != nil {
		return Project{}, manifestErr
	}
	if err := r.Reload(); err != nil {
		return Project{}, err
	}
	e, err := r.Find(abs) // 用绝对路径定位，避免多个授权目录下同名项目的歧义
	if err != nil {
		return Project{}, err
	}
	return e.Project, nil
}

func envNames(info *collection.CollectionInfo) []string {
	out := make([]string, 0, len(info.Envs))
	for _, e := range info.Envs {
		out = append(out, e.Name)
	}
	return out
}

func countTree(nodes []*collection.Node, reqs, folders *int) {
	for _, n := range nodes {
		if n == nil {
			continue
		}
		switch n.Type {
		case "request":
			*reqs++
		case "folder":
			*folders++
		}
		countTree(n.Children, reqs, folders)
	}
}
