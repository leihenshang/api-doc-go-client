package mcp

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/index"
)

// 工具层的常见错误（AI 读得懂的中文提示）。
var (
	errEmptyQuery      = errors.New("缺少 query：请给出要搜索的关键词")
	errNoProjectLoaded = errors.New("当前没有已加载的项目：先用 list_projects 选一个项目打开，或用 get_project_modules 触发打开")
	errNeedName        = errors.New("缺少 name：创建请求必须给名称")
	errEmptyDraft      = errors.New("没有可用的请求内容")
	errNeedUID         = errors.New("缺少 uid：请用 search_requests 或 get_project_modules 找到目标请求")
	errConflict        = errors.New("文件已被外部修改，本次保存被拒绝：请重新 get_request_detail 读取最新内容，合并后再试（或用 create_request 另存为新请求）")
)

// Service 是 MCP 工具的语义实现（不依赖 SDK，便于单测与将来复用到 CLI）。
type Service struct {
	reg *Registry
}

// NewService 构造服务层。
func NewService(reg *Registry) *Service { return &Service{reg: reg} }

// Reg 暴露底层 Registry（测试需要直接打开项目以填充统计；工具层不需要）。
func (s *Service) Reg() *Registry { return s.reg }

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

// Projects 返回当前扫描到的项目条目数（入口日志用）。
func (s *Service) Projects() []Entry { return s.reg.Entries() }

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
}

// GetProjectModules 项目目录（模块）列表；query 模糊匹配模块名与模块下请求名/方法/路径。
func (s *Service) GetProjectModules(project, query string, includeRequests bool) (*ModulesResult, error) {
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
	a, _, err := s.reg.App(project)
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
func (s *Service) SearchRequests(query, project string, limit int) ([]Match, int, error) {
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
		ms, n, err := s.searchIn(a, key, query, 0)
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
		a, _, err := s.reg.App(project)
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

// searchIn 对一个项目做两阶段打分：先用索引字段（name/url/method/path），
// 命中不足时再读候选文件补 header/docs/body 后重新打分。
func (s *Service) searchIn(a ProjectApp, project, query string, limit int) ([]Match, int, error) {
	if _, err := a.ReloadCollection(); err != nil { // 索引/树是缓存的，先刷新
		return nil, 0, err
	}
	nodes, err := a.SearchIndex("", 0) // 空查询 = 全量
	if err != nil {
		return nil, 0, err
	}
	var matches []Match
	for _, c := range candidatesFromIndex(nodes) {
		score, fields := scoreCandidate(c, query)
		if score <= 0 {
			continue
		}
		matches = append(matches, Match{
			UID: c.UID, Name: c.Name, Method: c.Method, URL: c.URL, Path: c.Path,
			Project: project, Score: score, Matched: fields, Hash: c.hash,
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
		s.enrichAndRescore(a, seeds, query)
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

// enrichAndRescore 读候选文件，补 header / docs / body 字段后重打分（分数只升不降）。
func (s *Service) enrichAndRescore(a ProjectApp, matches []Match, query string) {
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
		c := candidate{
			UID: r.UID, Name: r.Name, Method: r.Method, URL: r.URL, Path: r.Path,
			Header: sb.String(), Docs: r.Docs, Body: bodySummary(r),
		}
		if score, fields := scoreCandidate(c, query); score > matches[i].Score {
			matches[i].Score, matches[i].Matched = score, fields
		}
	}
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
	Params      []collection.KV             `json:"params,omitempty"`
	Headers     []collection.KV             `json:"headers,omitempty"`
	Body        collection.Body             `json:"body"`
	Auth        *collection.Auth            `json:"auth,omitempty"`
	Settings    *collection.RequestSettings `json:"settings,omitempty"`
	GRPC        *collection.GrpcBlock       `json:"grpc,omitempty"`
	Docs        string                      `json:"docs,omitempty"`        // 请求自带说明（Markdown）
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
	Query        string // 在示例名/文档名里再筛一遍（可选）
}

// GetRequestDetail 接口详情：请求（含 headers/body/auth/grpc）+ 说明 + 已保存响应示例。
func (s *Service) GetRequestDetail(project, uid string, opts DetailOptions) (*RequestDetail, error) {
	a, e, err := s.reg.App(project)
	if err != nil {
		return nil, err
	}
	r, err := a.ReadRequest(uid)
	if err != nil {
		return nil, fmt.Errorf("读取请求 %s 失败: %w", uid, err)
	}
	hash, _ := s.hashOf(a, r.UID)
	d := &RequestDetail{
		Project: e.Path, UID: r.UID, Hash: hash, Name: r.Name, Method: r.Method, URL: r.URL, Path: r.Path,
		Params: r.Params, Headers: r.Headers, Body: r.Body, Auth: r.Auth,
		Settings: r.Settings, GRPC: r.GRPC, Docs: r.Docs,
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
				UID: ex.UID, Name: ex.Name, Path: ex.Path, CreatedAt: ex.CreatedAt,
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

// CreateRequestInput create_request 的入参。
type CreateRequestInput struct {
	Project     string           `json:"project"`
	Folder      string           `json:"folder,omitempty"`
	Name        string           `json:"name"`
	Method      string           `json:"method,omitempty"`
	URL         string           `json:"url,omitempty"`
	Headers     []collection.KV  `json:"headers,omitempty"`
	Params      []collection.KV  `json:"params,omitempty"`
	BodyType    string           `json:"bodyType,omitempty"`
	BodyRaw     string           `json:"bodyRaw,omitempty"`
	Auth        *collection.Auth `json:"auth,omitempty"`
	Docs        string           `json:"docs,omitempty"`
	Curl        string           `json:"curl,omitempty"`        // 与 name/folder 互斥：走 ParseCurl 导入
	CopyFromUID string           `json:"copyFromUid,omitempty"` // 复制另一个请求再改名
}

// CreateRequest 创建请求（三种来源：手填参数 / cURL 导入 / 复制已有请求）。
func (s *Service) CreateRequest(in CreateRequestInput) (*collection.Request, error) {
	a, e, err := s.reg.App(in.Project)
	if err != nil {
		return nil, err
	}
	var draft *collection.Request
	switch {
	case strings.TrimSpace(in.Curl) != "":
		// cURL 导入：URL/方法/头/体都从命令里解析，name 仍由调用方给（导入结果作为草稿内容）
		draft, err = a.ParseCurl(in.Curl)
		if err != nil {
			return nil, fmt.Errorf("解析 cURL 失败: %w", err)
		}
	case strings.TrimSpace(in.CopyFromUID) != "":
		draft, err = a.ReadRequest(strings.TrimSpace(in.CopyFromUID))
		if err != nil {
			return nil, fmt.Errorf("读取源请求 %s 失败: %w", in.CopyFromUID, err)
		}
	default:
		draft = &collection.Request{Method: strings.ToUpper(strings.TrimSpace(in.Method))}
	}
	if draft == nil {
		return nil, errEmptyDraft
	}
	if in.Method != "" {
		draft.Method = strings.ToUpper(strings.TrimSpace(in.Method))
	}
	if in.URL != "" {
		draft.URL = in.URL
	}
	if len(in.Headers) > 0 {
		draft.Headers = in.Headers
	}
	if len(in.Params) > 0 {
		draft.Params = in.Params
	}
	if in.BodyType != "" || in.BodyRaw != "" {
		draft.Body = collection.Body{Type: in.BodyType, Raw: in.BodyRaw}
	}
	if in.Auth != nil {
		draft.Auth = in.Auth
	}
	if in.Docs != "" {
		draft.Docs = in.Docs
	}
	// 草稿模板的默认 headers 会在没有 headers 时被 CreateRequestFromDraft 填上，这里不覆盖
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errNeedName
	}
	created, err := a.CreateRequestFromDraft(strings.TrimSpace(in.Folder), name, draft)
	if err != nil {
		return nil, err
	}
	_ = e
	return created, nil
}

// CreateModule 创建模块（目录），parent 为空表示根目录下新建。
func (s *Service) CreateModule(project, parent, name string) (string, error) {
	a, _, err := s.reg.App(project)
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errNeedName
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

// UpdateRequestInput update_request 的入参（指针字段 = 只改传了的）。
type UpdateRequestInput struct {
	Project string           `json:"project"`
	UID     string           `json:"uid"`
	Name    *string          `json:"name,omitempty"`
	Method  *string          `json:"method,omitempty"`
	URL     *string          `json:"url,omitempty"`
	Headers []collection.KV  `json:"headers,omitempty"`
	Body    *collection.Body `json:"body,omitempty"`
	Auth    *collection.Auth `json:"auth,omitempty"`
	Docs    *string          `json:"docs,omitempty"`
	Folder  *string          `json:"folder,omitempty"` // 移动到别的模块
	// IfMatch 冲突检测：传 get_request_detail / search_requests 返回的 hash，
	// 若磁盘文件在此期间被外部改动则拒绝写入（不传 = 不做保护，行为同客户端「最后写者赢」）。
	IfMatch string `json:"ifMatch,omitempty"`
}

// UpdateRequest 局部更新；带冲突检测（磁盘被外部改过就拒写）。
func (s *Service) UpdateRequest(in UpdateRequestInput) (*collection.Request, error) {
	a, _, err := s.reg.App(in.Project)
	if err != nil {
		return nil, err
	}
	r, err := a.ReadRequest(strings.TrimSpace(in.UID))
	if err != nil {
		return nil, fmt.Errorf("读取请求 %s 失败: %w", in.UID, err)
	}
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
		r.Headers = in.Headers
	}
	if in.Body != nil {
		r.Body = *in.Body
	}
	if in.Auth != nil {
		r.Auth = in.Auth
	}
	if in.Docs != nil {
		r.Docs = *in.Docs
	}
	// 冲突检测：优先用调用方（AI）读到的 hash；没给就退回当前磁盘 hash（等于不保护）
	hash := strings.TrimSpace(in.IfMatch)
	if hash == "" {
		if cur, herr := s.hashOf(a, r.UID); herr == nil {
			hash = cur
		}
	}
	r.ExpectHash = hash
	// 移动模块要在保存后做（路径/seq 由集合层分配）
	if err := a.SaveRequest(r); err != nil {
		if strings.Contains(err.Error(), "[conflict]") {
			return nil, errConflict
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

// CreateProject 在项目根下新建项目（集合目录 + manifest）。
func (s *Service) CreateProject(root, name, dirName string) (Project, error) {
	if strings.TrimSpace(root) == "" {
		root = s.reg.Root()
	}
	if root != s.reg.Root() {
		// 指定了别的根：临时建一个 registry 复用 CreateProject 逻辑
		reg, err := NewRegistry(root, s.reg.NewAppFactory())
		if err != nil {
			return Project{}, err
		}
		return reg.CreateProject(name, dirName)
	}
	return s.reg.CreateProject(name, dirName)
}

// hashOf 取请求文件的当前磁盘哈希（index 提供，失败返回空串=跳过冲突检测）。
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

// DeleteRequest 移除请求（进 .trash，可恢复）。
func (s *Service) DeleteRequest(project, uid string) error {
	a, _, err := s.reg.App(project)
	if err != nil {
		return err
	}
	return a.DeleteRequest(strings.TrimSpace(uid))
}

// SendRequestInput send_request 的入参。
type SendRequestInput struct {
	Project string `json:"project"`
	UID     string `json:"uid,omitempty"` // 与下面的临时请求字段二选一
	// 临时请求（不落盘）：AI 只想试发一个地址时用
	Method  string           `json:"method,omitempty"`
	URL     string           `json:"url,omitempty"`
	Headers []collection.KV  `json:"headers,omitempty"`
	Body    *collection.Body `json:"body,omitempty"`
	// 行为
	Env          string `json:"env,omitempty"`         // 空 = 默认环境
	SaveExample  *bool  `json:"saveExample,omitempty"` // 默认 true：把响应存成 examples/*.yml
	ExampleName  string `json:"exampleName,omitempty"` // 示例名；空则按状态码自动命名
	IncludeBody  *bool  `json:"includeBody,omitempty"` // 是否返回响应体；不传 = 默认返回
	MaxBodyBytes int    `json:"maxBodyBytes,omitempty"`
}

// SendRequest 发送请求；saveExample 为真时把响应存为响应示例（客户端可回看/删除）。
func (s *Service) SendRequest(in SendRequestInput) (*SendOutcome, error) {
	a, _, err := s.reg.App(in.Project)
	if err != nil {
		return nil, err
	}
	var req *collection.Request
	if uid := strings.TrimSpace(in.UID); uid != "" {
		req, err = a.ReadRequest(uid)
		if err != nil {
			return nil, fmt.Errorf("读取请求 %s 失败: %w", uid, err)
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
		return nil, fmt.Errorf("发送失败: %w", err)
	}
	out := outcomeOf(res, in.MaxBodyBytes)
	if in.IncludeBody != nil && !*in.IncludeBody {
		out.Body, out.Truncated, out.OriginalBytes = "", false, 0
	}
	if in.SaveExample == nil || *in.SaveExample {
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
