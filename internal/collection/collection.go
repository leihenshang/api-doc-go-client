// Package collection 客户端集合的文件层：本地目录即集合（Bruno 风格），
// 文件是真相源。目录布局见设计文档《客户端与同步架构设计》§3.1。
package collection

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	share "api-doc-go-share/collection"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// 集合内的保留目录/文件（不参与扫描与树渲染）。
var reserved = map[string]bool{
	".trash": true, ".conflicts": true, "assets": true,
	"node_modules": true, "environments": true, "docs": true,
	"examples": true, // 保存的响应示例（example.go），不是集合条目
}

var errNotFound = errors.New("条目不存在")

// Collection 一个已打开的集合目录。
type Collection struct {
	Dir  string // 绝对路径
	UID  string
	Name string
}

// ---------- 集合清单（opencollection.yml） ----------

const gitignoreContent = "# 客户端自动维护\n" +
	"environments/*.secrets.yml\n*.local.yml\n.trash/\n.conflicts/\nnode_modules/\n"

// Open 打开（不存在则初始化）一个集合目录。
func Open(dir string) (*Collection, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
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
	return c, nil
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
	return f
}

func fromFile(path string, f *requestFile) *Request {
	return &Request{
		UID: f.Meta.UID, Name: f.Info.Name, Seq: f.Info.Seq, Path: path,
		Method: strings.ToUpper(f.HTTP.Method), URL: f.HTTP.URL,
		Params: f.HTTP.Params, Headers: f.HTTP.Headers, Body: f.HTTP.Body,
		Auth: f.HTTP.Auth, Settings: f.Settings,
		Docs: f.Docs, BaseRev: f.Meta.BaseRev,
	}
}

// ---------- 扫描与树 ----------

// scanResult 一次目录扫描的产物。
type scanResult struct {
	reqs      map[string]*Request // uid → request
	reqByPath map[string]*Request
	uidByDir  map[string]string // 目录相对路径 → folder.yml 的 uid
	nameByDir map[string]string // 目录相对路径 → folder.yml 的显示名（重命名后与目录名不同）
}

func (c *Collection) scan() (*scanResult, error) {
	res := &scanResult{
		reqs: map[string]*Request{}, reqByPath: map[string]*Request{},
		uidByDir: map[string]string{}, nameByDir: map[string]string{},
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
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// Tree 扫描集合并构建树（分组在前，组内按 seq / 名称排序）。
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
func (c *Collection) CreateRequest(folder, name, method string) (*Request, error) {
	if !validEntryName(name) {
		return nil, fmt.Errorf("名称含非法字符或为空")
	}
	if folder != "" {
		clean := filepath.Clean(folder)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return nil, fmt.Errorf("非法的分组路径")
		}
		if err := os.MkdirAll(filepath.Join(c.Dir, filepath.FromSlash(clean)), 0o755); err != nil {
			return nil, err
		}
		if _, ok := c.folderUID(clean); !ok {
			if err := c.ensureFolderYML(clean, filepath.Base(clean)); err != nil {
				return nil, err
			}
		}
	}
	// 生成不冲突的文件名
	base := sanitizeFileName(name)
	rel := joinRel(folder, base+".yml")
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(c.Dir, filepath.FromSlash(rel))); os.IsNotExist(err) {
			break
		}
		rel = joinRel(folder, fmt.Sprintf("%s-%d.yml", base, i))
	}
	seq := c.nextSeq(folder)
	r := &Request{
		UID: uuid.NewString(), Name: name, Seq: seq, Path: rel,
		Method: strings.ToUpper(strings.TrimSpace(method)), URL: "{{host}}/",
		Params:  []KV{{Enabled: true}},
		Headers: []KV{{Name: "Content-Type", Value: "application/json", Enabled: true}},
		Body:    Body{Type: "none"},
	}
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
	if prev, err := readRequestFile(full); err == nil {
		out.MergeExtra(prev.Extra)
	}
	data, err := yaml.Marshal(out)
	if err != nil {
		return err
	}
	return os.WriteFile(full, data, 0o644)
}

// DeleteRequest 删除请求：文件移入 .trash/（带时间戳，可人工找回）。
func (c *Collection) DeleteRequest(uid string) error {
	res, err := c.scan()
	if err != nil {
		return err
	}
	r, ok := res.reqs[uid]
	if !ok {
		return errNotFound
	}
	return c.moveToTrash(filepath.Join(c.Dir, filepath.FromSlash(r.Path)))
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
