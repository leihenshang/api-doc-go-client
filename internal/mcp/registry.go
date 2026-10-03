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
}

// Skipped 不可用原因（空 = 可用）：缺 manifest、Bruno 集合、打开失败等。
// 导出以便测试与状态展示读取（内部逻辑仍用 skipped 字段）。
func (e *Entry) Skipped() string { return e.skipped }

// Registry 管理 -root 下的多个项目，按需打开。
//
// 懒加载的原因：App.OpenCollection 会为每个集合启动一个 fsnotify 监听 goroutine，
// 若启动就打开 root 下所有集合，常驻监听数会随项目总数增长；懒加载让它与
// 「真正被用到的项目数」成正比。
type Registry struct {
	root string

	mu      sync.RWMutex
	entries []*Entry                              // 扫描结果（按 Path 排序，顺序稳定）
	opened  map[string]ProjectApp                 // key = 绝对目录
	infos   map[string]*collection.CollectionInfo // key = 绝对目录（树与环境，随打开缓存）

	// newApp 构造项目级运行时（见 runner.go 的 ProjectApp）。为 nil 时项目无法打开。
	newApp NewAppFunc
}

// NewRegistry 扫描 root 下一级子目录，识别含 opencollection.yml 的项目。
// newApp 为 nil 时只能扫描/查看项目信息，打开项目会报错。
func NewRegistry(root string, newApp NewAppFunc) (*Registry, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("项目根目录不可用: %w", err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("%s 不是目录", abs)
	}
	r := &Registry{root: abs, opened: map[string]ProjectApp{}, infos: map[string]*collection.CollectionInfo{}, newApp: newApp}
	if err := r.scan(); err != nil {
		return nil, err
	}
	return r, nil
}

// Root 项目根目录（绝对路径）。
func (r *Registry) Root() string { return r.root }

// scan 重新扫描 root（启动时与 list_projects 的 refresh=true 共用）。
func (r *Registry) scan() error {
	items, err := os.ReadDir(r.root)
	if err != nil {
		return err
	}
	var entries []*Entry
	for _, it := range items {
		if !it.IsDir() {
			continue
		}
		abs, _ := filepath.Abs(filepath.Join(r.root, it.Name()))
		rel, _ := filepath.Rel(r.root, abs)
		e := &Entry{Project: Project{Name: it.Name(), Path: filepath.ToSlash(rel), Dir: abs}}
		switch {
		case collection.IsBrunoDir(abs):
			e.skipped = "这是 Bruno 集合目录，请先用客户端的「导入」功能转换"
		default:
			// 只有含 manifest 的目录才算项目；其余（比如随手放的一层壳）标为跳过并说明原因
			if _, err := os.Stat(filepath.Join(abs, "opencollection.yml")); err != nil {
				e.skipped = "缺少 opencollection.yml（不是 api-doc-go 集合）"
				break
			}
			c, err := collection.Open(abs)
			if err != nil {
				e.skipped = "集合打开失败: " + err.Error()
				break
			}
			e.Name, e.UID = c.Name, c.UID
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	r.mu.Lock()
	r.entries = entries
	for dir := range r.opened {
		keep := false
		for _, e := range entries {
			if e.Dir == dir {
				keep = true
				break
			}
		}
		if !keep {
			// 目录已被删/改名：丢弃缓存引用（其文件监听 goroutine 随进程结束而退出）
			delete(r.opened, dir)
			delete(r.infos, dir)
		}
	}
	r.mu.Unlock()
	return nil
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
	absKey := key
	if !filepath.IsAbs(absKey) {
		absKey = filepath.Join(r.root, filepath.FromSlash(key))
	}
	absKey, _ = filepath.Abs(absKey)
	var byUID, byName, byPath []*Entry
	for _, e := range r.entries {
		switch {
		case e.UID != "" && e.UID == key:
			byUID = append(byUID, e)
		case e.Name == key:
			byName = append(byName, e)
		case e.Path == key || e.Dir == absKey || e.Dir == key:
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

// NewAppFactory 返回构造项目级运行时的工厂（CreateProject 指定别的根时要复用同一个工厂）。
func (r *Registry) NewAppFactory() NewAppFunc { return r.newApp }

// App 打开（或复用）指定项目，返回门面与项目条目。
func (r *Registry) App(key string) (ProjectApp, *Entry, error) {
	e, err := r.Find(key)
	if err != nil {
		return nil, nil, err
	}
	if e.skipped != "" {
		return nil, nil, fmt.Errorf("项目 %q 不可用：%s", e.Path, e.skipped)
	}
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

// CreateProject 在 root 下新建一个项目（集合目录 + manifest）。
//
// 复用集合层的 Open 语义：目录不存在会 MkdirAll 并在写首个请求时生成 manifest；
// 这里额外要求调用方给 name（既是目录名也是 manifest 的 info.name），避免出现无名集合。
func (r *Registry) CreateProject(name, dirName string) (Project, error) {
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
	abs, _ := filepath.Abs(filepath.Join(r.root, dirName))
	rel, _ := filepath.Rel(r.root, abs)
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
	if err := c.EnsureManifest(); err != nil {
		return Project{}, err
	}
	if err := r.Reload(); err != nil {
		return Project{}, err
	}
	e, err := r.Find(rel)
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
