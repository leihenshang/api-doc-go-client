// Package collection 客户端集合的文件层：本地目录即集合（Bruno 风格），
// 文件是真相源。目录布局见设计文档《客户端与同步架构设计》§3.1。
package collection

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	share "github.com/leihenshang/api-doc-go-share/collection"

	"api-doc-go-client/internal/config"
	"api-doc-go-client/internal/index"
	"api-doc-go-client/internal/watch"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// 集合内的保留目录/文件（不参与扫描与树渲染）。
// docs/ 载文档条目（B13），参与扫描；assets/ 载附件（R9），不参与。
var reserved = map[string]bool{
	".trash": true, ".conflicts": true, "assets": true,
	"node_modules": true, "environments": true,
	"examples": true, // 保存的响应示例（example.go），不是集合条目
}

var errNotFound = errors.New("条目不存在")

// Collection 一个已打开的集合目录。
type Collection struct {
	Dir  string // 绝对路径
	UID  string
	Name string
	idx  *index.DB      // 本地 SQLite 索引（C1）；惰性打开
	w    *watch.Watcher // 外部改动监听（D1）；惰性启动
}

// ---------- 集合清单（opencollection.yml） ----------

const gitignoreContent = "# 客户端自动维护\n" +
	"environments/*.secrets.yml\n*.local.yml\n.trash/\n.conflicts/\nnode_modules/\n"

// Open 打开（不存在则初始化）一个集合目录。
// 原生 Bruno 目录（含 bruno.json 或 .bru）不能直接打开，须走导入流程。
func Open(dir string) (*Collection, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(abs); err == nil && st.IsDir() {
		if IsBrunoDir(abs) {
			return nil, fmt.Errorf("检测到 Bruno 集合目录，请使用「导入」功能转换后再打开")
		}
	}
	if st, err := os.Stat(abs); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return nil, err
		}
	} else if !st.IsDir() {
		return nil, fmt.Errorf("%s 不是目录", abs)
	}

	c := &Collection{Dir: abs}
	mPath := filepath.Join(abs, "opencollection.yml")
	if data, err := os.ReadFile(mPath); err == nil {
		m, err := share.DecodeManifest(data)
		if err != nil {
			return nil, fmt.Errorf("opencollection.yml 解析失败: %w", err)
		}
		c.UID, c.Name = m.Meta.UID, m.Info.Name
	}
	if c.UID == "" {
		c.UID = uuid.NewString()
	}
	if c.Name == "" {
		c.Name = filepath.Base(abs)
	}
	if err := c.writeManifest(); err != nil {
		return nil, err
	}
	// 集合级 .gitignore（幂等）：secret 文件与本地目录不进版本控制
	gi := filepath.Join(abs, ".gitignore")
	if _, err := os.Stat(gi); os.IsNotExist(err) {
		_ = os.WriteFile(gi, []byte(gitignoreContent), 0o644)
	}
	// 首次打开：给一个默认环境，便于立刻体验变量替换
	if _, err := os.Stat(filepath.Join(abs, "environments")); os.IsNotExist(err) {
		_ = c.SaveEnv(Env{Name: "dev", Vars: []Var{{
			Name: "host", Value: "http://127.0.0.1:8080", Enabled: true,
		}}})
	}
	// 本地索引（C1）：打开即重建一次，保证与目录一致；删除索引文件后下次打开自动补回
	_ = c.RebuildIndex()
	// 外部改动监听（D1/D2）
	_ = c.StartWatch()
	return c, nil
}

// ---------- 外部改动监听（D1/D2）----------

// StartWatch 启动目录监听；失败不阻塞打开（可手动 ⟳ 兜底）。
func (c *Collection) StartWatch() error {
	if c.w != nil {
		return nil
	}
	w, err := watch.New(c.Dir)
	if err != nil {
		return err
	}
	c.w = w
	return nil
}

// WatchEvents 外部改动事件流（未启动时返回 nil）。
func (c *Collection) WatchEvents() <-chan watch.Event {
	if c.w == nil {
		return nil
	}
	return c.w.Events()
}

// StopWatch 停止监听。
func (c *Collection) StopWatch() {
	if c.w != nil {
		_ = c.w.Close()
		c.w = nil
	}
}

// ignoreWrite 自写回环：写盘前短暂屏蔽对该相对路径的监听。
func (c *Collection) ignoreWrite(rel string) {
	if c.w != nil {
		c.w.Ignore(rel, 400*time.Millisecond)
	}
}

// ---------- 本地索引（C1） ----------

// indexPath 索引文件放在用户配置目录（不污染集合、不进 git）。
func (c *Collection) indexPath() string {
	base, err := config.Dir()
	if err != nil {
		return filepath.Join(c.Dir, ".index.sqlite")
	}
	return filepath.Join(base, "index", c.UID+".sqlite")
}

// ensureIndex 惰性打开索引库。
func (c *Collection) ensureIndex() (*index.DB, error) {
	if c.idx != nil {
		return c.idx, nil
	}
	db, err := index.Open(c.indexPath())
	if err != nil {
		return nil, err
	}
	c.idx = db
	return c.idx, nil
}

// RebuildIndex 从目录扫描整表重建索引（可删库后恢复）。
// 请求写入内容 hash（sha256）；synced_hash/base_rev 由索引层保留。
func (c *Collection) RebuildIndex() error {
	db, err := c.ensureIndex()
	if err != nil {
		return err
	}
	res, err := c.scan()
	if err != nil {
		return err
	}
	nodes := make([]index.Node, 0, len(res.reqs)+len(res.uidByDir))
	for dir, uid := range res.uidByDir {
		full := filepath.Join(c.Dir, filepath.FromSlash(dir))
		var mtime int64
		if st, err := os.Stat(full); err == nil {
			mtime = st.ModTime().UnixMilli()
		}
		nodes = append(nodes, index.Node{
			UID: uid, Type: "folder", Path: dir,
			Title: res.nameByDir[dir], MTime: mtime,
		})
	}
	for uid, r := range res.reqs {
		full := filepath.Join(c.Dir, filepath.FromSlash(r.Path))
		var mtime int64
		if st, err := os.Stat(full); err == nil {
			mtime = st.ModTime().UnixMilli()
		}
		nodes = append(nodes, index.Node{
			UID: uid, Type: "request", Path: r.Path,
			Title: r.Name, Method: r.Method, URL: r.URL, MTime: mtime,
			Hash: fileHash(full), BaseRev: r.BaseRev,
		})
	}
	return db.Rebuild(nodes)
}

// fileHash 计算文件内容 sha256（读不到时返回空串）。
func fileHash(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// DirtyNodes 返回 hash 与上次同步不一致的节点。
func (c *Collection) DirtyNodes() ([]index.Node, error) {
	db, err := c.ensureIndex()
	if err != nil {
		return nil, err
	}
	// 先刷新 hash
	_ = c.RebuildIndex()
	return db.Dirty()
}

// MarkSynced 标记某请求已同步（hash 固化 + base_rev 更新）。
func (c *Collection) MarkSynced(uid, hash string, baseRev int64) error {
	db, err := c.ensureIndex()
	if err != nil {
		return err
	}
	return db.MarkSynced(uid, hash, baseRev)
}

// NodeHash 取某 uid 当前内容 hash。
func (c *Collection) NodeHash(uid string) string {
	db, err := c.ensureIndex()
	if err != nil {
		return ""
	}
	n, ok, _ := db.Get(uid)
	if !ok {
		return ""
	}
	return n.Hash
}

// FileHashOf 按相对路径直接算文件 hash（不依赖索引，pull 应用后固化用）。
func (c *Collection) FileHashOf(rel string) string {
	return fileHash(filepath.Join(c.Dir, filepath.FromSlash(rel)))
}

// Search 本地索引搜索（标题/URL/方法/路径）；q 为空返回全部。
func (c *Collection) Search(q string, limit int) ([]index.Node, error) {
	db, err := c.ensureIndex()
	if err != nil {
		return nil, err
	}
	// 索引空而目录非空时补建（首次或被删）
	if n, err := db.Count(); err == nil && n == 0 {
		_ = c.RebuildIndex()
	}
	return db.Search(q, limit)
}

// CloseIndex 释放索引句柄（切换集合时调用）。
func (c *Collection) CloseIndex() {
	if c.idx != nil {
		_ = c.idx.Close()
		c.idx = nil
	}
}

// manifestVersion 集合清单的 opencollection 版本号。
const manifestVersion = "1.0.0"

func (c *Collection) writeManifest() error {
	m := &manifest{}
	m.Info.Name, m.Meta.UID = c.Name, c.UID
	data, err := m.Encode(manifestVersion)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.Dir, "opencollection.yml"), data, 0o644)
}

// ---------- 内部：文件 ↔ 结构 ----------

// readRequestFile 读取磁盘上的请求文件（未知顶层字段留在 Extra 里，未丢失）。
func readRequestFile(path string) (*requestFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return share.DecodeRequest(data)
}

func (r *Request) toFile() *requestFile {
	f := &requestFile{Docs: r.Docs, Settings: r.Settings}
	f.Info.Name, f.Info.Type, f.Info.Seq = r.Name, "http", r.Seq
	f.Meta.UID, f.Meta.BaseRev = r.UID, r.BaseRev
	f.HTTP.Method, f.HTTP.URL = r.Method, r.URL
	f.HTTP.Params, f.HTTP.Headers, f.HTTP.Body = r.Params, r.Headers, r.Body
	f.HTTP.Auth = r.Auth
	f.Extra = map[string]any{}
	if len(r.VarsPreRequest) > 0 {
		list := make([]map[string]any, 0, len(r.VarsPreRequest))
		for _, v := range r.VarsPreRequest {
			list = append(list, map[string]any{"name": v.Name, "value": v.Value, "enabled": v.Enabled})
		}
		f.Extra["vars"] = map[string]any{"pre-request": list}
	}
	if r.Script != nil && (r.Script.PreRequest != "" || r.Script.PostResponse != "") {
		m := map[string]any{}
		if r.Script.PreRequest != "" {
			m["pre-request"] = r.Script.PreRequest
		}
		if r.Script.PostResponse != "" {
			m["post-response"] = r.Script.PostResponse
		}
		f.Extra["script"] = m
	}
	if len(r.Asserts) > 0 {
		list := make([]map[string]any, 0, len(r.Asserts))
		for _, a := range r.Asserts {
			item := map[string]any{"expr": a.Expr}
			if a.Name != "" {
				item["name"] = a.Name
			}
			list = append(list, item)
		}
		f.Extra["assert"] = list
	}
	return f
}

func fromFile(path string, f *requestFile) *Request {
	r := &Request{
		UID: f.Meta.UID, Name: f.Info.Name, Seq: f.Info.Seq, Path: path,
		Method: strings.ToUpper(f.HTTP.Method), URL: f.HTTP.URL,
		Params: f.HTTP.Params, Headers: f.HTTP.Headers, Body: f.HTTP.Body,
		Auth: f.HTTP.Auth, Settings: f.Settings,
		Docs: f.Docs, BaseRev: f.Meta.BaseRev,
	}
	r.VarsPreRequest, r.Script, r.Asserts = parseScriptExtra(f.Extra)
	return r
}

// parseScriptExtra 从 Extra 里取 vars/script/assert（Bruno 超集顶层字段）。
func parseScriptExtra(extra map[string]any) (vars []ScriptVar, scr *ScriptBlock, asserts []ScriptAssert) {
	if extra == nil {
		return nil, nil, nil
	}
	if v, ok := extra["vars"].(map[string]any); ok {
		if list, ok := v["pre-request"].([]any); ok {
			for _, item := range list {
				m, _ := item.(map[string]any)
				if m == nil {
					continue
				}
				vars = append(vars, ScriptVar{
					Name:    asString(m["name"]),
					Value:   asString(m["value"]),
					Enabled: asBool(m["enabled"], true),
				})
			}
		}
	}
	if v, ok := extra["script"].(map[string]any); ok {
		s := &ScriptBlock{
			PreRequest:   asString(v["pre-request"]),
			PostResponse: asString(v["post-response"]),
		}
		if s.PreRequest != "" || s.PostResponse != "" {
			scr = s
		}
	}
	if list, ok := extra["assert"].([]any); ok {
		for _, item := range list {
			m, _ := item.(map[string]any)
			if m == nil {
				continue
			}
			expr := asString(m["expr"])
			if expr == "" {
				continue
			}
			asserts = append(asserts, ScriptAssert{Name: asString(m["name"]), Expr: expr})
		}
	}
	return vars, scr, asserts
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func asBool(v any, def bool) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x != "false" && x != ""
	case nil:
		return def
	default:
		return def
	}
}

// ---------- 扫描与树 ----------

// scanResult 一次目录扫描的产物。
type scanResult struct {
	reqs      map[string]*Request // uid → request
	reqByPath map[string]*Request
	uidByDir  map[string]string    // 目录相对路径 → folder.yml 的 uid
	nameByDir map[string]string    // 目录相对路径 → folder.yml 的显示名（重命名后与目录名不同）
	docs      map[string]*DocEntry // uid → doc
}

func (c *Collection) scan() (*scanResult, error) {
	res := &scanResult{
		reqs: map[string]*Request{}, reqByPath: map[string]*Request{},
		uidByDir: map[string]string{}, nameByDir: map[string]string{},
		docs: map[string]*DocEntry{},
	}
	err := filepath.WalkDir(c.Dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(c.Dir, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if reserved[name] || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".yml" && ext != ".yaml" {
			return nil
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		if dir == "." {
			dir = ""
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if strings.Contains(name, ".secrets.") {
			return nil // secret 文件随主环境文件一起处理
		}
		var head struct {
			Info struct {
				Type string `yaml:"type"`
				Name string `yaml:"name"`
				Seq  int    `yaml:"seq"`
			} `yaml:"info"`
			Meta struct {
				UID string `yaml:"uid"`
			} `yaml:"meta"`
		}
		if yaml.Unmarshal(data, &head) != nil {
			return nil // 非法 YAML：跳过（不中断整个扫描）
		}
		switch head.Info.Type {
		case "http":
			var f requestFile
			if yaml.Unmarshal(data, &f) != nil || f.Meta.UID == "" {
				return nil
			}
			r := fromFile(rel, &f)
			res.reqs[r.UID] = r
			res.reqByPath[rel] = r
		case "folder":
			if head.Meta.UID != "" {
				res.uidByDir[dir] = head.Meta.UID
			}
			if head.Info.Name != "" {
				res.nameByDir[dir] = head.Info.Name
			}
		default:
			// B13 文档条目：docs/*.md
			if dir == docsDir && strings.HasSuffix(strings.ToLower(name), ".md") {
				if d, err := parseDocFile(rel, data); err == nil {
					res.docs[d.UID] = d
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// Tree 扫描集合并构建树（分组在前，组内按 seq / 名称排序；含文档条目 B13）。
func (c *Collection) Tree() ([]*Node, error) {
	res, err := c.scan()
	if err != nil {
		return nil, err
	}
	// 目录树：先收集目录与请求
	type dirNode struct {
		name     string
		path     string
		children []*Node
	}
	dirs := map[string]*dirNode{"": {name: "", path: ""}}
	var ensureDir func(dir string) *dirNode
	ensureDir = func(dir string) *dirNode {
		if n, ok := dirs[dir]; ok {
			return n
		}
		parent := filepath.ToSlash(filepath.Dir(dir))
		if parent == "." {
			parent = ""
		}
		p := ensureDir(parent)
		name := res.nameByDir[dir] // 优先 folder.yml 的显示名（重命名后与目录名不同）
		if name == "" {
			name = filepath.Base(dir)
		}
		n := &dirNode{name: name, path: dir}
		dirs[dir] = n
		p.children = append(p.children, &Node{Type: "folder", UID: res.uidByDir[dir], Name: name, Path: dir, Children: nil})
		return n
	}
	// 挂请求
	for _, r := range res.reqByPath {
		dir := filepath.ToSlash(filepath.Dir(r.Path))
		if dir == "." {
			dir = ""
		}
		n := ensureDir(dir)
		n.children = append(n.children, &Node{Type: "request", UID: r.UID, Name: r.Name, Path: r.Path, Method: r.Method, Seq: r.Seq})
	}
	// 挂文档条目（B13）：docs/*.md 归入根，type=doc
	for _, d := range res.docs {
		root := ensureDir("")
		root.children = append(root.children, &Node{Type: "doc", UID: d.UID, Name: d.Name, Path: d.Path, Seq: 0})
	}
	// 空分组（只有 folder.yml、暂无请求）也必须出现在树里，否则建完就"消失"
	for dir := range res.uidByDir {
		if dir != "" {
			ensureDir(dir)
		}
	}
	// 组装 + 排序（分组在前；组内 seq 升序、名称次之）
	var build func(children []*Node) []*Node
	build = func(children []*Node) []*Node {
		out := make([]*Node, 0, len(children))
		for _, ch := range children {
			if ch.Type != "folder" {
				out = append(out, ch)
				continue
			}
			dn, ok := dirs[ch.Path]
			if !ok {
				continue
			}
			ch.Children = build(dn.children)
			out = append(out, ch)
		}
		share.SortNodes(out)
		return out
	}
	return build(dirs[""].children), nil
}

// Info 集合概要（树 + 环境）。
func (c *Collection) Info() (*CollectionInfo, error) {
	tree, err := c.Tree()
	if err != nil {
		return nil, err
	}
	envs, err := c.ListEnvs()
	if err != nil {
		return nil, err
	}
	return &CollectionInfo{Dir: c.Dir, Name: c.Name, UID: c.UID, Tree: tree, Envs: envs}, nil
}

// ---------- 环境变量 ----------

func (c *Collection) envPaths(name string) (main, sec string) {
	base := filepath.Join(c.Dir, "environments", name)
	return base + ".yml", base + ".secrets.yml"
}

// ListEnvs 读取全部环境；secret 变量的值从 *.secrets.yml 合并（本地明文）。
func (c *Collection) ListEnvs() ([]Env, error) {
	entries, err := os.ReadDir(filepath.Join(c.Dir, "environments"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || (strings.ToLower(filepath.Ext(n)) != ".yml" && strings.ToLower(filepath.Ext(n)) != ".yaml") {
			continue
		}
		if strings.Contains(n, ".secrets.") {
			continue
		}
		names = append(names, strings.TrimSuffix(n, filepath.Ext(n)))
	}
	sort.Strings(names)
	out := make([]Env, 0, len(names))
	for _, name := range names {
		env, err := c.readEnv(name)
		if err != nil {
			return nil, err
		}
		out = append(out, *env)
	}
	return out, nil
}

func (c *Collection) readEnv(name string) (*Env, error) {
	mainP, secP := c.envPaths(name)
	data, err := os.ReadFile(mainP)
	if err != nil {
		return nil, err
	}
	f, err := share.DecodeEnv(data)
	if err != nil {
		return nil, fmt.Errorf("环境 %s 解析失败: %w", name, err)
	}
	sec := map[string]string{}
	if sd, err := os.ReadFile(secP); err == nil {
		if got, serr := share.DecodeSecrets(sd); serr == nil && got != nil {
			sec = got
		}
	}
	env := &Env{Name: name}
	for _, v := range f.Vars {
		if v.Secret {
			if val, ok := sec[v.Name]; ok {
				v.Value = val
			}
		}
		env.Vars = append(env.Vars, v)
	}
	return env, nil
}

// SaveEnv 保存环境：secret 变量的值写入 *.secrets.yml（不进可提交文件），主文件留占位。
func (c *Collection) SaveEnv(env Env) error {
	if !validEnvName(env.Name) {
		return fmt.Errorf("环境名只能包含字母、数字、- 与 _")
	}
	if err := os.MkdirAll(filepath.Join(c.Dir, "environments"), 0o755); err != nil {
		return err
	}
	f := envFile{}
	f.Info.Name = env.Name
	sec := secretsFile{Secrets: map[string]string{}}
	for _, v := range env.Vars {
		if v.Name == "" {
			continue
		}
		if v.Secret {
			sec.Secrets[v.Name] = v.Value
			v.Value = ""
		}
		f.Vars = append(f.Vars, v)
	}
	mainData, err := f.Encode()
	if err != nil {
		return err
	}
	secData, err := share.EncodeSecrets(sec.Secrets)
	if err != nil {
		return err
	}
	mainP, secP := c.envPaths(env.Name)
	if err := os.WriteFile(mainP, mainData, 0o644); err != nil {
		return err
	}
	return os.WriteFile(secP, secData, 0o600) // secrets 文件收权
}

// DeleteEnv 删除环境（主文件与 secrets 文件移入 .trash/）。
func (c *Collection) DeleteEnv(name string) error {
	if !validEnvName(name) {
		return fmt.Errorf("非法环境名")
	}
	mainP, secP := c.envPaths(name)
	for _, p := range []string{mainP, secP} {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := c.moveToTrash(p); err != nil {
			return err
		}
	}
	return nil
}

// ---------- 请求 CRUD ----------

// validEnvName / sanitizeFileName 的磁盘命名规则随共享包走，改名处只需调一次。
var (
	validEnvName     = share.ValidEnvName
	sanitizeFileName = share.SanitizeFileName
	validEntryName   = share.ValidEntryName
)

// CreateRequest 在 folder（相对路径，"" = 根目录）下新建请求并落盘。
// URL 留空：新建时不再预填 {{host}} 占位，让用户（或「导入 cURL」）决定真实地址。
func (c *Collection) CreateRequest(folder, name, method string) (*Request, error) {
	if !validEntryName(name) {
		return nil, fmt.Errorf("名称含非法字符或为空")
	}
	r := &Request{
		Method:  strings.ToUpper(strings.TrimSpace(method)),
		Params:  []KV{{Enabled: true}},
		Headers: []KV{{Name: "Content-Type", Value: "application/json", Enabled: true}},
		Body:    Body{Type: "none"},
	}
	return c.saveNewRequest(folder, name, r)
}

// CreateRequestFromDraft 把内存草稿落盘成新请求（新建流程：先开空 tab 编辑，关闭时才落盘）。
// uid / 文件名 / seq / path 一律由集合层重新分配（草稿里可能带临时 uid），其余内容原样采用。
func (c *Collection) CreateRequestFromDraft(folder, name string, src *Request) (*Request, error) {
	if !validEntryName(name) {
		return nil, fmt.Errorf("名称含非法字符或为空")
	}
	if src == nil {
		return nil, fmt.Errorf("请求内容为空")
	}
	method := strings.ToUpper(strings.TrimSpace(src.Method))
	if method == "" {
		method = "GET"
	}
	r := &Request{
		Method:         method,
		URL:            strings.TrimSpace(src.URL),
		Params:         src.Params,
		Headers:        src.Headers,
		Body:           src.Body,
		Auth:           src.Auth,
		Settings:       src.Settings,
		Docs:           src.Docs,
		VarsPreRequest: src.VarsPreRequest,
		Script:         src.Script,
		Asserts:        src.Asserts,
	}
	if len(r.Params) == 0 {
		r.Params = []KV{{Enabled: true}}
	}
	return c.saveNewRequest(folder, name, r)
}

// CreateRequestWithUID 用指定 uid 新建（同步 pull 落盘用，保证与服务端 ext_uid 一致）。
func (c *Collection) CreateRequestWithUID(folder, name, method, uid string) (*Request, error) {
	if uid == "" {
		return nil, fmt.Errorf("缺少 uid")
	}
	if !validEntryName(name) {
		return nil, fmt.Errorf("名称含非法字符或为空")
	}
	r := &Request{
		UID:     uid,
		Method:  strings.ToUpper(strings.TrimSpace(method)),
		URL:     "{{host}}/",
		Params:  []KV{{Enabled: true}},
		Headers: []KV{{Name: "Content-Type", Value: "application/json", Enabled: true}},
		Body:    Body{Type: "none"},
	}
	if err := c.ensureDir(folder); err != nil {
		return nil, err
	}
	base := sanitizeFileName(name)
	rel := joinRel(folder, base+".yml")
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(c.Dir, filepath.FromSlash(rel))); os.IsNotExist(err) {
			break
		}
		rel = joinRel(folder, fmt.Sprintf("%s-%d.yml", base, i))
	}
	r.Name, r.Seq, r.Path = name, c.nextSeq(folder), rel
	if err := c.SaveRequest(r); err != nil {
		return nil, err
	}
	return r, nil
}

// ensureDir 确保分组目录存在且带 folder.yml（folder 为空表示根目录）。
func (c *Collection) ensureDir(folder string) error {
	if folder == "" {
		return nil
	}
	clean := filepath.Clean(folder)
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return fmt.Errorf("非法的分组路径")
	}
	if err := os.MkdirAll(filepath.Join(c.Dir, filepath.FromSlash(clean)), 0o755); err != nil {
		return err
	}
	if _, ok := c.folderUID(clean); !ok {
		return c.ensureFolderYML(clean, filepath.Base(clean))
	}
	return nil
}

// saveNewRequest 落盘一个新请求：文件名冲突时自动加序号，Path / Seq / UID 由本方法决定。
func (c *Collection) saveNewRequest(folder, name string, r *Request) (*Request, error) {
	if err := c.ensureDir(folder); err != nil {
		return nil, err
	}
	base := sanitizeFileName(name)
	rel := joinRel(folder, base+".yml")
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(c.Dir, filepath.FromSlash(rel))); os.IsNotExist(err) {
			break
		}
		rel = joinRel(folder, fmt.Sprintf("%s-%d.yml", base, i))
	}
	r.UID, r.Name, r.Seq, r.Path = uuid.NewString(), name, c.nextSeq(folder), rel
	if err := c.SaveRequest(r); err != nil {
		return nil, err
	}
	return r, nil
}

// CreateFolder 新建分组（目录 + folder.yml）。
// CreateFolder 在 parent（相对路径，空 = 根）下创建分组。
func (c *Collection) CreateFolder(parent, name string) error {
	if !validEntryName(name) {
		return fmt.Errorf("名称含非法字符或为空")
	}
	base, err := c.resolveFolder(parent)
	if err != nil {
		return err
	}
	rel := joinRel(base, sanitizeFileName(name))
	if err := os.MkdirAll(filepath.Join(c.Dir, filepath.FromSlash(rel)), 0o755); err != nil {
		return err
	}
	return c.ensureFolderYML(rel, name)
}

// RenameFolder 改分组显示名（目录名与 uid 保持不变，避免路径漂移）。
func (c *Collection) RenameFolder(uid, name string) error {
	if !validEntryName(name) {
		return fmt.Errorf("名称含非法字符或为空")
	}
	dir, err := c.findDir(uid)
	if err != nil {
		return err
	}
	full := filepath.Join(c.Dir, filepath.FromSlash(dir), "folder.yml")
	data, err := os.ReadFile(full)
	if os.IsNotExist(err) {
		return c.ensureFolderYML(dir, name)
	}
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
	info["name"] = name
	doc["info"] = info
	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("序列化分组描述: %w", err)
	}
	return os.WriteFile(full, out, 0o644)
}

// DeleteFolder 删除空分组（有子分组/请求时拒绝，避免误删）；目录移入 .trash。
func (c *Collection) DeleteFolder(uid string) error {
	dir, err := c.findDir(uid)
	if err != nil {
		return err
	}
	if dir == "" {
		return fmt.Errorf("不能删除集合根目录")
	}
	if !c.folderEmpty(dir) {
		return fmt.Errorf("分组非空：请先删除其下的分组与请求")
	}
	return c.moveToTrash(filepath.Join(c.Dir, filepath.FromSlash(dir)))
}

// RenameRequest 改请求显示名（文件名与 uid 保持不变）。
func (c *Collection) RenameRequest(uid, name string) error {
	if !validEntryName(name) {
		return fmt.Errorf("名称含非法字符或为空")
	}
	res, err := c.scan()
	if err != nil {
		return err
	}
	r, ok := res.reqs[uid]
	if !ok {
		return errNotFound
	}
	r.Name = name
	return c.SaveRequest(r)
}

// MoveRequest 把请求移动到目标分组（destFolder 空 = 根）；文件名与 meta.uid 保持不变。
func (c *Collection) MoveRequest(uid, destFolder string) error {
	res, err := c.scan()
	if err != nil {
		return err
	}
	r, ok := res.reqs[uid]
	if !ok {
		return errNotFound
	}
	dest, err := c.resolveFolder(destFolder)
	if err != nil {
		return err
	}
	oldFull := filepath.Join(c.Dir, filepath.FromSlash(r.Path))
	oldDir := filepath.ToSlash(filepath.Dir(r.Path))
	if oldDir == dest {
		return nil // 已在目标分组
	}
	if err := c.ensureDir(dest); err != nil {
		return err
	}
	base := filepath.Base(filepath.FromSlash(r.Path))
	newRel := joinRel(dest, base)
	newFull := filepath.Join(c.Dir, filepath.FromSlash(newRel))
	if err := os.MkdirAll(filepath.Dir(newFull), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(newFull); err == nil {
		return fmt.Errorf("目标分组已存在同名文件")
	}
	if err := os.Rename(oldFull, newFull); err != nil {
		return fmt.Errorf("移动请求文件: %w", err)
	}
	r.Path = newRel
	return c.SaveRequest(r)
}

// MoveFolder 把分组移动到目标父分组（destParent 空 = 根）；目录名与 uid 保持不变。
// 拒绝移入自己或自己的后代，避免目录树成环。
func (c *Collection) MoveFolder(uid, destParent string) error {
	dir, err := c.findDir(uid)
	if err != nil {
		return err
	}
	if dir == "" {
		return fmt.Errorf("不能移动集合根目录")
	}
	dest, err := c.resolveFolder(destParent)
	if err != nil {
		return err
	}
	// 目标不能是自己或后代
	if dest == dir || strings.HasPrefix(dest+"/", dir+"/") {
		return fmt.Errorf("不能移动到自己或其子分组下")
	}
	oldParent := filepath.ToSlash(filepath.Dir(dir))
	if oldParent == dest {
		return nil
	}
	base := path.Base(dir)
	newRel := joinRel(dest, base)
	if newRel == dir {
		return nil
	}
	// 目标下不允许同名分组
	if _, err := os.Stat(filepath.Join(c.Dir, filepath.FromSlash(newRel))); err == nil {
		return fmt.Errorf("目标分组下已存在同名目录")
	}
	if err := os.MkdirAll(filepath.Join(c.Dir, filepath.FromSlash(dest)), 0o755); err != nil {
		return err
	}
	if err := os.Rename(
		filepath.Join(c.Dir, filepath.FromSlash(dir)),
		filepath.Join(c.Dir, filepath.FromSlash(newRel)),
	); err != nil {
		return fmt.Errorf("移动分组: %w", err)
	}
	return nil
}

// resolveFolder 校验并规范化分组相对路径（必须是集合内已存在的目录）。
func (c *Collection) resolveFolder(parent string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(parent)))
	if clean == "." || clean == "/" {
		return "", nil
	}
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("非法的分组路径")
	}
	if _, ok := c.folderUID(clean); !ok {
		return "", fmt.Errorf("分组不存在: %s", clean)
	}
	return clean, nil
}

func (c *Collection) findDir(uid string) (string, error) {
	res, err := c.scan()
	if err != nil {
		return "", err
	}
	for dir, u := range res.uidByDir {
		if u == uid {
			return dir, nil
		}
	}
	return "", errNotFound
}

// folderEmpty 分组是否为空（仅允许 folder.yml 与保留文件）。
func (c *Collection) folderEmpty(dir string) bool {
	entries, err := os.ReadDir(filepath.Join(c.Dir, filepath.FromSlash(dir)))
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			return false
		}
		if reserved[name] || strings.HasPrefix(name, "folder.") {
			continue
		}
		if ext := strings.ToLower(filepath.Ext(name)); ext == ".yml" || ext == ".yaml" {
			return false
		}
	}
	return true
}

func (c *Collection) folderUID(dir string) (string, bool) {
	for _, ext := range []string{".yml", ".yaml"} {
		data, err := os.ReadFile(filepath.Join(c.Dir, filepath.FromSlash(dir), "folder"+ext))
		if err != nil {
			continue
		}
		var f struct {
			Meta struct {
				UID string `yaml:"uid"`
			} `yaml:"meta"`
		}
		if yaml.Unmarshal(data, &f) == nil && f.Meta.UID != "" {
			return f.Meta.UID, true
		}
	}
	return "", false
}

func (c *Collection) ensureFolderYML(dir, name string) error {
	if _, err := os.Stat(filepath.Join(c.Dir, filepath.FromSlash(dir), "folder.yml")); err == nil {
		return nil
	}
	uid := uuid.NewString()
	if existing, ok := c.folderUID(dir); ok {
		uid = existing
	}
	data, err := yaml.Marshal(map[string]any{
		"info": map[string]any{"name": name, "type": "folder", "seq": 1},
		"meta": map[string]any{"uid": uid},
	})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.Dir, filepath.FromSlash(dir), "folder.yml"), data, 0o644)
}

func (c *Collection) nextSeq(folder string) int {
	entries, err := os.ReadDir(filepath.Join(c.Dir, filepath.FromSlash(folder)))
	if err != nil {
		return 1
	}
	max := 0
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if e.IsDir() || (ext != ".yml" && ext != ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(c.Dir, folder, e.Name()))
		if err != nil {
			continue
		}
		var f struct {
			Info struct {
				Seq int `yaml:"seq"`
			} `yaml:"info"`
		}
		if yaml.Unmarshal(data, &f) == nil && f.Info.Seq > max {
			max = f.Info.Seq
		}
	}
	return max + 1
}

// ReadRequest 按 uid 读取请求。
func (c *Collection) ReadRequest(uid string) (*Request, error) {
	res, err := c.scan()
	if err != nil {
		return nil, err
	}
	r, ok := res.reqs[uid]
	if !ok {
		return nil, errNotFound
	}
	return r, nil
}

// SaveRequest 按 Path 写回请求文件（Path 必须在集合内）。
func (c *Collection) SaveRequest(r *Request) error {
	clean := filepath.Clean(r.Path)
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return fmt.Errorf("非法的请求路径")
	}
	if r.UID == "" {
		return fmt.Errorf("缺少 uid")
	}
	full := filepath.Join(c.Dir, filepath.FromSlash(clean))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	out := r.toFile()
	// 磁盘上已有的未知顶层字段必须保留：Bruno 会忽略它们，但我们不能丢（round-trip）
	// vars/script/assert 已升为 Request 一等字段，由 toFile 写出，不再从旧 Extra 合并回来。
	if prev, err := readRequestFile(full); err == nil {
		extra := map[string]any{}
		for k, v := range prev.Extra {
			if k == "vars" || k == "script" || k == "assert" {
				continue
			}
			extra[k] = v
		}
		out.MergeExtra(extra)
	}
	data, err := yaml.Marshal(out)
	if err != nil {
		return err
	}
	c.ignoreWrite(clean)
	return os.WriteFile(full, data, 0o644)
}

// DeleteRequest 删除请求：文件移入 .trash/（带时间戳，可人工找回）。
// 同时记入 pendingDeletes，供同步引擎推送 tombstone。
func (c *Collection) DeleteRequest(uid string) error {
	res, err := c.scan()
	if err != nil {
		return err
	}
	r, ok := res.reqs[uid]
	if !ok {
		return errNotFound
	}
	if err := c.moveToTrash(filepath.Join(c.Dir, filepath.FromSlash(r.Path))); err != nil {
		return err
	}
	c.recordPendingDelete(uid)
	return nil
}

// ---------- 待推送删除（tombstone）----------

const pendingDeletesFile = ".sync-deletes.json"

// recordPendingDelete 记一条待推送的删除。
func (c *Collection) recordPendingDelete(uid string) {
	if uid == "" {
		return
	}
	list := c.LoadPendingDeletes()
	for _, u := range list {
		if u == uid {
			return
		}
	}
	list = append(list, uid)
	c.SavePendingDeletes(list)
}

// LoadPendingDeletes 读待推送删除列表。
func (c *Collection) LoadPendingDeletes() []string {
	data, err := os.ReadFile(filepath.Join(c.Dir, pendingDeletesFile))
	if err != nil {
		return nil
	}
	var list []string
	_ = json.Unmarshal(data, &list)
	return list
}

// SavePendingDeletes 写待推送删除列表。
func (c *Collection) SavePendingDeletes(list []string) error {
	if len(list) == 0 {
		_ = os.Remove(filepath.Join(c.Dir, pendingDeletesFile))
		return nil
	}
	data, _ := json.MarshalIndent(list, "", "  ")
	return os.WriteFile(filepath.Join(c.Dir, pendingDeletesFile), data, 0o644)
}

// ClearPendingDeletes 清空（推送成功后调用）。
func (c *Collection) ClearPendingDeletes() {
	_ = os.Remove(filepath.Join(c.Dir, pendingDeletesFile))
}

// ---------- 通用 ----------

func (c *Collection) moveToTrash(full string) error {
	trash := filepath.Join(c.Dir, ".trash")
	if err := os.MkdirAll(trash, 0o755); err != nil {
		return err
	}
	return os.Rename(full, filepath.Join(trash, share.TrashName(filepath.Base(full), time.Now())))
}

func joinRel(folder, name string) string {
	if folder == "" {
		return name
	}
	return folder + "/" + name
}
