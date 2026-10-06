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
	"regexp"
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
	idx  *index.DB // 本地 SQLite 索引（C1）；惰性打开
	// indexKey 索引文件名用的键，空 = 用 UID（见 SetIndexKey：多根并存时同 uid 的拷贝目录要区分开）
	indexKey string
	w        *watch.Watcher // 外部改动监听（D1）；惰性启动
	// grpcDefault 集合级默认 gRPC 定义（清单里的 grpc 段，P8）；nil = 未配置。
	// 只有在内存里保留它，writeManifest 才不会把清单里的这一段写丢。
	grpcDefault *share.GRPCDefault
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
		c.grpcDefault = m.GRPC
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

// WatchDone 监听器关闭信号（未启动时返回 nil）。
// 事件通道在 Close 后不会再关闭，消费方（watchLoop）靠它退出，避免 goroutine 永久阻塞。
func (c *Collection) WatchDone() <-chan struct{} {
	if c.w == nil {
		return nil
	}
	return c.w.Done()
}

// StopWatch 停止监听。
func (c *Collection) StopWatch() {
	if c.w != nil {
		_ = c.w.Close()
		c.w = nil
	}
}

// Close 释放集合占用的资源：停止文件监听并关闭索引句柄；可重复调用。
//
// 为什么要显式关闭：索引是常驻的 sqlite 句柄，Windows 上被句柄占住的文件删不掉 ——
// 关闭一个工作目录（多根并存）或退出应用时不释放，用户删目录 / 测试清理临时目录都会失败。
// 关闭后再用会自动重开（ensureIndex），所以调用方不必关心后续是否还会访问。
func (c *Collection) Close() error {
	c.StopWatch()
	return c.closeIndex()
}

// ignoreWrite 自写回环：写盘前短暂屏蔽对该相对路径的监听。
// 拿不到写入内容时用它（窗口内一律忽略）。
func (c *Collection) ignoreWrite(rel string) {
	if c.w != nil {
		c.w.Ignore(rel, 400*time.Millisecond)
	}
}

// ignoreWriteContent 自写回环（带内容指纹）：窗口内只有「文件内容仍等于我方写入的那份」
// 才忽略；同一窗口里别人改的内容照常上报（否则别人的改动会被静默吞掉）。
func (c *Collection) ignoreWriteContent(rel string, content []byte) {
	if c.w != nil {
		c.w.IgnoreWrite(rel, sha256Hex(content), 400*time.Millisecond)
	}
}

// ---------- 本地索引（C1） ----------

// indexPath 索引文件放在用户配置目录（不污染集合、不进 git）。
//
// UID 来自集合清单（不可信输入），必须转成安全文件名后才能拼路径：否则
// `meta.uid: ../../x` 会把 sqlite 建到配置目录之外（MkdirAll + 建表 = 越界写盘）。
func (c *Collection) indexPath() string {
	key := c.UID
	if c.indexKey != "" {
		key = c.indexKey
	}
	name := safeIndexName(key) + ".sqlite"
	base, err := config.Dir()
	if err != nil {
		return filepath.Join(c.Dir, ".index.sqlite")
	}
	return filepath.Join(base, "index", name)
}

// SetIndexKey 覆盖索引文件用的键（默认用集合 uid）。
//
// 为什么需要：索引按「集合 uid」落一个 sqlite 文件，而两个工作目录完全可能是同一份集合的
// 拷贝（uid 相同、路径不同）—— 共用同一个索引文件会让两边的搜索结果 / 同步哈希互相污染。
// App 在多根并存时检测到 uid 撞车，就给后打开的那个根换一个区分用的键。
//
// 调用后索引句柄会关闭（键变了，旧句柄指向的文件已经不对），下次访问会自动按新键重开；
// 调用方通常紧接着重建一次索引，避免读到旧键留下的行。
func (c *Collection) SetIndexKey(key string) {
	if key == c.indexKey {
		return
	}
	c.indexKey = key
	_ = c.closeIndex()
}

// closeIndex 关闭索引句柄（文件保留）；返回关闭错误。
func (c *Collection) closeIndex() error {
	if c.idx == nil {
		return nil
	}
	err := c.idx.Close()
	c.idx = nil
	return err
}

// indexNameAllowed 索引文件名的合法字符（与 uuid 的字符集一致）。
var indexNameAllowed = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// safeIndexName 把 UID 转成可安全用作文件名的字符串：
// 合法则原样返回（保持既有索引文件不被弃用），否则退化为 UID 的哈希（稳定且不含路径分隔符）。
func safeIndexName(uid string) string {
	if indexNameAllowed.MatchString(uid) && uid != "." && uid != ".." {
		return uid
	}
	sum := sha256.Sum256([]byte(uid))
	return "u" + hex.EncodeToString(sum[:16])
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
	return sha256Hex(data)
}

// sha256Hex 内容哈希（index 与冲突检测共用）。
func sha256Hex(data []byte) string {
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

// DirtyNodesFast 只读索引里的 dirty 集，不做全量重建（不 walk 目录、不读请求文件）。
//
// 供高频轮询使用（状态栏的 GetSyncStatus）：RebuildIndex 会遍历整个集合并逐个读 yml，
// 一旦被轮询反复调用，大集合下会明显卡顿。索引本身由文件监听与每次保存维护，通常是最新的。
func (c *Collection) DirtyNodesFast() ([]index.Node, error) {
	db, err := c.ensureIndex()
	if err != nil {
		return nil, err
	}
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

// EnsureManifest 落一份 opencollection.yml（已存在则按当前内存值重写）。
//
// 供「创建项目」这类先建目录、后写内容的场景用：collection.Open 只在目录不存在时
// 建目录，manifest 要等第一次写请求时才生成；提前落盘能让新项目立刻出现在列表里。
func (c *Collection) EnsureManifest() error { return c.writeManifest() }

// writeManifest 落盘集合清单。
//
// 以「读改写」的方式更新既有文件（而不是按已知字段整份重写）：清单里可能有本客户端
// 不认识的键（未来的字段、其它工具的元数据）与键顺序、注释，整份重写会静默丢掉它们。
func (c *Collection) writeManifest() error {
	path := filepath.Join(c.Dir, "opencollection.yml")
	doc, err := loadManifestDoc(path)
	if err != nil {
		return err
	}
	doc.setScalar("opencollection", manifestVersion)
	setMapScalar(doc.ensureMap("info"), "name", c.Name)
	setMapScalar(doc.ensureMap("meta"), "uid", c.UID)
	// 集合级默认定义要写回去，否则每次开集合都会被清掉；未配置时删掉该段
	if c.grpcDefault == nil {
		doc.remove("grpc")
	} else if node, err := encodeYAMLValue(c.grpcDefault); err != nil {
		return fmt.Errorf("序列化集合级 gRPC 定义: %w", err)
	} else {
		doc.setNode("grpc", node)
	}
	data, err := yaml.Marshal(doc.root)
	if err != nil {
		return fmt.Errorf("序列化集合清单: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// Meta 集合身份（清单里的名称与 uid）。
type Meta struct {
	Name string
	UID  string
}

// ReadMeta 只读集合清单，不建索引、不起文件监听、不写盘。
//
// 用途：扫描项目列表（MCP 的 list_projects）这类只读场景 —— 之前那里走 Open，
// 会对每个项目建索引、写清单、起一个 fsnotify 监听并永不释放（句柄泄漏 + 只读操作有副作用）。
func ReadMeta(dir string) (Meta, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Meta{}, err
	}
	data, err := os.ReadFile(filepath.Join(abs, "opencollection.yml"))
	if err != nil {
		return Meta{}, err
	}
	m, err := share.DecodeManifest(data)
	if err != nil {
		return Meta{}, fmt.Errorf("opencollection.yml 解析失败: %w", err)
	}
	meta := Meta{Name: m.Info.Name, UID: m.Meta.UID}
	if meta.Name == "" {
		meta.Name = filepath.Base(abs)
	}
	return meta, nil
}

// ---------- 清单的 YAML 节点操作（保真读改写） ----------

// manifestDoc 清单的 YAML 节点视图：保留未知键、键顺序与注释。
type manifestDoc struct{ root *yaml.Node }

func loadManifestDoc(path string) (*manifestDoc, error) {
	doc := &manifestDoc{root: &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return doc, nil
		}
		return nil, fmt.Errorf("读取集合清单: %w", err)
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, fmt.Errorf("解析集合清单: %w", err)
	}
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return doc, nil
		}
		node = *node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("集合清单不是键值结构")
	}
	doc.root = &node
	return doc, nil
}

// setMapScalar 在 mapping 节点上写一个字符串值（键不存在则追加到末尾）。
func setMapScalar(n *yaml.Node, key, val string) {
	node := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: val}
	if old := lookupMapKey(n, key); old != nil {
		*old = *node
		return
	}
	n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, node)
}

// lookupMapKey 取 mapping 节点里某个键的值节点。
func lookupMapKey(n *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func (d *manifestDoc) setScalar(key, val string) { setMapScalar(d.root, key, val) }

// ensureMap 取（或新建）一个子 mapping。
func (d *manifestDoc) ensureMap(key string) *yaml.Node {
	if old := lookupMapKey(d.root, key); old != nil && old.Kind == yaml.MappingNode {
		return old
	}
	d.setNode(key, &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
	return lookupMapKey(d.root, key)
}

// setNode 写入任意节点（键不存在则追加）。
func (d *manifestDoc) setNode(key string, node *yaml.Node) {
	if old := lookupMapKey(d.root, key); old != nil {
		*old = *node
		return
	}
	d.root.Content = append(d.root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, node)
}

func (d *manifestDoc) remove(key string) {
	for i := 0; i+1 < len(d.root.Content); i += 2 {
		if d.root.Content[i].Value == key {
			d.root.Content = append(d.root.Content[:i], d.root.Content[i+2:]...)
			return
		}
	}
}

// encodeYAMLValue 把一个结构体编码成 YAML 节点（用于写回 grpc 段）。
func encodeYAMLValue(v any) (*yaml.Node, error) {
	data, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, err
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return node.Content[0], nil
	}
	return &node, nil
}

// ---------- 集合级默认 gRPC 定义（P8） ----------

// GrpcDefault 当前集合级默认 gRPC 定义（未配置时为 nil）。
func (c *Collection) GrpcDefault() *share.GRPCDefault {
	if c.grpcDefault == nil {
		return nil
	}
	cp := *c.grpcDefault
	cp.Imports = append([]string(nil), c.grpcDefault.Imports...)
	return &cp
}

// SetGrpcDefault 写集合级默认 gRPC 定义（proto 为空 = 清除并删掉清单里的 grpc 段）。
func (c *Collection) SetGrpcDefault(proto string, imports []string) error {
	proto = strings.TrimSpace(proto)
	if proto == "" {
		c.grpcDefault = nil
		return c.writeManifest()
	}
	c.grpcDefault = &share.GRPCDefault{Proto: proto, Imports: cleanPaths(imports)}
	return c.writeManifest()
}

// applyGrpcDefault 请求自身没写定义时回落到集合级默认定义（P8）。
// 只在内存里生效：写盘时 stripGrpcDefault 又会把它省略掉，读回照旧回落（round-trip 稳定）。
func (c *Collection) applyGrpcDefault(r *Request) {
	if r == nil || r.GRPC == nil || c.grpcDefault == nil {
		return
	}
	if strings.TrimSpace(r.GRPC.Proto) != "" {
		return
	}
	r.GRPC.Proto = c.grpcDefault.Proto
	if len(r.GRPC.Imports) == 0 {
		r.GRPC.Imports = append([]string(nil), c.grpcDefault.Imports...)
	}
}

// stripGrpcDefault 写盘时省略与集合默认定义完全一致的 proto/imports（P8：请求只写 service/method）。
// 注意 GRPC 是共享指针：这里必须先复制再改，避免把内存里的请求也改空。
func (c *Collection) stripGrpcDefault(f *requestFile) {
	if f == nil || f.GRPC == nil || c.grpcDefault == nil {
		return
	}
	if strings.TrimSpace(f.GRPC.Proto) != strings.TrimSpace(c.grpcDefault.Proto) {
		return
	}
	if !samePaths(f.GRPC.Imports, c.grpcDefault.Imports) {
		return
	}
	cp := *f.GRPC
	cp.Proto, cp.Imports = "", nil
	f.GRPC = &cp
}

// cleanPaths 去空白、去重（保持顺序），并统一成正斜杠。
func cleanPaths(list []string) []string {
	out := make([]string, 0, len(list))
	for _, p := range list {
		p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
		if p == "" {
			continue
		}
		dup := false
		for _, item := range out {
			if item == p {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, p)
		}
	}
	return out
}

// samePaths 两个路径列表是否等价（忽略顺序与重复）。
func samePaths(a, b []string) bool {
	ca, cb := cleanPaths(a), cleanPaths(b)
	if len(ca) != len(cb) {
		return false
	}
	for _, p := range ca {
		found := false
		for _, q := range cb {
			if p == q {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
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
	f.Info.Name, f.Info.Seq = r.Name, r.Seq
	f.Meta.UID, f.Meta.BaseRev = r.UID, r.BaseRev
	if r.IsGRPC() {
		// gRPC：只写 grpc 段（http 段为空 → 靠 HTTPBlock.IsZero + omitempty 整段省略）
		f.Info.Type = TypeGRPC
		f.GRPC = r.GRPC
	} else {
		f.Info.Type = TypeHTTP
		f.HTTP.Method, f.HTTP.URL = r.Method, r.URL
		f.HTTP.Params, f.HTTP.Headers, f.HTTP.Body = r.Params, r.Headers, r.Body
		f.HTTP.Auth = r.Auth
	}
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
		Settings: f.Settings, Docs: f.Docs, BaseRev: f.Meta.BaseRev,
	}
	r.VarsPreRequest, r.Script, r.Asserts = parseScriptExtra(f.Extra)

	if f.Info.Type == TypeGRPC || f.GRPC != nil {
		r.GRPC = f.GRPC
		// Method/URL 为派生值（不落盘）：索引、历史、搜索、同步都按这两个字段工作。
		r.Method = MethodGRPC
		if f.GRPC != nil {
			r.URL = GrpcURL(f.GRPC.Target, f.GRPC.Service, f.GRPC.Method)
		}
		return r
	}
	r.Method = strings.ToUpper(f.HTTP.Method)
	r.URL = f.HTTP.URL
	r.Params, r.Headers, r.Body = f.HTTP.Params, f.HTTP.Headers, f.HTTP.Body
	r.Auth = f.HTTP.Auth
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

// conflictMarker 冲突错误的识别标记：写盘时发现磁盘内容已被别人改动，错误文本以它开头。
// 前端据此展示「重新加载 / 另存为副本」而不是普通保存失败提示。
const conflictMarker = "[conflict]"

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
		dir := filepath.ToSlash(filepath.Dir(rel))
		if dir == "." {
			dir = ""
		}
		// 扩展名白名单：yml/yaml 是请求与环境文件；docs/*.md 是文档条目（B13）——
		// 之前这里把 .md 一并挡掉，导致 ListDocs 永远扫不到任何文档（含客户端自己创建的）。
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".yml" && ext != ".yaml" && !(ext == ".md" && dir == docsDir) {
			return nil
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
		case TypeHTTP, TypeGRPC:
			var f requestFile
			if yaml.Unmarshal(data, &f) != nil || f.Meta.UID == "" {
				return nil
			}
			r := fromFile(rel, &f)
			c.applyGrpcDefault(r) // 没写 proto 的 gRPC 请求回落到集合默认定义（P8）
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
		p.children = append(p.children, &Node{Type: "folder", UID: folderUIDOrFallback(res.uidByDir[dir], dir), Name: name, Path: dir, Children: nil})
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

// SaveEnv 保存环境（不做并发校验；界面单写者场景够用）。
// 需要「别人是否在我读之后改过」保护的调用方走 SaveEnvChecked。
func (c *Collection) SaveEnv(env Env) error { return c.SaveEnvChecked(env, "") }

// EnvFileHash 返回环境主文件的内容哈希（不存在返回空串）。
//
// 给并发保护用：环境是「整份重写」，调用方先读后写的窗口里若被别人改过，
// 直接写回会静默抹掉对方的改动。读到的哈希留着，写入时校验即可发现。
// 只哈希主文件 *.yml：secrets 文件只随 secret 变量增删一起重写，冲突时主文件哈希已能反映。
func (c *Collection) EnvFileHash(name string) string {
	if !validEnvName(name) {
		return ""
	}
	mainP, _ := c.envPaths(name)
	return fileHash(mainP)
}

// SaveEnvChecked 保存环境：secret 变量的值写入 *.secrets.yml（不进可提交文件），主文件留占位。
//
// expectHash 非空时先校验磁盘内容是否仍是该哈希（支持 git 风格前缀，≥8 位即可），
// 不一致直接返回带 conflictMarker 的错误且**不落盘**，避免覆盖别人的改动。
func (c *Collection) SaveEnvChecked(env Env, expectHash string) error {
	if !validEnvName(env.Name) {
		return fmt.Errorf("环境名只能包含字母、数字、- 与 _")
	}
	if expectHash != "" {
		mainP, _ := c.envPaths(env.Name)
		if h := fileHash(mainP); h != "" && !hashMatches(h, expectHash) {
			return fmt.Errorf("%s 环境 %s 的文件已变化（可能客户端界面或另一个 AI 会话刚改过），"+
				"请重新 list_envs 读取后合并，或换个环境名重试", conflictMarker, env.Name)
		}
	}
	return c.saveEnv(env)
}

// saveEnv 真正落盘（调用方已做完名字与冲突校验）。
func (c *Collection) saveEnv(env Env) error {
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

// RenameEnv 给环境改名：读旧（含 secrets 合并）→ 写新名 → 旧的两个文件移入 .trash/。
//
// 为什么在 collection 层做、而不是让调用方「SaveEnv(新名) + DeleteEnv(旧名)」：
//
//	① 并发校验要校验**旧名**文件的哈希。若放在调用层，SaveEnvChecked 收到的 Env.Name 已经是
//	   新名，它会去看新文件（不存在 → 哈希为空 → 校验被静默跳过），保护形同虚设；
//	② 旧名与新名在大小写不敏感的文件系统上是同一个文件时，写新名等于原地覆盖旧文件，
//	   紧接着删旧名就把环境删没了。这里直接拒绝；
//	③ 环境的两个文件必须同时搬，只有这一层知道它们的布局。
func (c *Collection) RenameEnv(oldName, newName, expectHash string) error {
	if !validEnvName(oldName) || !validEnvName(newName) {
		return fmt.Errorf("环境名只能包含字母、数字、- 与 _")
	}
	oldMain, _ := c.envPaths(oldName)
	newMain, _ := c.envPaths(newName)
	if _, err := os.Stat(oldMain); err != nil {
		return fmt.Errorf("找不到环境 %s", oldName)
	}
	// 大小写不敏感的文件系统上，dev → DEV 是同一个文件：先写后删等于让环境凭空消失。
	if strings.EqualFold(oldMain, newMain) {
		return fmt.Errorf("新环境名 %s 与原名在当前文件系统上是同一个文件，改名不会生效", newName)
	}
	if _, err := os.Stat(newMain); err == nil {
		return fmt.Errorf("环境名 %s 已被占用", newName)
	}
	if expectHash != "" {
		if h := fileHash(oldMain); h != "" && !hashMatches(h, expectHash) {
			return fmt.Errorf("%s 环境 %s 的文件已变化（可能客户端界面或另一个 AI 会话刚改过），"+
				"请重新 list_envs 读取后合并，或换个环境名重试", conflictMarker, oldName)
		}
	}
	env, err := c.readEnv(oldName)
	if err != nil {
		return err
	}
	env.Name = newName
	if err := c.saveEnv(*env); err != nil {
		return err
	}
	// 新文件已落盘后才删旧的：中途失败最坏是「两个环境都在」，不会丢数据。
	return c.DeleteEnv(oldName)
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
	r := &Request{
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
	if src.IsGRPC() {
		// gRPC 草稿：原样带上 grpc 段；Method/URL 由集合层派生（供索引/历史/搜索使用）
		r.GRPC = src.GRPC
		r.Method = MethodGRPC
		r.URL = GrpcURL(src.GRPC.Target, src.GRPC.Service, src.GRPC.Method)
		return c.saveNewRequest(folder, name, r)
	}
	r.Method = strings.ToUpper(strings.TrimSpace(src.Method))
	if r.Method == "" {
		r.Method = "GET"
	}
	r.URL = strings.TrimSpace(src.URL)
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
	dest, err := c.resolveFolderForMove(destFolder)
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
	dest, err := c.resolveFolderForMove(destParent)
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
// folderFallbackPrefix 给「没有 folder.yml 的分组」用的 uid 前缀。
// 这类目录（外部创建、手工建目录、旧集合）在集合树里同样显示为分组，
// 若 uid 留空，界面上的重命名/移动/删除都会失败（uid 为空 → 后端找不到目录）。
const folderFallbackPrefix = "dir:"

// folderUIDOrFallback folder.yml 里的 uid 优先；没有就用 "dir:<相对路径>" 作为稳定标识。
func folderUIDOrFallback(uid, dir string) string {
	if uid != "" {
		return uid
	}
	return folderFallbackPrefix + dir
}

// resolveFolderUID 把分组 uid 解析成目录相对路径：真实 uid 查 uidByDir，
// 兜底 uid（"dir:<路径>"）按**磁盘目录是否存在**判断 —— 这类目录没有 folder.yml，
// 扫描结果里没有它的 nameByDir 记录，所以不能拿扫描结果当依据。
func (c *Collection) resolveFolderUID(res *scanResult, uid string) (string, bool) {
	if strings.HasPrefix(uid, folderFallbackPrefix) {
		dir := strings.TrimPrefix(uid, folderFallbackPrefix)
		if dir == "" {
			return "", false
		}
		if st, err := os.Stat(filepath.Join(c.Dir, filepath.FromSlash(dir))); err == nil && st.IsDir() {
			return dir, true
		}
		return "", false
	}
	for dir, u := range res.uidByDir {
		if u == uid {
			return dir, true
		}
	}
	return "", false
}

func (c *Collection) resolveFolder(parent string) (string, error) {
	clean, err := normalizeFolderPath(parent)
	if err != nil {
		return "", err
	}
	if clean == "" {
		return "", nil
	}
	if _, ok := c.folderUID(clean); !ok {
		return "", fmt.Errorf("分组不存在: %s", clean)
	}
	return clean, nil
}

// resolveFolderForMove 解析「移动目标分组」：比 resolveFolder 宽松一档 ——
// 磁盘上**已存在的目录**（集合树里能看到、但可能缺 folder.yml 的分组，例如外部创建或
// 手工建目录的场景）也算合法目标，此时按需补一个 folder.yml 让分组身份成立。
// 为什么需要：树里显示得出来、拖进去却报「分组不存在」是不一致的行为。
func (c *Collection) resolveFolderForMove(parent string) (string, error) {
	clean, err := normalizeFolderPath(parent)
	if err != nil {
		return "", err
	}
	if clean == "" {
		return "", nil
	}
	if _, ok := c.folderUID(clean); ok {
		return clean, nil
	}
	if st, statErr := os.Stat(filepath.Join(c.Dir, filepath.FromSlash(clean))); statErr == nil && st.IsDir() {
		if err := c.ensureDir(clean); err != nil {
			return "", err
		}
		return clean, nil
	}
	return "", fmt.Errorf("分组不存在: %s", clean)
}

// normalizeFolderPath 规范化分组路径："" / "." / "/" 归一为根（空串），并挡掉 ../ 与绝对路径。
func normalizeFolderPath(parent string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(parent)))
	if clean == "." || clean == "/" {
		return "", nil
	}
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("非法的分组路径")
	}
	return clean, nil
}

func (c *Collection) findDir(uid string) (string, error) {
	res, err := c.scan()
	if err != nil {
		return "", err
	}
	if dir, ok := c.resolveFolderUID(res, uid); ok {
		return dir, nil
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
	// 冲突检测：客户端带 hash 来时，磁盘内容必须还是它读到的那份，否则拒写（不覆盖别人的改动）
	if r.ExpectHash != "" {
		if h := fileHash(full); h != "" && !hashMatches(h, r.ExpectHash) {
			return fmt.Errorf("%s 磁盘上的文件已变化（可能被其它编辑器、另一个客户端窗口或同步改过），"+
				"请「重新加载」后合并，或「另存为副本」再保存", conflictMarker)
		}
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	out := r.toFile()
	c.stripGrpcDefault(out) // 与集合默认定义一致的 proto/imports 不写盘（P8）
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
	c.ignoreWriteContent(clean, data)
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return err
	}
	c.refreshIndexNode(r, full, data)
	return nil
}

// hashMatches 比对「期望哈希」与磁盘实际哈希，支持 git 风格前缀（minHashPrefix 个字符以上即可）。
//
// 为什么允许前缀：界面与 MCP 工具的输出里 hash 只展示前 12 位（64 位全串太长、噪音大），
// 若这里只做全等比较，调用方把展示出来的前缀原样传回来就会永远「冲突」，保护形同虚设。
func hashMatches(actual, expect string) bool {
	if expect == "" {
		return true
	}
	if len(expect) >= len(actual) {
		return actual == expect
	}
	if len(expect) < minHashPrefix {
		return false // 前缀太短容易误判成「没冲突」，宁可直接报冲突
	}
	return strings.HasPrefix(actual, expect)
}

// minHashPrefix 允许的最短哈希前缀长度。
const minHashPrefix = 8

// refreshIndexNode 写盘后把索引里的 hash/mtime 更新成新值。
//
// 必要性：文件监听会忽略「自己刚写的路径」（ignoreWrite），所以索引不会因为我们自己的保存而更新；
// 若不同步，前端下一次读到的 hash 仍是旧值 → 下一次保存会被误判成外部改动（假冲突）。
func (c *Collection) refreshIndexNode(r *Request, full string, data []byte) {
	if c.idx == nil {
		return
	}
	// 索引里还没有（新创建的请求）也要写进去，否则它要等下次全量重建才能被搜索到
	n, ok, err := c.idx.Get(r.UID)
	if err != nil || !ok {
		n = index.Node{UID: r.UID, Type: "request", Path: filepath.ToSlash(r.Path)}
	}
	n.Hash = sha256Hex(data)
	if st, serr := os.Stat(full); serr == nil {
		n.MTime = st.ModTime().UnixMilli()
	}
	n.Title = r.Name
	n.Method = r.Method
	n.URL = r.URL
	_ = c.idx.Upsert(n)
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
