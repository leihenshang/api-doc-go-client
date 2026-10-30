package mcp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/index"
)

// 工具层的常见错误（AI 读得懂的中文提示）。
var (
	errEmptyQuery      = coded(CodeInvalid, errors.New("缺少 query：请给出要搜索的关键词"))
	errNoProjectLoaded = coded(CodeInvalid, errors.New("当前没有已加载的项目：先用 list_workspaces 查看可用工作目录并 use_workspace 选定，或用 list_projects 打开一个项目再搜"))
	errNeedName        = coded(CodeInvalid, errors.New("缺少 name：创建请求必须给名称"))
	errEmptyDraft      = coded(CodeInvalid, errors.New("没有可用的请求内容"))
	errNeedUID         = coded(CodeInvalid, errors.New("缺少 uid：请用 search_requests 或 get_project_modules 找到目标请求"))
	errRespDocGRPC     = coded(CodeInvalid, errors.New("gRPC 请求不承载响应字段文档（respDoc）：与客户端 UpdateResponseDoc 的限制一致"))
)

// conflictError ifMatch 冲突的统一报错：明确「未写入任何内容」并附当前 hash，
// AI 可以立刻带着新 hash 重新读取/重试，而不是猜「到底是报错还是静默覆盖」。
func conflictError(kind, uid, hash string) error {
	if hash == "" {
		return coded(CodeConflict, fmt.Errorf("ifMatch 不匹配：%s %s 已被外部修改，本次未写入任何内容。请重新读取后合并再试；不传 ifMatch 可强制覆盖", kind, uid))
	}
	return coded(CodeConflict, fmt.Errorf("ifMatch 不匹配：%s %s 已被外部修改，本次未写入任何内容。当前 hash=%s，请重新读取后合并再试；不传 ifMatch 可强制覆盖", kind, uid, hash))
}

// Service 是 MCP 工具的语义实现（不依赖 SDK，便于单测与将来复用到 CLI）。
//
// 注意「默认工作目录」**不在这里**：use_workspace 选定后「本会话可省略 project」是**会话级**状态，
// 由工具层按会话 id 保存（见 tools.go 的 toolSet.sel）。Service 只认显式传进来的 project，
// 于是它保持无状态、可直接单测，也不会让多个 MCP 客户端（HTTP 下的多个 Mcp-Session-Id）
// 互相改掉对方选定的目录。
type Service struct {
	reg *Registry

	// 候选清单的索引刷新节流（项目 → 上次刷新时间）：候选只在错误路径出现，
	// 连续 not found 时不必每次全树重建；但完全不刷新会让刚创建、fsnotify
	// 还没回写的条目缺候选，所以保留一次节流刷新。
	candMu sync.Mutex
	candAt map[string]time.Time
}

// NewService 构造服务层。
func NewService(reg *Registry) *Service {
	return &Service{reg: reg, candAt: map[string]time.Time{}}
}

// ResetCandidateCache 写盘成功后调用：下次列候选必须重新取索引。
func (s *Service) ResetCandidateCache() {
	s.candMu.Lock()
	s.candAt = map[string]time.Time{}
	s.candMu.Unlock()
}

// reloadForCandidates 为候选清单刷新索引（同一项目 2 秒内只刷一次）。
func (s *Service) reloadForCandidates(a ProjectApp) {
	key := fmt.Sprintf("%p", a)
	now := time.Now()
	s.candMu.Lock()
	if last, ok := s.candAt[key]; ok && now.Sub(last) < 2*time.Second {
		s.candMu.Unlock()
		return
	}
	s.candAt[key] = now
	s.candMu.Unlock()
	_, _ = a.ReloadCollection()
}

// Reg 暴露底层 Registry（测试需要直接打开项目以填充统计；工具层不需要）。
func (s *Service) Reg() *Registry { return s.reg }

// resolve 解析项目：project 为空时用 use_workspace 选定的默认目标。
func (s *Service) resolve(project string) (ProjectApp, *Entry, error) {
	key, err := s.resolveKey(project)
	if err != nil {
		return nil, nil, withInvalid(err)
	}
	a, e, err := s.reg.App(key)
	if err != nil {
		return nil, nil, classifyAccessError(err)
	}
	return a, e, nil
}

// resolveWrite 写操作的解析：额外要求目标工作目录是**可写**授权。
func (s *Service) resolveWrite(project string) (ProjectApp, *Entry, error) {
	key, err := s.resolveKey(project)
	if err != nil {
		return nil, nil, withInvalid(err)
	}
	a, e, err := s.reg.WriteApp(key)
	if err != nil {
		return nil, nil, classifyAccessError(err)
	}
	return a, e, nil
}

// classifyAccessError 把项目解析错误映射成稳定错误码：
//   - 不在授权白名单 → AUTH
//   - 只读授权 → READONLY
//   - 找不到项目 / 项目不可用 / 尚未打开 → NOT_FOUND
//   - 打开失败（清单损坏、IO）→ IO
func classifyAccessError(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "不在 MCP 授权"):
		return coded(CodeForbidden, err)
	case strings.Contains(msg, "只读授权"):
		return coded(CodeReadOnly, err)
	case strings.Contains(msg, "无法打开"):
		return coded(CodeIO, err)
	default:
		return coded(CodeNotFound, err)
	}
}

// resolveKey 把 project 解析成项目标识；空 = 没指定（报错并给出可用项目提示）。
func (s *Service) resolveKey(project string) (string, error) {
	if k := strings.TrimSpace(project); k != "" {
		return k, nil
	}
	// 为什么把「会话/重启会失效」写进报错：默认目标按会话保存（见 tools.go 的 toolSet.sel），
	// 客户端重连或 MCP 服务重启后就是新的会话，之前选定的目录自然不在了 —— 不说明的话
	// 调用方只会觉得「刚才还好好的，怎么突然没指定 project」。
	return "", fmt.Errorf("未指定 project：请先 use_workspace 选定默认工作目录（选定后本会话可省略 project）。"+
		"默认目标按会话保存，客户端重连或 MCP 服务重启后需重新选定；不想依赖它也可以每次显式传 project。%s",
		s.workspaceHint())
}

// workspaceHint 可用项目提示（拼进报错里，AI 能照着改）。
func (s *Service) workspaceHint() string {
	entries := s.reg.Entries()
	usable := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.skipped == "" {
			usable = append(usable, fmt.Sprintf("%s（%s）", e.Name, e.Path))
		}
	}
	if len(usable) > 0 {
		return "可用项目：" + strings.Join(usable, "、")
	}
	if bad := s.reg.BadDirs(); len(bad) > 0 {
		return "当前没有可用项目：授权目录不可用 " + strings.Join(bad, "、")
	}
	return "当前没有可用项目：客户端设置里的 MCP 工作目录白名单可能为空（安全默认 = 不授权任何目录）"
}

// WorkspaceItem list_workspaces 的输出项：一个授权工作目录及其中的项目。
type WorkspaceItem struct {
	// Path 工作目录绝对路径（可直接传给 use_workspace）
	Path string `json:"path"`
	// Name 目录基名（便于口头指代）
	Name string `json:"name"`
	// Writable 是否可写授权（false = 只读：写工具会被拒）
	Writable bool `json:"writable"`
	// Projects 该目录下的项目标识（可取 name/path/uid，直接当 project 传）
	Projects []string `json:"projects,omitempty"`
	// Selected 当前默认工作目录是否落在它里面
	Selected bool `json:"selected,omitempty"`
	// Error 目录不可用时的原因（不存在等）
	Error string `json:"error,omitempty"`
}

// ListWorkspaces 列出授权的工作目录与其中的项目。
// selected = 本次调用所属**会话**选定的默认目标（由工具层给出；单个 Service 不持有它）。
func (s *Service) ListWorkspaces(selected string) []WorkspaceItem {
	entries := s.reg.Entries()
	out := make([]WorkspaceItem, 0, len(s.reg.Allow()))
	for _, a := range s.reg.Allow() {
		it := WorkspaceItem{Path: a.Path, Name: filepath.Base(a.Path), Writable: a.Writable}
		if !isDir(a.Path) {
			it.Error = "目录不存在或不是目录"
			out = append(out, it)
			continue
		}
		for _, e := range entries {
			if e.skipped != "" || e.AllowRoot() != a.Path {
				continue
			}
			it.Projects = append(it.Projects, e.Path)
			// 默认目标命中：项目自己、或它的授权目录
			if selected == e.Path || selected == a.Path || selected == e.Dir || selected == e.UID {
				it.Selected = true
			}
		}
		out = append(out, it)
	}
	return out
}

// UseWorkspace 解析「工作目录路径」或「项目标识（name/path/uid）」，两者都必须在白名单内。
//
// 返回：该项工作目录（Selected 已标记）+ **要记住的项目标识**（第二个返回值）。
// 谁来记、记多久由调用方决定 —— 工具层按会话 id 记，Service 不持有默认目标。
func (s *Service) UseWorkspace(workspace string) (WorkspaceItem, string, error) {
	key := strings.TrimSpace(workspace)
	if key == "" {
		return WorkspaceItem{}, "", fmt.Errorf("缺少 workspace：请传 list_workspaces 返回的工作目录 path，或一个项目标识")
	}
	// ① 先按工作目录解析（授权目录集合里有它才算数）
	if a, err := s.reg.allowFor(key, false); err == nil {
		projects := []string{}
		for _, e := range s.reg.Entries() {
			if e.skipped == "" && e.AllowRoot() == a.Path {
				projects = append(projects, e.Path)
			}
		}
		switch len(projects) {
		case 0:
			return WorkspaceItem{}, "", fmt.Errorf("工作目录 %s 下没有可用项目（用 list_workspaces 查看）", a.Path)
		case 1:
			return s.workspaceItem(projects[0], a), projects[0], nil
		default:
			return WorkspaceItem{}, "", fmt.Errorf("工作目录 %s 下有多个项目：%s；请直接指定其中之一", a.Path, strings.Join(projects, "、"))
		}
	}
	// ② 再按项目解析
	e, err := s.reg.Find(key)
	if err != nil {
		return WorkspaceItem{}, "", err
	}
	if e.skipped != "" {
		return WorkspaceItem{}, "", fmt.Errorf("项目 %q 不可用：%s", e.Path, e.skipped)
	}
	al, err := s.reg.allowFor(e.Dir, false)
	if err != nil {
		return WorkspaceItem{}, "", err
	}
	return s.workspaceItem(e.Path, al), e.Path, nil
}

// workspaceItem 组出「projectKey 所在工作目录」的输出项（Selected 按 projectKey 标记）。
func (s *Service) workspaceItem(projectKey string, a *AllowDir) WorkspaceItem {
	for _, it := range s.ListWorkspaces(projectKey) {
		if it.Path == a.Path {
			return it
		}
	}
	return WorkspaceItem{Path: a.Path, Name: filepath.Base(a.Path), Writable: a.Writable}
}

// 默认输出裁剪：响应体 8 KiB、列表 50 条。AI 上下文有限，超出必须显式标注而不是静默丢弃。
const (
	DefaultMaxBodyBytes = 8 << 10
	DefaultLimit        = 50
	MaxLimit            = 200
)

// enrichLimit 两阶段打分的「深读」上限：补 header/docs/body 时最多读这么多文件，
// 避免一次查询把整个集合的文件都读一遍。
const enrichLimit = 60

func clampLimit(limit int) int {
	switch {
	case limit <= 0:
		return DefaultLimit
	case limit > MaxLimit:
		return MaxLimit
	default:
		return limit
	}
}

// ProjectItem list_projects 的输出项。
type ProjectItem struct {
	Path       string   `json:"path"`
	Name       string   `json:"name"`
	UID        string   `json:"uid,omitempty"`
	Requests   int      `json:"requests"`
	Folders    int      `json:"folders"`
	Envs       []string `json:"envs,omitempty"`
	DefaultEnv string   `json:"defaultEnv,omitempty"`
	Skipped    string   `json:"skipped,omitempty"`
}

// Projects 返回当前扫描到的项目条目（入口日志与状态展示用）。
func (s *Service) Projects() []Entry { return s.reg.Entries() }

// ProjectCount 当前扫描到的项目数（状态快照/Refresh 后刷新计数用，免拷贝整个条目表）。
func (s *Service) ProjectCount() int { return len(s.reg.Entries()) }

// ListProjects 项目列表（query 模糊匹配项目名/路径；refresh=true 重新扫描目录）。
func (s *Service) ListProjects(query string, refresh bool) ([]ProjectItem, int, error) {
	if refresh {
		if err := s.reg.Reload(); err != nil {
			return nil, 0, err
		}
	}
	q := normalize(query)
	entries := s.reg.Entries()
	items := make([]ProjectItem, 0, len(entries))
	skipped := 0
	for _, e := range entries {
		if q != "" && !strings.Contains(normalize(e.Name), q) && !strings.Contains(normalize(e.Path), q) {
			continue
		}
		if e.skipped != "" {
			skipped++
		}
		items = append(items, ProjectItem{
			Path: e.Path, Name: e.Name, UID: e.UID,
			Requests: e.Requests, Folders: e.Folders,
			Envs: e.Envs, DefaultEnv: e.Default, Skipped: e.skipped,
		})
	}
	return items, skipped, nil
}

// ModuleItem get_project_modules 的输出项（一个模块/分组；根目录请求归到 "(根目录)"）。
type ModuleItem struct {
	Path     string `json:"path"` // 相对集合根；根目录为 ""
	Name     string `json:"name"`
	Requests int    `json:"requests"`
	Dirs     int    `json:"dirs"`
}

// ModulesResult get_project_modules 的返回。
type ModulesResult struct {
	Modules  []ModuleItem       `json:"modules"`
	Requests map[string][]Match `json:"requests,omitempty"` // 模块 → 请求（include_requests=true）
	Total    int                `json:"total"`              // 项目内请求总数
	Matched  int                `json:"matched"`            // 命中 query 的请求数
	// ModuleTotal 过滤前的模块总数（onlyMatched 收敛后，Modules 会变少；汇总行仍能说清「共几个」）。
	ModuleTotal int `json:"moduleTotal"`
}

// GetProjectModules 项目目录（模块）列表；query 模糊匹配模块名与模块下请求名/方法/路径。
// onlyMatched=true 且 query 非空时，只保留「模块名命中」或「其下有命中请求」的模块。
func (s *Service) GetProjectModules(project, query string, includeRequests, onlyMatched bool) (*ModulesResult, error) {
	// 集合的扫描/索引是打开时缓存的（桌面端每次写完都 Reload），这里同样先刷新再读树
	info, err := s.reload(project)
	if err != nil {
		return nil, err
	}
	q := normalize(query)
	res := &ModulesResult{Requests: map[string][]Match{}}
	var walk func(nodes []*collection.Node, prefix string)
	walk = func(nodes []*collection.Node, prefix string) {
		for _, n := range nodes {
			if n == nil {
				continue
			}
			switch n.Type {
			case "folder":
				child := n.Name
				if prefix != "" {
					child = prefix + "/" + n.Name
				}
				res.Modules = append(res.Modules, ModuleItem{Path: child, Name: n.Name})
				walk(n.Children, child)
			case "request":
				mod := prefix
				if mod == "" {
					mod = rootModule
				}
				res.Total++
				if q != "" && !strings.Contains(normalize(n.Name), q) &&
					!strings.Contains(normalize(n.Method), q) && !strings.Contains(normalize(n.Path), q) {
					continue
				}
				res.Matched++
				if includeRequests {
					res.Requests[mod] = append(res.Requests[mod], Match{
						UID: n.UID, Name: n.Name, Method: n.Method, Path: n.Path,
					})
				} else {
					bumpRequest(res.Modules, mod)
				}
			}
		}
	}
	walk(info.Tree, "")
	if includeRequests {
		for i := range res.Modules {
			res.Modules[i].Requests = len(res.Requests[res.Modules[i].Path])
		}
		for mod, list := range res.Requests {
			if !hasModule(res.Modules, mod) { // 根目录请求没有对应的模块行
				res.Modules = append(res.Modules, ModuleItem{Path: mod, Name: mod, Requests: len(list)})
			}
		}
	}
	for i := range res.Modules {
		res.Modules[i].Dirs = countDirs(info.Tree, res.Modules[i].Path)
	}
	res.ModuleTotal = len(res.Modules)
	// 收敛：query 非空时只留有命中的模块（模块名本身命中也算）；ModuleTotal 保留过滤前总数
	if onlyMatched && q != "" {
		kept := res.Modules[:0]
		for _, m := range res.Modules {
			if m.Requests > 0 || strings.Contains(normalize(m.Path), q) || strings.Contains(normalize(m.Name), q) {
				kept = append(kept, m)
			}
		}
		res.Modules = kept
	}
	if q != "" { // 有查询时把有命中的模块排前面
		sort.SliceStable(res.Modules, func(i, j int) bool { return res.Modules[i].Requests > res.Modules[j].Requests })
	}
	return res, nil
}

// rootModule 根目录请求在模块结果里的占位名。
const rootModule = "(根目录)"

func bumpRequest(mods []ModuleItem, path string) {
	for i := range mods {
		if mods[i].Path == path {
			mods[i].Requests++
		}
	}
}

func hasModule(mods []ModuleItem, path string) bool {
	for i := range mods {
		if mods[i].Path == path {
			return true
		}
	}
	return false
}

func countDirs(nodes []*collection.Node, path string) int {
	cur := nodes
	for _, seg := range strings.Split(path, "/") {
		var next []*collection.Node
		for _, n := range cur {
			if n.Type == "folder" && n.Name == seg {
				next = n.Children
			}
		}
		cur = next
	}
	n := 0
	for _, c := range cur {
		if c.Type == "folder" {
			n++
		}
	}
	return n
}

// reload 刷新项目（重扫集合 + 重建索引）并返回最新的集合概要。
//
// 为什么需要：collection 的扫描结果与索引都是「打开时快照」，写操作后不会自动更新
// （桌面端的做法也是写完调 ReloadCollection）。MCP 侧同理，且工具调用之间互相独立。
func (s *Service) reload(project string) (*collection.CollectionInfo, error) {
	return s.Reload(project)
}

// Reload 重建项目索引（文件监听之外的显式刷新；测试与「外部改动后重读」用）。
func (s *Service) Reload(project string) (*collection.CollectionInfo, error) {
	a, _, err := s.resolve(project)
	if err != nil {
		return nil, err
	}
	return a.ReloadCollection()
}

// candidatesFromIndex 把索引节点变成打分候选（不读文件正文，成本低）。
func candidatesFromIndex(nodes []index.Node) []candidate {
	out := make([]candidate, 0, len(nodes))
	for _, n := range nodes {
		if n.Type != "request" {
			continue
		}
		out = append(out, candidate{UID: n.UID, Name: n.Title, Method: n.Method, URL: n.URL, Path: n.Path, hash: n.Hash})
	}
	return out
}

// SearchRequests 跨项目（或单项目）模糊搜索请求。project 为空时搜所有已加载项目。
// opts.Exact=true 时只认整串出现（消掉分词/子序列误报）；opts.Fields 可限定搜索范围。
func (s *Service) SearchRequests(query, project string, limit int, opts SearchOptions) ([]Match, int, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, 0, errEmptyQuery
	}
	limit = clampLimit(limit)
	targets, err := s.searchTargets(project)
	if err != nil {
		return nil, 0, err
	}
	var all []Match
	total := 0
	for key, a := range targets {
		ms, n, err := s.searchIn(a, key, query, 0, opts)
		if err != nil {
			continue // 单个项目失败不影响整体
		}
		total += n
		all = append(all, ms...)
	}
	sortMatches(all)
	if len(all) > limit {
		all = all[:limit]
	}
	return all, total, nil
}

// searchTargets 决定搜索范围：指定项目则只搜它；否则搜所有「已加载」的项目
// （未加载的不会为了搜索而全部打开，那会为每个项目起一个文件监听）。
func (s *Service) searchTargets(project string) (map[string]ProjectApp, error) {
	if strings.TrimSpace(project) != "" {
		a, _, err := s.resolve(project)
		if err != nil {
			return nil, err
		}
		return map[string]ProjectApp{project: a}, nil
	}
	out := map[string]ProjectApp{}
	for _, e := range s.reg.Entries() {
		if e.skipped != "" {
			continue
		}
		if a, _, err := s.reg.ensureOpen(e.Path); err == nil {
			out[e.Path] = a
		}
	}
	if len(out) == 0 {
		return nil, errNoProjectLoaded
	}
	return out, nil
}

// searchIn 对一个项目做两阶段打分：先用索引字段（name/uri），
// 命中不足时再读候选文件补 header/docs/body/respDoc/bodyFields 后重新打分。
func (s *Service) searchIn(a ProjectApp, project, query string, limit int, opts SearchOptions) ([]Match, int, error) {
	if _, err := a.ReloadCollection(); err != nil { // 索引/树是缓存的，先刷新
		return nil, 0, err
	}
	nodes, err := a.SearchIndex("", 0) // 空查询 = 全量
	if err != nil {
		return nil, 0, err
	}
	var matches []Match
	for _, c := range candidatesFromIndex(nodes) {
		score, fields, snippet := scoreCandidate(c, query, opts)
		if score <= 0 {
			continue
		}
		matches = append(matches, Match{
			UID: c.UID, Name: c.Name, Method: c.Method, URL: c.URL, Path: c.Path,
			Project: project, Score: score, Matched: fields, Snippet: snippet, Hash: c.hash,
			WeakMatch: c.isWeakMatch(query, opts),
		})
	}
	total := len(matches)
	sortMatches(matches)
	// 索引字段（名称/URL/方法/路径）没命中时，query 可能命中 header / docs / body ——
	// 那就得读文件才有���料可打分，所以候选集取「索引里的前若干个请求」而不是空的命中列表。
	if total == 0 || len(matches) > enrichLimit {
		seeds := matches
		if len(seeds) == 0 {
			// 一个都没命中：拿索引前 N 个请求去深读（header/docs/body 命中只能靠读文件）
			pool := nodes
			if len(pool) > enrichLimit {
				pool = pool[:enrichLimit]
			}
			seeds = matchFromNodes(pool, project)
		} else if len(seeds) > enrichLimit {
			seeds = seeds[:enrichLimit]
		}
		s.enrichAndRescore(a, seeds, query, opts)
		// 深读只是补字段：读完后仍 0 分的候选说明其实没命中，不能当结果返回
		// （旧实现会把「索引前 N 个」原样吐出，看起来像一堆无关命中）。
		kept := seeds[:0]
		for _, m := range seeds {
			if m.Score > 0 {
				kept = append(kept, m)
			}
		}
		seeds = kept
		sortMatches(seeds)
		matches = seeds
		total = len(matches)
	}
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, total, nil
}

// matchFromNodes 把索引节点铺成待打分的 Match（分数先 0，等 enrich 后重排）。
// project/hash 要一起带上：深读路径产出的结果同样要能告诉 AI 「属于哪个项目、磁盘哈希是多少」。
func matchFromNodes(nodes []index.Node, project string) []Match {
	out := make([]Match, 0, len(nodes))
	for _, c := range candidatesFromIndex(nodes) {
		out = append(out, Match{
			UID: c.UID, Name: c.Name, Method: c.Method, URL: c.URL, Path: c.Path,
			Project: project, Hash: c.hash, Matched: []string{"name"},
		})
	}
	return out
}

// enrichAndRescore 读候选文件，补 header / docs / body / respDoc / bodyFields 字段后重打分（分数只升不降）。
func (s *Service) enrichAndRescore(a ProjectApp, matches []Match, query string, opts SearchOptions) {
	for i := range matches {
		r, err := a.ReadRequest(matches[i].UID)
		if err != nil {
			continue // 读不到就保留原分数
		}
		var sb strings.Builder
		for _, h := range r.Headers {
			sb.WriteString(h.Name)
			sb.WriteByte(' ')
			sb.WriteString(h.Value)
			sb.WriteByte(' ')
		}
		var fb strings.Builder
		for _, f := range r.Body.Fields {
			fmt.Fprintf(&fb, "%s %s %s\n", f.Name, f.Description, f.Example)
		}
		c := candidate{
			UID: r.UID, Name: r.Name, Method: r.Method, URL: r.URL, Path: r.Path,
			Header: sb.String(), Docs: r.Docs, Body: bodySummary(r),
			RespDoc: respDocSearchText(r.RespDoc), BodyFields: fb.String(),
		}
		if score, fields, snippet := scoreCandidate(c, query, opts); score > matches[i].Score {
			matches[i].Score, matches[i].Matched, matches[i].Snippet = score, fields, snippet
		}
	}
}

// respDocSearchText 把响应字段表与样例压成可搜索文本（只用于打分/片段，不进输出）。
func respDocSearchText(d *collection.ResponseDoc) string {
	if d == nil {
		return ""
	}
	var sb strings.Builder
	for _, p := range d.Params {
		fmt.Fprintf(&sb, "%s %s %s\n", p.FieldName, p.Description, p.Example)
	}
	sb.WriteString(d.Sample)
	return sb.String()
}

// sortMatches 分数降序；同分按名称升序，保证结果稳定可复现。
func sortMatches(ms []Match) {
	sort.SliceStable(ms, func(i, j int) bool {
		if ms[i].Score != ms[j].Score {
			return ms[i].Score > ms[j].Score
		}
		return ms[i].Name < ms[j].Name
	})
}

// RequestDetail get_request_detail 的返回。
type RequestDetail struct {
	Project string `json:"project"`
	UID     string `json:"uid"`
	// Hash 磁盘内容哈希：原样传给 update_request.if_match 即可获得冲突保护。
	Hash        string                      `json:"hash,omitempty"`
	Name        string                      `json:"name"`
	Method      string                      `json:"method"`
	URL         string                      `json:"url"`
	Path        string                      `json:"path"`
	Summary     string                      `json:"summary,omitempty"` // API 概述（纯文本一句话）
	Params      []collection.KV             `json:"params,omitempty"`
	Headers     []collection.KV             `json:"headers,omitempty"`
	Body        collection.Body             `json:"body"`
	Auth        *collection.Auth            `json:"auth,omitempty"`
	Settings    *collection.RequestSettings `json:"settings,omitempty"`
	GRPC        *collection.GrpcBlock       `json:"grpc,omitempty"`
	Docs        string                      `json:"docs,omitempty"`        // 请求自带说明（Markdown）
	RespDoc     *collection.ResponseDoc     `json:"respDoc,omitempty"`     // 响应字段文档（返回类型/字段表/样例；样例按 includeBody 裁剪）
	RelatedDocs []DocBrief                  `json:"relatedDocs,omitempty"` // 集合里与该请求相关的 docs/*.md 条目
	Examples    []ExampleItem               `json:"examples,omitempty"`    // 已保存的响应示例
}

// DocBrief 集合级文档条目的摘要。
type DocBrief struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// DetailOptions get_request_detail 的可选参数。
type DetailOptions struct {
	IncludeBody  bool // 是否返回响应示例/请求体的完整内容
	MaxBodyBytes int
	Query        string   // 在示例名/文档名里再筛一遍（可选）
	Sections     []string // 只取指定分段：overview/docs/bodyFields/respDoc/examples；空 = 全量
}

// GetRequestDetail 接口详情：请求（含 headers/body/auth/grpc）+ 说明 + 已保存响应示例。
func (s *Service) GetRequestDetail(project, uid string, opts DetailOptions) (*RequestDetail, error) {
	return s.GetRequestDetailAt(project, uid, "", opts)
}

// GetRequestDetailAt 同 GetRequestDetail，但支持按 path 定位（uid 与 path 二选一）。
func (s *Service) GetRequestDetailAt(project, uid, reqPath string, opts DetailOptions) (*RequestDetail, error) {
	// 先校验分段取值：未知值直接报错（免得静默返回全量让 AI 以为筛选生效了）
	if _, err := NormalizeDetailSections(opts.Sections); err != nil {
		return nil, err
	}
	a, e, err := s.resolve(project)
	if err != nil {
		return nil, err
	}
	uid, err = s.resolveRequestUID(a, uid, reqPath)
	if err != nil {
		return nil, err
	}
	r, err := a.ReadRequest(uid)
	if err != nil {
		// 找不到时附候选：AI 常拿「用户说的接口名/路径」，直接给清单比让它再搜一次更省事。
		return nil, withNotFound(fmt.Errorf("读取请求 %s 失败: %w；%s", uid, err, s.requestCandidates(a, reqPath)))
	}
	hash, _ := s.hashOf(a, r.UID)
	d := &RequestDetail{
		Project: e.Path, UID: r.UID, Hash: hash, Name: r.Name, Method: r.Method, URL: r.URL, Path: r.Path,
		Summary: r.Summary, Params: r.Params, Headers: r.Headers, Body: r.Body, Auth: r.Auth,
		Settings: r.Settings, GRPC: r.GRPC, Docs: r.Docs,
	}
	// 响应字段文档：字段表全量返回（AI 要「读了再改整张表」）；样例原文按 includeBody 裁剪，避免大体撞爆上下文
	if r.RespDoc != nil {
		rd := *r.RespDoc
		if opts.IncludeBody {
			rd.Sample, _, _ = Truncate(rd.Sample, opts.MaxBodyBytes)
		} else {
			rd.Sample, _, _ = Truncate(rd.Sample, DefaultMaxBodyBytes/4)
		}
		d.RespDoc = &rd
	}
	// 集合级文档：按 query 或请求名做模糊关联（docs 是集合级条目，没有直接外键）
	docs, _ := a.ListDocs()
	for _, doc := range docs {
		if matchDoc(doc.Name, doc.Path, r.Name, opts.Query) {
			d.RelatedDocs = append(d.RelatedDocs, DocBrief{UID: doc.UID, Name: doc.Name, Path: doc.Path})
		}
	}
	// 已保存的响应示例
	examples, err := a.ListResponseExamples(r.UID)
	if err == nil {
		q := normalize(opts.Query)
		for _, ex := range examples {
			if q != "" && !strings.Contains(normalize(ex.Name), q) {
				continue
			}
			item := ExampleItem{
				UID: ex.UID, Name: ex.Name, Path: ex.Path, Hash: a.FileHash(ex.Path), CreatedAt: ex.CreatedAt,
				Status: ex.Response.Status, TimeMS: ex.Response.TimeMS, BodyBytes: len(ex.Response.Body),
			}
			if opts.IncludeBody {
				body, truncated, _ := Truncate(ex.Response.Body, opts.MaxBodyBytes)
				item.WithBody, item.Truncated = body, truncated
			}
			d.Examples = append(d.Examples, item)
		}
	}
	if opts.IncludeBody {
		if body, truncated, _ := Truncate(bodyRaw(r.Body), opts.MaxBodyBytes); truncated {
			d.Body.Raw = body
		}
	}
	return d, nil
}

func bodyRaw(b collection.Body) string {
	if b.Type == "raw" {
		return b.Raw
	}
	return ""
}

// matchDoc 文档与请求的关联判断：显式 query 优先，否则用请求名做子串/子序列匹配。
func matchDoc(name, path, reqName, query string) bool {
	q := normalize(query)
	if q == "" {
		q = normalize(reqName)
	}
	if q == "" {
		return false
	}
	return strings.Contains(normalize(name), q) || strings.Contains(normalize(path), q) || isSubsequence(q, normalize(name))
}

// resolveRequestUID 把「uid 或 path」统一成 uid；两者都给时以 uid 为准。
//
// path 依次匹配：集合内相对路径（api/users.yml）、去掉 .yml 的路径（api/users）、
// 不含目录时的纯文件名（users.yml）。找不到/歧义时报错并列出候选 —— 用户常说
// 「改 /api/users 那个接口」，让调用方再搜一次不如直接给清单。
func (s *Service) resolveRequestUID(a ProjectApp, uid, reqPath string) (string, error) {
	if uid = strings.TrimSpace(uid); uid != "" {
		return uid, nil
	}
	p := strings.TrimSpace(filepath.ToSlash(reqPath))
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return "", coded(CodeInvalid, errors.New("缺少 uid 或 path：请用 search_requests 找到目标接口（两者给一个即可）"))
	}
	nodes, err := a.SearchIndex("", 0)
	if err != nil {
		return "", coded(CodeIO, fmt.Errorf("列不出请求: %w", err))
	}
	var exact, loose []index.Node
	for _, n := range nodes {
		if n.Type != "request" {
			continue
		}
		np := strings.TrimPrefix(filepath.ToSlash(n.Path), "/")
		switch {
		case np == p || strings.TrimSuffix(np, ".yml") == p:
			exact = append(exact, n)
		case !strings.Contains(p, "/") && filepath.Base(np) == p:
			loose = append(loose, n)
		}
	}
	hit := exact
	if len(hit) == 0 {
		hit = loose
	}
	switch len(hit) {
	case 1:
		return hit[0].UID, nil
	case 0:
		return "", withNotFound(fmt.Errorf("找不到请求 %q：%s", p, s.requestCandidates(a, p)))
	default:
		names := make([]string, 0, len(hit))
		for _, n := range hit {
			names = append(names, n.Path)
		}
		return "", coded(CodeConflict, fmt.Errorf("路径 %q 匹配到多个请求：%s；请传完整 path 或 uid", p, strings.Join(names, ", ")))
	}
}

// requestCandidates 项目内请求候选（前 5 个：方法 + 名称 + 路径 + uid）。
// 按与 query（调用方给的 path）的匹配强度排序：前缀/目录命中优先，其次包含，最后保持原顺序。
func (s *Service) requestCandidates(a ProjectApp, query string) string {
	const maxShown = 5
	// 节流刷新：刚建/改过的条目可能还没进索引（fsnotify 有延迟），但连续
	// not found 时每次都全树重建太贵。
	s.reloadForCandidates(a)
	nodes, err := a.SearchIndex("", 0)
	if err != nil {
		return "（候选清单读取失败，请用 search_requests 查找）"
	}
	reqs := make([]index.Node, 0, len(nodes))
	for _, n := range nodes {
		if n.Type == "request" {
			reqs = append(reqs, n)
		}
	}
	if len(reqs) == 0 {
		return "项目里还没有请求（可用 create_request 新建）"
	}
	rankNodes(reqs, query)
	parts := make([]string, 0, maxShown)
	for i, n := range reqs {
		if i >= maxShown {
			break
		}
		parts = append(parts, fmt.Sprintf("%s %s（%s，uid=%s）", n.Method, n.Title, n.Path, n.UID))
	}
	out := "候选：" + strings.Join(parts, "、")
	if len(reqs) > maxShown {
		out += fmt.Sprintf(" 等 %d 个", len(reqs))
	}
	return out
}

// rankNodes 按路径与 query 的匹配强度做稳定排序（原地）：目录/前缀命中优先，其次包含。
func rankNodes(nodes []index.Node, query string) {
	q := strings.ToLower(strings.TrimSpace(filepath.ToSlash(query)))
	if q == "" {
		return
	}
	score := func(p string) int {
		lp := strings.ToLower(filepath.ToSlash(p))
		switch {
		case strings.HasPrefix(lp, q), strings.Contains(lp, "/"+q):
			return 2
		case strings.Contains(lp, q):
			return 1
		default:
			return 0
		}
	}
	sort.SliceStable(nodes, func(i, j int) bool { return score(nodes[i].Path) > score(nodes[j].Path) })
}

// KVInput 参数 / 表单 / 头行的 MCP 入参（不直接复用 collection.KV）。
//
// 为什么不复用：collection.KV.Enabled 没有 omitempty，生成的 inputSchema 会把它标成
// required —— AI 想「加一行 header」也必须记得编造 enabled:true，漏了就 [INVALID]。
// 这里把 Enabled 改成指针（不传 = 启用）；Value/Type/Description 也用指针，
// 让 upsert 能区分「不传 = 保留原值」与「传空串 = 清空」。
type KVInput struct {
	Name        string  `json:"name" jsonschema:"行名（参数名 / header 名 / 表单字段名）"`
	Value       *string `json:"value,omitempty" jsonschema:"值；不传 = 保留原值（整表替换时按空串）"`
	Type        *string `json:"type,omitempty" jsonschema:"类型（如 multipart 的文件行）；不传 = 保留原值"`
	Description *string `json:"description,omitempty" jsonschema:"说明列；不传 = 保留原值"`
	Enabled     *bool   `json:"enabled,omitempty" jsonschema:"是否启用；不传 = true（不会因为漏传而报错）"`
}

// toKV 转成落盘类型：未传的 Enabled 按 true，未传的字符串按空串。
func (k KVInput) toKV() collection.KV {
	enabled := true
	if k.Enabled != nil {
		enabled = *k.Enabled
	}
	out := collection.KV{Name: k.Name, Enabled: enabled}
	if k.Value != nil {
		out.Value = *k.Value
	}
	if k.Type != nil {
		out.Type = *k.Type
	}
	if k.Description != nil {
		out.Description = *k.Description
	}
	return out
}

// kvList 整表替换用：逐行转成落盘类型（保持顺序）。
func kvList(in []KVInput) []collection.KV {
	if in == nil {
		return nil
	}
	out := make([]collection.KV, 0, len(in))
	for _, k := range in {
		out = append(out, k.toKV())
	}
	return out
}

// kvFromRows 反向投影（读侧/测试用：把落盘行转成入参行）。
func kvFromRows(rows []collection.KV) []KVInput {
	out := make([]KVInput, 0, len(rows))
	for _, r := range rows {
		v, t, d, en := r.Value, r.Type, r.Description, r.Enabled
		out = append(out, KVInput{Name: r.Name, Value: &v, Type: &t, Description: &d, Enabled: &en})
	}
	return out
}

// BodyFieldInput 请求体字段表的 MCP 入参：Required 指针（不传 = 非必填）。
type BodyFieldInput struct {
	Name        string  `json:"name" jsonschema:"字段名"`
	Type        *string `json:"type,omitempty" jsonschema:"字段类型（string/integer/boolean/object/array…）；不传 = string"`
	Required    *bool   `json:"required,omitempty" jsonschema:"是否必填；不传 = false（upsert 时不传 = 保留原值）"`
	Example     *string `json:"example,omitempty" jsonschema:"示例值；不传 = 保留原值"`
	Description *string `json:"description,omitempty" jsonschema:"说明；不传 = 保留原值"`
}

// toField 转成落盘类型：未传的 Type 按 string、Required 按 false。
func (f BodyFieldInput) toField() collection.BodyField {
	out := collection.BodyField{Name: f.Name}
	if f.Type != nil {
		out.Type = *f.Type
	}
	if f.Required != nil {
		out.Required = *f.Required
	}
	if f.Example != nil {
		out.Example = *f.Example
	}
	if f.Description != nil {
		out.Description = *f.Description
	}
	return out
}

// fieldList 整表替换用。
func fieldList(in []BodyFieldInput) []collection.BodyField {
	if in == nil {
		return nil
	}
	out := make([]collection.BodyField, 0, len(in))
	for _, f := range in {
		out = append(out, f.toField())
	}
	return out
}

// PatchInput 行级合并入参（update_request.patch / update_requests.items[].patch）。
// 同名只覆盖传入的子字段，没有则追加；与对应的整表字段互斥。
type PatchInput struct {
	// 三个子表都是「按名合并」：同名的只覆盖传入的子字段，没有的追加；
	// 显式传空数组 = 清空整表（与 params/headers/bodyFields 整表覆盖一致），不传 = 不动。
	Params     []KVInput        `json:"params,omitempty" jsonschema:"按 name 合并参数行；显式空数组 = 清空参数表，不传 = 不动"`
	Headers    []KVInput        `json:"headers,omitempty" jsonschema:"按 header 名合并；显式空数组 = 清空请求头，不传 = 不动"`
	BodyFields []BodyFieldInput `json:"bodyFields,omitempty" jsonschema:"按字段名合并；显式空数组 = 清空字段表，不传 = 不动"`
}

// BodyInput 请求体的**局部更新**入参（create_request / update_request 专用）。
//
// 为什么不直接复用 collection.Body：Body.Form 的 json tag 没有 omitempty，一旦出现在
// inputSchema 里就变成 required（type/raw/form 三件套）——AI 只想改 raw 也得编造 form，
// 漏传就把整张表单清空了。这里全部改成指针：nil = 保留原值，显式空串 / 空数组 / 空对象 = 清空。
// 该类型只活在入参侧，落盘格式（share 的 collection.Body）完全不变。
type BodyInput struct {
	Type   *string            `json:"type,omitempty" jsonschema:"请求体类型：none/json/text/form/multipart；不传 = 保留原值，传空串 = 清空"`
	Raw    *string            `json:"raw,omitempty" jsonschema:"请求体原文（json/text）；不传 = 保留原值，传空串 = 清空"`
	Data   *string            `json:"data,omitempty" jsonschema:"Bruno data 字段（往返用）；不传 = 保留"`
	Form   *[]KVInput         `json:"form,omitempty" jsonschema:"form/multipart 表单行；不传保留，空数组清空"`
	Descs  *map[string]string `json:"descs,omitempty" jsonschema:"JSON 体「字段路径 → 备注」；不传保留，空对象清空"`
	Fields *[]BodyFieldInput  `json:"fields,omitempty" jsonschema:"字段表格；不传保留，空数组清空"`
}

// merge 把入参合并到既有请求体上：nil 字段一律保留 base 的原值（显式零值才覆盖）。
func (in BodyInput) merge(base collection.Body) collection.Body {
	out := base
	if in.Type != nil {
		out.Type = *in.Type
	}
	if in.Raw != nil {
		out.Raw = *in.Raw
	}
	if in.Data != nil {
		out.Data = *in.Data
	}
	if in.Form != nil {
		out.Form = kvList(*in.Form)
	}
	if in.Descs != nil {
		out.Descs = *in.Descs
	}
	if in.Fields != nil {
		out.Fields = fieldList(*in.Fields)
	}
	return out
}

// RespDocInput 响应字段文档的**局部更新**入参（create_request / update_request 专用）。
//
// 与 BodyInput 同口径：nil = 保留原值；显式空串 / 0 / 空数组 = 清空。
// 相比旧的 mergeRespDoc（空串/0 一律视为「没传」，因此永远清不掉样例与状态码），
// 指针让「清空」成为一种可表达的意图。
type RespDocInput struct {
	Type        *string                     `json:"type,omitempty" jsonschema:"返回类型徽标（object/array/...）；不传 = 保留，传空串 = 清空"`
	Params      *[]collection.ResponseParam `json:"params,omitempty" jsonschema:"返回参数行；不传 = 保留，传空数组 = 清空"`
	Sample      *string                     `json:"sample,omitempty" jsonschema:"响应样例原文；不传 = 保留，传空串 = 清空"`
	Status      *int                        `json:"status,omitempty" jsonschema:"响应状态码快照；不传 = 保留，传 0 = 清零"`
	Headers     *[]KVInput                  `json:"headers,omitempty" jsonschema:"响应头快照；不传 = 保留，传空数组 = 清空"`
	ContentType *string                     `json:"contentType,omitempty" jsonschema:"响应媒体类型（json/text/xml/html/binary）；不传 = 保留，传空串 = 清空"`
}

// merge 把入参合并到既有响应文档上；base 为 nil 时按空文档起步（入参 nil 仍表示保留）。
func (in RespDocInput) merge(base *collection.ResponseDoc) *collection.ResponseDoc {
	out := collection.ResponseDoc{}
	if base != nil {
		out = *base
	}
	if in.Type != nil {
		out.Type = *in.Type
	}
	if in.Params != nil {
		out.Params = *in.Params
	}
	if in.Sample != nil {
		out.Sample = *in.Sample
	}
	if in.Status != nil {
		out.Status = *in.Status
	}
	if in.Headers != nil {
		out.Headers = kvList(*in.Headers)
	}
	if in.ContentType != nil {
		out.ContentType = *in.ContentType
	}
	return &out
}

// CreateRequestInput create_request 的入参。
type CreateRequestInput struct {
	Project     string                      `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	Folder      string                      `json:"folder,omitempty" jsonschema:"目标模块路径（如 api/user）；留空 = 项目根；不存在 [NOT_FOUND]"`
	Name        string                      `json:"name"`
	Method      string                      `json:"method,omitempty" jsonschema:"请求方法（GET/POST/PUT/PATCH/DELETE/HEAD/OPTIONS/TRACE/CONNECT/GRPC；其它值 [INVALID]）"`
	URL         string                      `json:"url,omitempty"`
	Headers     []KVInput                   `json:"headers,omitempty" jsonschema:"请求头（name/value/description/enabled）"`
	Params      []KVInput                   `json:"params,omitempty" jsonschema:"参数表；行可带 description 与 type（multipart 文件行）"`
	BodyType    string                      `json:"bodyType,omitempty" jsonschema:"请求体类型快捷写法（等价 body.type）；只覆盖 type"`
	BodyRaw     string                      `json:"bodyRaw,omitempty" jsonschema:"请求体原文快捷写法（等价 body.raw）；只覆盖 raw"`
	Body        *BodyInput                  `json:"body,omitempty" jsonschema:"请求体完整形态（form/multipart 用 form 行）；不传的子项保留原值"`
	BodyFields  []BodyFieldInput            `json:"bodyFields,omitempty" jsonschema:"字段表（名/类型/必填/示例/说明）；与 body 独立，传了只覆盖表格"`
	Summary     string                      `json:"summary,omitempty" jsonschema:"API 概述（纯文本一句话）"`
	Settings    *collection.RequestSettings `json:"settings,omitempty" jsonschema:"单请求设置（超时/重定向/忽略证书/URL 编码）"`
	RespDoc     *RespDocInput               `json:"respDoc,omitempty" jsonschema:"响应字段文档；按子项合并，只写 params 时其余不动；gRPC 不支持"`
	Auth        *collection.Auth            `json:"auth,omitempty"`
	Docs        string                      `json:"docs,omitempty" jsonschema:"接口说明（Markdown）；结构见 resource apidoc://doc-template"`
	Curl        string                      `json:"curl,omitempty" jsonschema:"用一条 curl 命令建请求（与手填字段互斥）"`
	CopyFromUID string                      `json:"copyFromUid,omitempty" jsonschema:"复制已有请求（uid 来自 search_requests）后叠加本次字段"`
	// UID 可选：指定 uid 做幂等导入（批量同步时固定身份）
	UID string `json:"uid,omitempty" jsonschema:"可选：指定 uid（批量导入做幂等）；已存在时按 onConflict 处理"`
	// OnConflict 同路径或同 uid 已存在时的策略（与 create_doc 一致，默认 error）
	OnConflict string `json:"onConflict,omitempty" jsonschema:"已存在时：error（默认，回 [CONFLICT] + 已有 uid/path）/ suffix（-2）/ overwrite（复用覆盖）"`
	// DryRun 只做校验与冲突判定，返回将创建的 uid/path，不落盘。
	DryRun bool `json:"dryRun,omitempty" jsonschema:"true = 只预演（返回将创建的 uid/path 与冲突判定），不写盘"`
	// FileName 可选：落盘文件名（不含 .yml）。留空 = 由 name slug 化推导 —— 中文 name
	// 会得到中文文件名（集合是纯文本、文件即真相源）；要 ASCII 名（脚本/工具链友好）就在这里给。
	FileName string `json:"fileName,omitempty" jsonschema:"落盘文件名（不含 .yml）；留空 = 由 name 推导；需要 ASCII 名时显式传"`
}

// CreateRequest 创建请求（三种来源：手填参数 / cURL 导入 / 复制已有请求）。
func (s *Service) CreateRequest(in CreateRequestInput) (*collection.Request, error) {
	a, _, err := s.resolveWrite(in.Project)
	if err != nil {
		return nil, err
	}
	draft, err := resolveCreateDraft(a, in)
	if err != nil {
		return nil, err
	}
	if err := applyCreateFields(draft, in); err != nil {
		return nil, err
	}
	method, merr := normalizeMethod(draft.Method)
	if merr != nil {
		return nil, merr
	}
	draft.Method = method
	// 草稿模板的默认 headers 会在没有 headers 时被 CreateRequestFromDraft 填上，这里不覆盖
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errNeedName
	}
	// fileName 只在显式给了才生效：默认仍由 name slug 化推导（中文名落中文文件是既有取舍）。
	opts := collection.RequestCreateOpts{
		Folder:     strings.TrimSpace(in.Folder),
		Name:       name,
		Src:        draft,
		UID:        strings.TrimSpace(in.UID),
		OnConflict: in.OnConflict,
		FileName:   strings.TrimSpace(in.FileName),
	}
	var r *collection.Request
	if in.DryRun {
		r, err = a.PlanCreateRequestWithOpts(opts)
	} else {
		r, err = a.CreateRequestWithOpts(opts)
	}
	if err != nil {
		switch {
		case errors.Is(err, collection.ErrRequestConflict):
			return nil, coded(CodeConflict, err)
		case strings.Contains(err.Error(), "onConflict"), strings.Contains(err.Error(), "名称含非法字符"),
			strings.Contains(err.Error(), "请求内容为空"):
			return nil, withInvalid(err)
		default:
			return nil, coded(CodeIO, err)
		}
	}
	return r, nil
}

// ImportCollectionInput import_collection 的入参。
type ImportCollectionInput struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	Format  string `json:"format" jsonschema:"postman / openapi / apidocgo（本客户端导出的整包）；Bruno 目录请用客户端导入"`
	Text    string `json:"text" jsonschema:"集合文件的正文内容（不是文件路径：请先读出内容再传）；上限 10MB"`
	Parent  string `json:"parent,omitempty" jsonschema:"落在哪个模块下（相对集合根，如 api/user）；留空 = 项目根"`
	DryRun  bool   `json:"dryRun,omitempty" jsonschema:"true = 只解析并回报将导入的条数，不写盘"`
}

// ImportCollection 批量导入外部集合：一次落多条请求，省掉逐个 create_request 的往返。
//
// 与客户端「导入」同源（collection.ImportPostman / ImportOpenAPI / ImportBundle），
// 只是入口从选文件变成传正文 —— MCP 侧拿不到本地文件选择对话框。
func (s *Service) ImportCollection(in ImportCollectionInput) (*collection.ImportSummary, error) {
	a, _, err := s.resolveWrite(in.Project)
	if err != nil {
		return nil, err
	}
	format := strings.ToLower(strings.TrimSpace(in.Format))
	text := strings.TrimSpace(in.Text)
	if format == "" {
		return nil, withInvalid(errors.New("缺少 format：postman / openapi / apidocgo"))
	}
	if text == "" {
		return nil, withInvalid(errors.New("缺少 text：请传集合文件的正文内容（不支持文件路径）"))
	}
	// 上限保护：整包正文一次性进内存并全量解析，超大输入会拖垮进程。
	if len(text) > maxImportTextBytes {
		return nil, withLimit(fmt.Errorf("text 超过 %d MB（本次约 %d MB）：请拆分后分批导入",
			maxImportTextBytes>>20, len(text)>>20))
	}
	if in.DryRun {
		sum, perr := a.ParseImport(format, text)
		if perr != nil {
			return nil, withInvalid(perr)
		}
		return sum, nil
	}
	sum, err := a.ImportCollection(strings.TrimSpace(in.Parent), format, text)
	if err != nil {
		// 格式不对 / 文件为空 / 版本过高由集合层包 ErrImportInvalid（调用方能修）；落盘失败才是 IO。
		if errors.Is(err, collection.ErrImportInvalid) {
			return nil, withInvalid(err)
		}
		return nil, coded(CodeIO, err)
	}
	return sum, nil
}

// ReadRequest 读接口原文（delete_request 的结构化输出要在删除前拿到 path/name）。
func (s *Service) ReadRequest(project, uid string) (*collection.Request, error) {
	return s.ReadRequestAt(project, uid, "")
}

// ReadRequestAt 同 ReadRequest，但支持按 path 定位（uid 与 path 二选一）。
func (s *Service) ReadRequestAt(project, uid, reqPath string) (*collection.Request, error) {
	a, _, err := s.resolve(project)
	if err != nil {
		return nil, err
	}
	uid, err = s.resolveRequestUID(a, uid, reqPath)
	if err != nil {
		return nil, err
	}
	r, err := a.ReadRequest(uid)
	if err != nil {
		return nil, withNotFound(fmt.Errorf("读取请求 %s 失败（用 search_requests 查可用 uid）: %w；%s", uid, err, s.requestCandidates(a, reqPath)))
	}
	return r, nil
}

// resolveCreateDraft 造出 create_request 的草稿底稿：cURL 导入 / 复制已有请求 / 空白三种来源。
// name 仍由调用方给（cURL 导入结果作为草稿内容）。
func resolveCreateDraft(a ProjectApp, in CreateRequestInput) (*collection.Request, error) {
	var draft *collection.Request
	var err error
	switch {
	case strings.TrimSpace(in.Curl) != "":
		// cURL 导入：URL/方法/头/体都从命令里解析
		draft, err = a.ParseCurl(in.Curl)
		if err != nil {
			return nil, withInvalid(fmt.Errorf("解析 cURL 失败: %w", err))
		}
	case strings.TrimSpace(in.CopyFromUID) != "":
		draft, err = a.ReadRequest(strings.TrimSpace(in.CopyFromUID))
		if err != nil {
			return nil, withNotFound(fmt.Errorf("读取源请求 %s 失败: %w", in.CopyFromUID, err))
		}
	default:
		draft = &collection.Request{Method: strings.ToUpper(strings.TrimSpace(in.Method))}
	}
	if draft == nil {
		return nil, errEmptyDraft
	}
	return draft, nil
}

// applyCreateFields 把入参里「显式传了」的字段覆盖到底稿上（非指针入参用「非空才覆盖」）。
// 唯一的校验：gRPC 请求不接受 respDoc，直接返回 errRespDocGRPC。
func applyCreateFields(draft *collection.Request, in CreateRequestInput) error {
	if in.Method != "" {
		draft.Method = strings.ToUpper(strings.TrimSpace(in.Method))
	}
	if in.URL != "" {
		draft.URL = in.URL
	}
	if len(in.Headers) > 0 {
		draft.Headers = kvList(in.Headers)
	}
	if len(in.Params) > 0 {
		draft.Params = kvList(in.Params)
	}
	if in.BodyType != "" || in.BodyRaw != "" {
		// 快捷字段只改报文类型/原文：不再整份替换 Body，否则 form/fields/descs 会被顺手清掉。
		if in.BodyType != "" {
			draft.Body.Type = in.BodyType
		}
		if in.BodyRaw != "" {
			draft.Body.Raw = in.BodyRaw
		}
	}
	// 完整 body（含 form/multipart 行）：按子项合并，表达力比 bodyType/bodyRaw 强
	if in.Body != nil {
		draft.Body = in.Body.merge(draft.Body)
	}
	// 字段表格与概述：与 body 相互独立，各自只在显式传入时覆盖（body 覆盖不再连带清掉它们）
	if in.BodyFields != nil {
		draft.Body.Fields = fieldList(in.BodyFields)
	}
	if in.Summary != "" {
		draft.Summary = in.Summary
	}
	if in.Settings != nil {
		draft.Settings = in.Settings
	}
	if in.RespDoc != nil {
		if draft.IsGRPC() {
			return errRespDocGRPC
		}
		draft.RespDoc = in.RespDoc.merge(draft.RespDoc)
	}
	if in.Auth != nil {
		draft.Auth = in.Auth
	}
	if in.Docs != "" {
		draft.Docs = in.Docs
	}
	return nil
}

// CreateModule 创建模块（目录），parent 为空表示根目录下新建。
func (s *Service) CreateModule(project, parent, name string) (string, error) {
	a, _, err := s.resolveWrite(project)
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errNeedName
	}
	childPath := strings.Trim(strings.Join([]string{strings.TrimSpace(parent), name}, "/"), "/")
	if _, ok := a.FolderUIDByPath(childPath); ok {
		return "", coded(CodeConflict, fmt.Errorf("模块 %s 已存在（改显示名用 rename_module）", childPath))
	}
	if err := a.CreateFolder(strings.TrimSpace(parent), name); err != nil {
		return "", err
	}
	child := name
	if p := strings.Trim(strings.TrimSpace(parent), "/"); p != "" {
		child = p + "/" + name
	}
	return child, nil
}

// update_request 的 section 取值：限定本次只改请求本身 / 响应文档 / 接口说明。
const (
	sectionRequest  = "request"
	sectionResponse = "response"
	sectionDocs     = "docs"
)

// ReplaceOp 文本局部替换（update_request.docs / update_doc.content）。
// 与整篇覆盖（docs / content）互斥：长文档只想改一段时用它能避免「抄全文再重写」的截断风险。
type ReplaceOp struct {
	Old        string `json:"old" jsonschema:"要被替换的原文片段（必须与正文完全一致，区分大小写）"`
	New        string `json:"new" jsonschema:"替换后的内容（传空串 = 删除该片段）"`
	ReplaceAll bool   `json:"replaceAll,omitempty" jsonschema:"true = 替换全部出现；默认只替换第一处"`
}

// applyReplaces 依次应用替换；任何一处 old 找不到都整体报错（调用方在写盘前调用，保证不落盘）。
func applyReplaces(text string, ops []ReplaceOp) (string, error) {
	for i, op := range ops {
		if op.Old == "" {
			return "", fmt.Errorf("replaces[%d].old 不能为空", i)
		}
		if !strings.Contains(text, op.Old) {
			return "", fmt.Errorf("replaces[%d] 未匹配到 old 片段（未写入任何内容）：%s", i, clipRunes(op.Old, 60))
		}
		if op.ReplaceAll {
			text = strings.ReplaceAll(text, op.Old, op.New)
		} else {
			text = strings.Replace(text, op.Old, op.New, 1)
		}
	}
	return text, nil
}

// UpdateRequestInput update_request 的入参（指针字段 = 只改传了的）。
type UpdateRequestInput struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	UID     string `json:"uid,omitempty" jsonschema:"请求 uid；与 path 二选一"`
	Path    string `json:"path,omitempty" jsonschema:"请求文件路径（相对集合根，如 api/users.yml 或 api/users）；与 uid 二选一"`
	// Section 节点守卫：限定本次调用只改某一个节点（见 validateSection）。
	Section string `json:"section,omitempty" jsonschema:"只在 request/response/docs 中改一个节点；混用报错；省略 = 不限制"`
	// DryRun 只校验参数与 ifMatch，不落盘、不移动目录
	DryRun bool      `json:"dryRun,omitempty" jsonschema:"true = 只预演（校验参数与 ifMatch），不落盘"`
	Name   *string   `json:"name,omitempty"`
	Method *string   `json:"method,omitempty" jsonschema:"请求方法（GET/POST/PUT/PATCH/DELETE/HEAD/OPTIONS/TRACE/CONNECT/GRPC；其它值会 [INVALID]）"`
	URL    *string   `json:"url,omitempty"`
	Params []KVInput `json:"params,omitempty" jsonschema:"参数表整表覆盖（含说明列）；不传不动；与 patch.params 互斥"`
	// Patch 行级合并：只加/改一行时用它，不必先抄全表；与对应的整表字段互斥。
	Patch      *PatchInput                 `json:"patch,omitempty" jsonschema:"行级合并 {params,headers,bodyFields}：同名改子字段否则追加；空数组清空；与整表字段互斥"`
	Headers    []KVInput                   `json:"headers,omitempty" jsonschema:"请求头整表覆盖；与 patch.headers 互斥"`
	Body       *BodyInput                  `json:"body,omitempty" jsonschema:"请求体局部更新：不传的子项保留；显式空串/空数组才清空"`
	BodyFields *[]BodyFieldInput           `json:"bodyFields,omitempty" jsonschema:"字段表整表覆盖；不传不动，空数组清空；与 patch.bodyFields 互斥"`
	Summary    *string                     `json:"summary,omitempty" jsonschema:"API 概述；不传不动，传空串清空"`
	Settings   *collection.RequestSettings `json:"settings,omitempty" jsonschema:"单请求设置（超时/重定向/忽略证书/URL 编码）"`
	RespDoc    *RespDocInput               `json:"respDoc,omitempty" jsonschema:"响应字段文档局部更新：不传的子项保留，显式空值清空；gRPC 拒绝"`
	Auth       *collection.Auth            `json:"auth,omitempty"`
	Docs       *string                     `json:"docs,omitempty" jsonschema:"接口说明（Markdown）；结构见 resource apidoc://doc-template"`
	// Replaces 接口说明的局部替换：与 docs 互斥；old 找不到即整单拒绝（不落盘）。
	Replaces []ReplaceOp `json:"replaces,omitempty" jsonschema:"说明的局部替换 [{old,new,replaceAll}]；与 docs 互斥；old 缺失则整单拒绝"`
	Folder   *string     `json:"folder,omitempty" jsonschema:"移动到别的模块（如 api/user；空串 = 项目根）；不存在 [NOT_FOUND]"` // 移动到别的模块
	// IfMatch 冲突检测：传 get_request_detail / search_requests 返回的 hash，
	// 若磁盘文件在此期间被外部改动则拒绝写入（不传 = 不做保护，行为同客户端「最后写者赢」）。
	IfMatch string `json:"ifMatch,omitempty"`
}

// validateSection 节点守卫：section 非空时，本次调用只允许传该节点的字段。
//
// 为什么要它：update_request 一次能做三件事（改请求本身 / 补响应文档 / 写接口说明），
// AI 常常「只想补响应字段」却顺手带上 url 之类，把请求本身改坏。加上 section 后，
// 越界会直接被拒，并在报错里列出**越界的字段名**与该 section 的允许清单，AI 能照着改。
// section 为空 = 不限制（兼容所有旧调用）。
func validateSection(section string, in UpdateRequestInput) error {
	sec := strings.ToLower(strings.TrimSpace(section))
	if sec == "" {
		return nil
	}
	// 本次实际传了的字段（指针/切片 nil = 没传；ifMatch/uid/project 不算数据字段）
	present := make([]string, 0, 12)
	if in.Name != nil {
		present = append(present, "name")
	}
	if in.Method != nil {
		present = append(present, "method")
	}
	if in.URL != nil {
		present = append(present, "url")
	}
	if in.Params != nil {
		present = append(present, "params")
	}
	if in.Patch != nil {
		present = append(present, "patch")
	}
	if in.Headers != nil {
		present = append(present, "headers")
	}
	if in.Body != nil {
		present = append(present, "body")
	}
	if in.BodyFields != nil {
		present = append(present, "bodyFields")
	}
	if in.Summary != nil {
		present = append(present, "summary")
	}
	if in.Auth != nil {
		present = append(present, "auth")
	}
	if in.Settings != nil {
		present = append(present, "settings")
	}
	if in.Folder != nil {
		present = append(present, "folder")
	}
	if in.RespDoc != nil {
		present = append(present, "respDoc")
	}
	if in.Docs != nil {
		present = append(present, "docs")
	}
	if len(in.Replaces) > 0 {
		present = append(present, "replaces")
	}
	var allowed []string
	switch sec {
	case sectionRequest:
		allowed = []string{"name", "method", "url", "params", "patch", "headers",
			"body", "bodyFields", "summary", "auth", "settings", "folder"}
	case sectionResponse:
		allowed = []string{"respDoc"}
	case sectionDocs:
		allowed = []string{"docs", "replaces"}
	default:
		// 参数错不是服务故障：必须包 withInvalid，否则会被归到 [INTERNAL]。
		return withInvalid(fmt.Errorf("不支持的 section %q：只支持 request（请求本身）/ response（响应字段文档 respDoc）/ docs（接口说明），省略 section 表示不限制", section))
	}
	allowedSet := make(map[string]bool, len(allowed))
	for _, f := range allowed {
		allowedSet[f] = true
	}
	bad := make([]string, 0, len(present))
	for _, f := range present {
		if !allowedSet[f] {
			bad = append(bad, f)
		}
	}
	if len(bad) > 0 {
		return withInvalid(fmt.Errorf("section=%s 只允许改 %s，本次还传了越界字段 %s：请去掉这些字段，或改用对应的 section（request / response / docs）",
			sec, strings.Join(allowed, "/"), strings.Join(bad, "/")))
	}
	return nil
}

// UpdateRequest 局部更新；带冲突检测（磁盘被外部改过就拒写）。
func (s *Service) UpdateRequest(in UpdateRequestInput) (*collection.Request, error) {
	a, _, err := s.resolveWrite(in.Project)
	if err != nil {
		return nil, err
	}
	return s.updateRequestWithApp(a, in)
}

// updateRequestWithApp 在已解析的项目实例上执行单条更新。
//
// 批量更新复用同一个 ProjectApp：resolveWrite 会做项目查找与加锁，逐条调用等于重复付出；
// 批量入口自己负责开始/结束时各刷新一次索引。
func (s *Service) updateRequestWithApp(a ProjectApp, in UpdateRequestInput) (*collection.Request, error) {
	if in.Method != nil {
		m, merr := normalizeMethod(*in.Method)
		if merr != nil {
			return nil, merr
		}
		in.Method = &m
	}
	// 节点守卫放在最前面：不值得为一次注定被拒的调用去读盘/建索引。
	if err := validateSection(in.Section, in); err != nil {
		return nil, err
	}
	// 整篇覆盖与局部替换互斥（同时传会让「到底以谁为准」变得不可预期）
	if len(in.Replaces) > 0 && in.Docs != nil {
		return nil, withInvalid(errors.New("docs 与 replaces 互斥：整篇覆盖用 docs，局部替换用 replaces（不要同时传）"))
	}
	uid, err := s.resolveRequestUID(a, in.UID, in.Path)
	if err != nil {
		return nil, err
	}
	r, err := a.ReadRequest(uid)
	if err != nil {
		return nil, withNotFound(fmt.Errorf("读取请求 %s 失败: %w；%s", uid, err, s.requestCandidates(a, in.Path)))
	}
	// 局部替换先算：old 找不到就整单拒绝（此时还没写盘）
	if len(in.Replaces) > 0 {
		docs, rerr := applyReplaces(r.Docs, in.Replaces)
		if rerr != nil {
			return nil, rerr
		}
		r.Docs = docs
	}
	if err := applyUpdateFields(r, in); err != nil {
		return nil, err
	}
	// 冲突检测：优先用调用方（AI）读到的 hash；没给就退回当前磁盘 hash（等于不保护）
	hash := strings.TrimSpace(in.IfMatch)
	if hash == "" {
		hash = s.diskHashOf(a, r)
	}
	r.ExpectHash = hash
	if in.DryRun {
		// dryRun：只校验参数与 ifMatch，不写盘、也不移动目录
		return r, nil
	}
	// 移动模块要在保存后做（路径/seq 由集合层分配）
	if err := a.SaveRequest(r); err != nil {
		if strings.Contains(err.Error(), "[conflict]") {
			return nil, conflictError("请求", r.UID, s.diskHashOf(a, r))
		}
		return nil, err
	}
	if in.Folder != nil {
		if err := a.MoveRequest(r.UID, strings.Trim(strings.TrimSpace(*in.Folder), "/")); err != nil {
			return nil, err
		}
		moved, err := a.ReadRequest(r.UID)
		if err == nil {
			r = moved
		}
	}
	return r, nil
}

// applyUpdateFields 把 update_request 里「非 nil」的字段覆盖到已读请求上（指针入参 = 只改传了的）。
// 唯一校验：gRPC 请求不接受 respDoc，返回 errRespDocGRPC。
func applyUpdateFields(r *collection.Request, in UpdateRequestInput) error {
	if in.Name != nil {
		r.Name = strings.TrimSpace(*in.Name)
	}
	if in.Method != nil {
		r.Method = strings.ToUpper(strings.TrimSpace(*in.Method))
	}
	if in.URL != nil {
		r.URL = strings.TrimSpace(*in.URL)
	}
	if in.Headers != nil {
		if in.Patch != nil && in.Patch.Headers != nil {
			return withInvalid(errors.New("headers 与 patch.headers 互斥：整表覆盖用 headers，只加/改一行用 patch"))
		}
		r.Headers = kvList(in.Headers)
	}
	if in.Params != nil {
		if in.Patch != nil && in.Patch.Params != nil {
			return withInvalid(errors.New("params 与 patch.params 互斥：整表覆盖用 params，只加/改一行用 patch"))
		}
		r.Params = kvList(in.Params)
	}
	if in.Body != nil {
		// 按子项合并：nil = 保留原值，显式空（"" / [] / {}）= 清空。
		// 以前是整份覆盖 + fields/descs 快照还原（只保住了这两个子项），
		// 合并语义把 form 也一起保住了 —— 只改 raw 不会再清掉表单行。
		r.Body = in.Body.merge(r.Body)
	}
	// 字段表格单独覆盖：显式传入（含空数组=清空）才动，优先级高于 body.fields
	if in.BodyFields != nil {
		if in.Patch != nil && in.Patch.BodyFields != nil {
			return withInvalid(errors.New("bodyFields 与 patch.bodyFields 互斥：整表覆盖用 bodyFields，只加/改一行用 patch"))
		}
		r.Body.Fields = fieldList(*in.BodyFields)
	}
	// 行级合并：同名只覆盖传入的子字段，没有则追加。
	// nil（没传）= 不动；空数组（显式传 []）= 清空整表 —— 与整表字段的语义一致，
	// 避免调用方以为「传 [] 清空了」实际什么都没发生。
	if in.Patch != nil {
		switch {
		case len(in.Patch.Headers) > 0:
			r.Headers = upsertKV(r.Headers, in.Patch.Headers)
		case in.Patch.Headers != nil: // 显式空数组 = 清空
			r.Headers = nil
		}
		switch {
		case len(in.Patch.Params) > 0:
			r.Params = upsertKV(r.Params, in.Patch.Params)
		case in.Patch.Params != nil:
			r.Params = nil
		}
		switch {
		case len(in.Patch.BodyFields) > 0:
			r.Body.Fields = upsertFields(r.Body.Fields, in.Patch.BodyFields)
		case in.Patch.BodyFields != nil:
			r.Body.Fields = nil
		}
	}
	if in.Summary != nil {
		r.Summary = *in.Summary
	}
	if in.Settings != nil {
		r.Settings = in.Settings
	}
	if in.RespDoc != nil {
		if r.IsGRPC() {
			return errRespDocGRPC
		}
		r.RespDoc = in.RespDoc.merge(r.RespDoc)
	}
	if in.Auth != nil {
		r.Auth = in.Auth
	}
	if in.Docs != nil {
		r.Docs = *in.Docs
	}
	return nil
}

// upsertKV 按 Name 合并行：同名**只覆盖传入的子字段**（nil = 保留原值），没有则追加。
// 空 name 的行跳过 —— 新建请求自带一行空占位参数，不该被"追加"出更多空行。
func upsertKV(rows []collection.KV, add []KVInput) []collection.KV {
	out := append([]collection.KV(nil), rows...)
	for _, a := range add {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			continue
		}
		idx := -1
		for i := range out {
			if out[i].Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			out = append(out, a.toKV())
			continue
		}
		out[idx].Name = name
		if a.Value != nil {
			out[idx].Value = *a.Value
		}
		if a.Type != nil {
			out[idx].Type = *a.Type
		}
		if a.Description != nil {
			out[idx].Description = *a.Description
		}
		if a.Enabled != nil {
			out[idx].Enabled = *a.Enabled
		}
	}
	return out
}

// upsertFields 按字段名合并请求体字段表：同名只覆盖传入的子字段（nil = 保留），没有则追加。
// 「只改这一行的说明」不会顺带清掉类型/示例值。
func upsertFields(rows []collection.BodyField, add []BodyFieldInput) []collection.BodyField {
	out := append([]collection.BodyField(nil), rows...)
	for _, a := range add {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			continue
		}
		idx := -1
		for i := range out {
			if out[i].Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			out = append(out, a.toField())
			continue
		}
		out[idx].Name = name
		if a.Type != nil {
			out[idx].Type = *a.Type
		}
		if a.Required != nil {
			out[idx].Required = *a.Required
		}
		if a.Example != nil {
			out[idx].Example = *a.Example
		}
		if a.Description != nil {
			out[idx].Description = *a.Description
		}
	}
	return out
}

// CreateProject 新建项目：目标必须是**授权且可写**的工作目录。
//
// root 为空时用第一个可写的授权目录。以前这里接受任意 root（临时建一个 registry 就在那儿建集合），
// 等于把「能写哪里」交给调用方 —— 现在白名单外一律拒绝。
func (s *Service) CreateProject(root, name, dirName string) (Project, error) {
	if strings.TrimSpace(root) == "" {
		p, err := s.reg.CreateProject(name, dirName)
		if err != nil {
			return Project{}, classifyCreateProjectErr(err)
		}
		return p, nil
	}
	al, err := s.reg.allowFor(root, true)
	if err != nil {
		return Project{}, classifyCreateProjectErr(err)
	}
	// root 必须是「工作目录」，不能是已有项目目录本身：
	// 否则会在项目里再套一个项目，扫描只认顶层子目录，新项目随后就「找不到」。
	absRoot, aerr := filepath.Abs(root)
	if aerr != nil {
		absRoot = root
	}
	absRoot = filepath.Clean(absRoot)
	if _, serr := os.Stat(filepath.Join(absRoot, "opencollection.yml")); serr == nil {
		return Project{}, withInvalid(fmt.Errorf("root %s 本身是项目目录，不能在其中新建项目；请传它的父级工作目录（list_workspaces 里可写的那个）", absRoot))
	}
	p, err := s.reg.CreateProjectIn(al.Path, name, dirName)
	if err != nil {
		return Project{}, classifyCreateProjectErr(err)
	}
	return p, nil
}

// classifyCreateProjectErr 给 create_project 的错误映射稳定错误码（评审反馈：此前一律 [INTERNAL]）。
func classifyCreateProjectErr(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "不在 MCP 授权"):
		return coded(CodeForbidden, err)
	case strings.Contains(msg, "只读授权"):
		return coded(CodeReadOnly, err)
	case strings.Contains(msg, "项目已存在"):
		return coded(CodeConflict, err)
	case strings.Contains(msg, "缺少 name"), strings.Contains(msg, "非法字符"), strings.Contains(msg, "目录名"),
		strings.Contains(msg, "本身是项目目录"):
		return withInvalid(err)
	default:
		return coded(CodeIO, err)
	}
}

// DocsOf 读取请求的说明文档原文（仅 docs 字段）：
// update_request 成功后的「落盘确认」需要旧文本做行级对比，不拉示例/响应字段。
func (s *Service) DocsOf(project, uid string) (string, error) {
	a, _, err := s.resolve(project)
	if err != nil {
		return "", err
	}
	r, err := a.ReadRequest(strings.TrimSpace(uid))
	if err != nil {
		return "", err
	}
	return r.Docs, nil
}

// hashOf 取请求文件的当前磁盘哈希（index 提供，失败返回空串=跳过冲突检测）。
// hashOf 索引里的请求 hash（get_request_detail 的返回口径）。
//
// 刻意不用 FileHash：索引未刷新时它仍是「客户端上次看到的状态」，
// 调用方拿这个 hash 做 ifMatch 才能真正发现「读之后被外部改过」。
func (s *Service) hashOf(a ProjectApp, uid string) (string, error) {
	nodes, err := a.SearchIndex("", 0)
	if err != nil {
		return "", err
	}
	for _, n := range nodes {
		if n.UID == uid {
			return n.Hash, nil
		}
	}
	return "", nil
}

// diskHashOf 直接按文件路径取当前 hash。
//
// 写路径用它：r 刚从磁盘读出来，路径已知，没必要为一条记录全树扫索引
// （批量更新 10 条时这是主要开销）。
func (s *Service) diskHashOf(a ProjectApp, r *collection.Request) string {
	if r == nil {
		return ""
	}
	return a.FileHash(r.Path)
}

// DeleteRequest 移除请求（进 .trash，可恢复）。
func (s *Service) DeleteRequest(project, uid, ifMatch string) error {
	return s.DeleteRequestAt(project, uid, "", ifMatch)
}

// DeleteRequestAt 同 DeleteRequest，但支持按 path 定位（uid 与 path 二选一）。
func (s *Service) DeleteRequestAt(project, uid, reqPath, ifMatch string) error {
	a, _, err := s.resolveWrite(project)
	if err != nil {
		return err
	}
	uid, err = s.resolveRequestUID(a, uid, reqPath)
	if err != nil {
		return err
	}
	r, err := a.ReadRequest(uid)
	if err != nil {
		return withNotFound(fmt.Errorf("读取请求 %s 失败（用 search_requests 查可用 uid）: %w；%s", uid, err, s.requestCandidates(a, reqPath)))
	}
	if !hashPrefixMatch(a.FileHash(r.Path), ifMatch) {
		return coded(CodeConflict, fmt.Errorf("请求 %s 已被他人修改（当前 path=%s），请重新读取后再删", uid, r.Path))
	}
	if err := a.DeleteRequest(uid); err != nil {
		return withNotFound(err)
	}
	return nil
}

// 允许的请求方法（schema enum 与运行时校验同源）。
var allowedMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true,
	"HEAD": true, "OPTIONS": true, "TRACE": true, "CONNECT": true, "GRPC": true,
}

// normalizeMethod 校验并大写 method；空值原样返回（不改动）。
func normalizeMethod(m string) (string, error) {
	m = strings.ToUpper(strings.TrimSpace(m))
	if m == "" || allowedMethods[m] {
		return m, nil
	}
	return "", withInvalid(fmt.Errorf("method %q 不合法；可选：GET/POST/PUT/PATCH/DELETE/HEAD/OPTIONS/TRACE/CONNECT/GRPC", m))
}

// DeleteRequestItem 批量删除的一条：uid + 必填 ifMatch（防误删）。
type DeleteRequestItem struct {
	UID     string `json:"uid,omitempty"`  // 与 Path 二选一
	Path    string `json:"path,omitempty"` // 请求文件路径（相对集合根）
	IfMatch string `json:"ifMatch"`        // 必填：该请求当前 hash
	// ConfirmName 可选逐条二次确认：非空时必须与请求显示名一致，否则该条拒绝。
	// 批量删同名请求（如十个「用户列表」）时，ifMatch 只能防并发，防不了「删错那一个」。
	ConfirmName string `json:"confirmName,omitempty"`
}

// DeleteRequestFailure 单条失败（不影响其它条目）。
type DeleteRequestFailure struct {
	Index int    `json:"index"`
	UID   string `json:"uid"`
	Code  string `json:"code"`
	Error string `json:"error"`
}

// DeleteRequests 批量删除（每条强制 ifMatch；dryRun 只校验不落盘）。
// confirm 非空时必须等于项目显示名 / 相对路径 / 目录名：整批删除比单条危险，给调用方一次显式确认点。
func (s *Service) DeleteRequests(project, confirm string, items []DeleteRequestItem, dryRun bool) ([]string, []DeleteRequestFailure, error) {
	if len(items) == 0 {
		return nil, nil, withInvalid(errors.New("items 不能为空：每条给 {uid 或 path, ifMatch}（ifMatch 从 search_requests 的 results[].hash 取）"))
	}
	if len(items) > maxBatchDocs {
		return nil, nil, withLimit(fmt.Errorf("items 一次最多 %d 条（本次 %d 条）：请分批调用", maxBatchDocs, len(items)))
	}
	a, e, err := s.resolveWrite(project)
	if err != nil {
		return nil, nil, err
	}
	if confirm = strings.TrimSpace(confirm); confirm != "" &&
		confirm != e.Name && confirm != e.Path && confirm != filepath.Base(e.Dir) {
		return nil, nil, coded(CodeConflict, fmt.Errorf("confirm 与项目名不一致（当前项目是 %q / %s），整批未删除", e.Name, e.Path))
	}
	var deleted []string
	var failed []DeleteRequestFailure
	for i, it := range items {
		uid, uerr := s.resolveRequestUID(a, it.UID, it.Path)
		if uerr != nil {
			code := CodeOf(uerr)
			failed = append(failed, DeleteRequestFailure{
				Index: i, UID: strings.TrimSpace(it.UID), Code: code,
				Error: strings.TrimPrefix(uerr.Error(), "["+code+"] "),
			})
			continue
		}
		if strings.TrimSpace(it.IfMatch) == "" {
			failed = append(failed, DeleteRequestFailure{Index: i, UID: uid, Code: CodeInvalid, Error: "每条都必须给 ifMatch（从 search_requests 的 results[].hash 取）"})
			continue
		}
		r, rerr := a.ReadRequest(uid)
		if rerr != nil {
			failed = append(failed, DeleteRequestFailure{Index: i, UID: uid, Code: CodeNotFound,
				Error: fmt.Sprintf("读取请求 %s 失败: %v", uid, rerr)})
			continue
		}
		if cn := strings.TrimSpace(it.ConfirmName); cn != "" && cn != strings.TrimSpace(r.Name) {
			failed = append(failed, DeleteRequestFailure{Index: i, UID: uid, Code: CodeConflict,
				Error: fmt.Sprintf("confirmName 与请求名不一致（该条是 %q），未删除", r.Name)})
			continue
		}
		if !hashPrefixMatch(a.FileHash(r.Path), it.IfMatch) {
			failed = append(failed, DeleteRequestFailure{Index: i, UID: uid, Code: CodeConflict,
				Error: fmt.Sprintf("请求 %s 已被他人修改，本次未删除", uid)})
			continue
		}
		if !dryRun {
			if derr := a.DeleteRequest(uid); derr != nil {
				failed = append(failed, DeleteRequestFailure{Index: i, UID: uid, Code: CodeIO, Error: derr.Error()})
				continue
			}
		}
		deleted = append(deleted, uid)
	}
	return deleted, failed, nil
}

// hashPrefixMatch 空的 expect 不校验；否则要求完整相等或 actual 以 expect 为前缀（12 位短 hash 也可）。
func hashPrefixMatch(actual, expect string) bool {
	expect = strings.TrimSpace(expect)
	return expect == "" || actual == expect || strings.HasPrefix(actual, expect)
}

// SendRequestInput send_request 的入参。
type SendRequestInput struct {
	Project string `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	UID     string `json:"uid,omitempty"` // 与下面的临时请求字段二选一
	// 临时请求（不落盘）：AI 只想试发一个地址时用
	Method  string           `json:"method,omitempty" jsonschema:"请求方法（GET/POST/PUT/PATCH/DELETE/HEAD/OPTIONS/TRACE/CONNECT/GRPC；其它值会 [INVALID]）"`
	URL     string           `json:"url,omitempty"`
	Headers []collection.KV  `json:"headers,omitempty"`
	Body    *collection.Body `json:"body,omitempty"`
	// 行为
	Env          string `json:"env,omitempty"`         // 空 = 默认环境
	SaveExample  *bool  `json:"saveExample,omitempty"` // 默认 false：显式 true 才把响应存成 examples/*.yml
	ExampleName  string `json:"exampleName,omitempty"` // 示例名；空则按状态码自动命名
	IncludeBody  *bool  `json:"includeBody,omitempty"` // 是否返回响应体；不传 = 默认返回
	MaxBodyBytes int    `json:"maxBodyBytes,omitempty"`
}

// SendRequest 发送请求；saveExample 为真时把响应存为响应示例（客户端可回看/删除）。
func (s *Service) SendRequest(in SendRequestInput) (*SendOutcome, error) {
	if strings.TrimSpace(in.Method) != "" {
		m, merr := normalizeMethod(in.Method)
		if merr != nil {
			return nil, merr
		}
		in.Method = m
	}
	a, e, err := s.resolve(in.Project)
	if err != nil {
		return nil, err
	}
	var req *collection.Request
	if uid := strings.TrimSpace(in.UID); uid != "" {
		req, err = a.ReadRequest(uid)
		if err != nil {
			return nil, withNotFound(fmt.Errorf("读取请求 %s 失败: %w", uid, err))
		}
	} else if strings.TrimSpace(in.URL) != "" {
		// 临时请求：不落盘，直接组装发送
		req = &collection.Request{
			UID:     "mcp-tmp",
			Method:  strings.ToUpper(strings.TrimSpace(in.Method)),
			URL:     strings.TrimSpace(in.URL),
			Headers: in.Headers,
		}
		if in.Body != nil {
			req.Body = *in.Body
		}
		if req.Method == "" {
			req.Method = "GET"
		}
	} else {
		return nil, errNeedUID
	}
	res, err := a.SendRequest(req, strings.TrimSpace(in.Env))
	if err != nil {
		if strings.Contains(err.Error(), "不是 http/https") {
			return nil, withInvalid(fmt.Errorf("URL 无法发送：%w", err))
		}
		return nil, coded(CodeIO, fmt.Errorf("发送失败: %w", err))
	}
	out := outcomeOf(res, in.MaxBodyBytes)
	if in.IncludeBody != nil && !*in.IncludeBody {
		out.Body, out.Truncated, out.OriginalBytes = "", false, 0
	}
	// 默认不落盘（外部评审 T7）：只读探测「打一下看看返回什么」不应产生副作用；
	// 需要留存的调用方显式传 saveExample=true。
	if in.SaveExample != nil && *in.SaveExample {
		// 只读授权目录不能写示例：发送本身允许（只读 / 镜像集合同样要能试打），落盘要拦
		if e != nil && !e.Writable() {
			return &out, fmt.Errorf("响应已返回，但工作目录 %s 是只读授权，示例未保存（需要保存请在客户端设置里把该目录改成可写）", e.AllowRoot())
		}
		// 只有集合内真实请求才存示例（临时请求没有文件可挂靠）
		if !strings.HasPrefix(req.UID, "mcp-tmp") && req.UID != "" {
			name := strings.TrimSpace(in.ExampleName)
			if name == "" {
				name = fmt.Sprintf("%d 响应", res.Status)
			}
			ex, serr := a.SaveResponseExample(req, name, res)
			if serr != nil {
				return &out, fmt.Errorf("响应已返回，但保存示例失败: %w", serr)
			}
			out.Saved, out.ExampleUID, out.ExamplePath = true, ex.UID, ex.Path
		}
	}
	return &out, nil
}

// ---- 响应示例（examples/*.yml）的增 / 改 / 删 ----
//
// send_request 只能「存刚发出来的那个响应」；AI 需要把外部文档、日志里的响应片段也留成示例，
// 或者修正已存示例的名字/状态码/响应体，于是补这三个工具。
// 示例是集合内的纯文本 YAML（可 git diff / 评审），删除一律走 .trash（可人工找回）。

// SaveResponseExampleInput save_response_example 的入参。
type SaveResponseExampleInput struct {
	Project     string          `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	UID         string          `json:"uid" jsonschema:"请求 uid（示例挂在它名下；用 search_requests 或 get_project_modules 获取）"`
	Name        string          `json:"name,omitempty" jsonschema:"示例名（同时用作文件名）；省略 = 「响应示例」；重名会自动加 (1)(2)"`
	Status      int             `json:"status,omitempty" jsonschema:"响应状态码；省略 = 200"`
	Headers     []collection.KV `json:"headers,omitempty" jsonschema:"响应头行（name/value/enabled）"`
	Body        string          `json:"body,omitempty" jsonschema:"响应体原文；落盘的 size 按它的字节数记录"`
	ContentType string          `json:"contentType,omitempty" jsonschema:"响应媒体类型；省略按 body 首字符推断（{/[ → JSON，其余 text/plain）"`
	DryRun      bool            `json:"dryRun,omitempty" jsonschema:"true = 只预演（校验请求/名称并给出落点），不写盘"`
}

// SaveResponseExample 手动保存一条响应示例（不发送请求）。
func (s *Service) SaveResponseExample(in SaveResponseExampleInput) (*collection.ResponseExample, error) {
	a, _, err := s.resolveWrite(in.Project)
	if err != nil {
		return nil, err
	}
	uid := strings.TrimSpace(in.UID)
	if uid == "" {
		return nil, errNeedUID
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = defaultExampleName
	}
	status := in.Status
	if status == 0 {
		status = 200 // 省略 = 最常见的那一种，而不是「状态码 0」这种不存在的响应
	}
	if in.DryRun {
		return a.PlanResponseExampleRaw(uid, name, status, in.Headers, in.Body, in.ContentType)
	}
	return a.SaveResponseExampleRaw(uid, name, status, in.Headers, in.Body, in.ContentType)
}

// defaultExampleName 示例名的默认值（AI 不给 name 时用）。
const defaultExampleName = "响应示例"

// UpdateResponseExampleInput update_response_example 的入参（指针 = 只改传了的）。
type UpdateResponseExampleInput struct {
	Project     string           `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	UID         string           `json:"uid" jsonschema:"请求 uid（示例所属请求）"`
	ExampleUID  string           `json:"exampleUid" jsonschema:"响应示例 uid（get_request_detail 返回的 examples[].uid）"`
	Name        *string          `json:"name,omitempty" jsonschema:"示例名；改名同步重命名磁盘文件；非法字符报错且不改动"`
	Status      *int             `json:"status,omitempty" jsonschema:"响应状态码；不传不动，传 0 = 清零"`
	Headers     *[]collection.KV `json:"headers,omitempty" jsonschema:"响应头；不传不动，传空数组即清空"`
	Body        *string          `json:"body,omitempty" jsonschema:"响应体原文；不传不动（改了会同步更新 size 字节数）"`
	ContentType *string          `json:"contentType,omitempty" jsonschema:"响应媒体类型；不传不动"`
	DryRun      bool             `json:"dryRun,omitempty" jsonschema:"true = 只预演（校验并给出改后落点），不写盘"`
}

// UpdateResponseExample 局部更新一条已保存的响应示例。
func (s *Service) UpdateResponseExample(in UpdateResponseExampleInput) (*collection.ResponseExample, error) {
	a, _, err := s.resolveWrite(in.Project)
	if err != nil {
		return nil, err
	}
	uid := strings.TrimSpace(in.UID)
	if uid == "" {
		return nil, errNeedUID
	}
	exUID := strings.TrimSpace(in.ExampleUID)
	if exUID == "" {
		return nil, withInvalid(fmt.Errorf("缺少 exampleUid：请先用 get_request_detail 查看该请求下的响应示例（examples[].uid）"))
	}
	// 先自己确认示例存在：写接口层返回的是未归类的哨兵错误，会落成 [INTERNAL]。
	exs, err := s.listExamples(a, uid)
	if err != nil {
		return nil, err
	}
	found := false
	for _, ex := range exs {
		if ex.UID == exUID {
			found = true
			break
		}
	}
	if !found {
		return nil, withNotFound(fmt.Errorf("响应示例 %s 不存在（请求 %s）", exUID, uid))
	}
	if in.DryRun {
		return a.PlanUpdateResponseExample(uid, exUID, in.Name, in.Status, in.Headers, in.Body, in.ContentType)
	}
	return a.UpdateResponseExample(uid, exUID, in.Name, in.Status, in.Headers, in.Body, in.ContentType)
}

// listExamples 列某请求下的响应示例。
//
// 先确认请求存在：集合层的「条目不存在」若直接归到 [IO]，调用方会误以为磁盘故障。
func (s *Service) listExamples(a ProjectApp, uid string) ([]*collection.ResponseExample, error) {
	if _, err := a.ReadRequest(uid); err != nil {
		return nil, withNotFound(fmt.Errorf("读取请求 %s 失败: %w", uid, err))
	}
	exs, err := a.ListResponseExamples(uid)
	if err != nil {
		return nil, coded(CodeIO, err)
	}
	return exs, nil
}

// DeleteResponseExample 删除一条响应示例（文件移入 .trash，可人工找回）。
func (s *Service) DeleteResponseExample(project, reqUID, exampleUID, ifMatch string) error {
	a, _, err := s.resolveWrite(project)
	if err != nil {
		return err
	}
	uid := strings.TrimSpace(reqUID)
	if uid == "" {
		return errNeedUID
	}
	exUID := strings.TrimSpace(exampleUID)
	if exUID == "" {
		return withInvalid(fmt.Errorf("缺少 exampleUid：请先用 get_request_detail 查看该请求下的响应示例（examples[].uid）"))
	}
	exs, lerr := s.listExamples(a, uid)
	if lerr != nil {
		return lerr
	}
	var target *collection.ResponseExample
	for _, ex := range exs {
		if ex.UID == exUID {
			target = ex
			break
		}
	}
	if target == nil {
		return withNotFound(fmt.Errorf("响应示例 %s 不存在（请求 %s）", exUID, uid))
	}
	if !hashPrefixMatch(a.FileHash(target.Path), ifMatch) {
		return coded(CodeConflict, fmt.Errorf("响应示例 %s 已被他人修改，请重新读取后再删", exUID))
	}
	return a.DeleteResponseExample(uid, exUID)
}
