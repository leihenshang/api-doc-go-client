package collection

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// DocEntry 一条文档条目（B13/B19）：本地任意目录下的 .md 文件，带 YAML frontmatter。
type DocEntry struct {
	UID     string `json:"uid"`
	Name    string `json:"name"`
	Path    string `json:"path"` // 相对集合根（B19：不再限制 docs/ 内）
	Content string `json:"content"`
	Icon    string `json:"icon,omitempty"`
	// Seq 同层排序权重（frontmatter 可选键），与请求的 info.seq 同语义
	Seq int `json:"seq,omitempty"`
}

// docFront 文档 frontmatter。
type docFront struct {
	UID   string         `yaml:"uid"`
	Name  string         `yaml:"name"`
	Icon  string         `yaml:"icon,omitempty"`
	Seq   int            `yaml:"seq,omitempty"`
	Extra map[string]any `yaml:"-"`
}

// docsDir 文档条目目录（B13）。
const docsDir = "docs"

// docHiddenPath 该相对路径的 .md 是否会成为「看不见的文档条目」：子目录里的 README.md。
// 扫描（scanExtAllowed）与所有写入口（CreateDoc / MoveDoc / RenameDoc）共用这一个判定，
// 避免「写进去了却扫不出来」的两处规则漂移。根目录的 README.md 仍可见。
func docHiddenPath(rel string) bool {
	rel = filepath.ToSlash(rel)
	if !strings.EqualFold(filepath.Ext(rel), ".md") {
		return false
	}
	if !strings.EqualFold(path.Base(rel), "README.md") {
		return false
	}
	return path.Dir(rel) != "."
}

// isReadmeDocName 名称是否可以落成 README.md（README / README.md，大小写不敏感）。
func isReadmeDocName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "readme" || n == "readme.md"
}

// isRootRelDir 分组目录是否为集合根（"" 或 "."）。
func isRootRelDir(dir string) bool {
	return path.Clean(filepath.ToSlash(strings.TrimSpace(dir))) == "."
}

// ListDocs 列出全部文档条目。
func (c *Collection) ListDocs() ([]*DocEntry, error) {
	res, err := c.scan()
	if err != nil {
		return nil, err
	}
	out := make([]*DocEntry, 0, len(res.docs))
	for _, d := range res.docs {
		out = append(out, d)
	}
	return out, nil
}

// ReadDoc 按 uid 读一条文档。
func (c *Collection) ReadDoc(uid string) (*DocEntry, error) {
	res, err := c.scan()
	if err != nil {
		return nil, err
	}
	d, ok := res.docs[uid]
	if !ok {
		return nil, errNotFound
	}
	return d, nil
}

// CreateDoc 新建文档条目（uid 为空时自动生成）。
// folder 为空落到 docs/ 默认位（兼容旧行为）；非空为分组目录相对路径（B19：与请求同目录混排）。
func (c *Collection) CreateDoc(name, uid, folder string) (*DocEntry, error) {
	if !validEntryName(name) {
		return nil, fmt.Errorf("名称含非法字符或为空")
	}
	if uid == "" {
		uid = uuid.NewString()
	}
	dir := strings.TrimSpace(folder)
	if dir == "" {
		dir = docsDir
	}
	// 子目录里的 README.md 扫不出来（scanExtAllowed 按规则排除）：
	// 入口直接拒绝，避免造出「文件存在、树里看不见」的文档。根级 README 允许。
	if isReadmeDocName(name) && !isRootRelDir(dir) {
		return nil, fmt.Errorf("子目录里的 README.md 不作为文档条目显示，请先改名再创建")
	}
	if err := os.MkdirAll(filepath.Join(c.Dir, filepath.FromSlash(dir)), 0o755); err != nil {
		return nil, err
	}
	base := sanitizeFileName(name)
	rel := joinRel(dir, base+".md")
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(c.Dir, filepath.FromSlash(rel))); os.IsNotExist(err) {
			break
		}
		rel = joinRel(dir, fmt.Sprintf("%s-%d.md", base, i))
	}
	d := &DocEntry{UID: uid, Name: name, Path: rel, Content: ""}
	if err := c.SaveDoc(d); err != nil {
		return nil, err
	}
	return d, nil
}

// SaveDoc 写回文档文件。Path 必须落在集合目录内（B19：任意目录，防越界写盘）。
func (c *Collection) SaveDoc(d *DocEntry) error {
	if d == nil || d.UID == "" {
		return fmt.Errorf("缺少 uid")
	}
	full, err := c.docPath(d.Path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	c.ignoreWrite(d.Path)
	return writeDocFile(full, d)
}

// DocFileHash 文档文件当前的磁盘 hash（B19：文档不进 SQLite 索引，单独算；
// 取不到（未落盘/扫描失败）返回空串，调用方据此跳过冲突检测）。
func (c *Collection) DocFileHash(uid string) string {
	res, err := c.scan()
	if err != nil {
		return ""
	}
	d, ok := res.docs[uid]
	if !ok {
		return ""
	}
	return c.FileHashOf(d.Path)
}

// SaveDocChecked 带期望哈希的文档保存（B19，与 SaveRequest 的 ExpectHash 同语义）：
// expectHash 非空且与磁盘 hash 不一致时拒写，错误以 conflictMarker 开头，
// 前端据此展示「重新加载 / 另存为副本」而不是普通保存失败。
//
// 比对走 hashMatches（支持 git 风格前缀，≥8 位），与请求/环境一致：
// MCP 的 get_doc 只回显 12 位前缀，若这里要求整串相等，AI 把看到的 hash 传回来必然被拒。
func (c *Collection) SaveDocChecked(d *DocEntry, expectHash string) error {
	if cur := c.DocFileHash(d.UID); cur != "" && !hashMatches(cur, expectHash) {
		return fmt.Errorf("%s 磁盘内容已被其他来源修改", conflictMarker)
	}
	return c.SaveDoc(d)
}

// docPath 校验并解析文档相对路径：必须是集合内的 .md 文件。
//
// 为什么必须校验：DocEntry 经 App 门面由前端 / devserver 桥传入，Path 属于外部输入；
// 不校验就等于提供了「任意路径写 .md」的能力（参见 SaveRequest 的同类校验）。
// B19 起不再限制 docs/ 目录（文档与请求同目录混排），但仍必须留在集合根内。
func (c *Collection) docPath(rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(strings.TrimSpace(rel)))
	if clean == "." || clean == "" || filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("非法的文档路径: %s", rel)
	}
	if !strings.EqualFold(filepath.Ext(clean), ".md") {
		return "", fmt.Errorf("文档必须是 .md 文件: %s", rel)
	}
	full := filepath.Join(c.Dir, clean)
	root := filepath.Clean(c.Dir)
	if full != root && !strings.HasPrefix(full, root+string(filepath.Separator)) {
		return "", fmt.Errorf("文档必须位于集合目录内: %s", rel)
	}
	return full, nil
}

// DeleteDoc 删除文档（移入 .trash），并记一条 type=doc 的待推送墓碑（P1-10）。
func (c *Collection) DeleteDoc(uid string) error {
	res, err := c.scan()
	if err != nil {
		return err
	}
	d, ok := res.docs[uid]
	if !ok {
		return errNotFound
	}
	if err := c.moveToTrash(filepath.Join(c.Dir, filepath.FromSlash(d.Path))); err != nil {
		return err
	}
	c.recordPendingDelete(uid, PendingDeleteDoc)
	return nil
}

// RenameDoc 改文档显示名（文件名不变，只改 frontmatter 的 name）。
func (c *Collection) RenameDoc(uid, name string) error {
	if !validEntryName(name) {
		return fmt.Errorf("名称含非法字符或为空")
	}
	d, err := c.ReadDoc(uid)
	if err != nil {
		return err
	}
	// 子目录里的文档改名为 README 会与「README.md 不显示」的规则混淆：
	// 入口拒绝（文件本来就不叫 README.md，这里拦的是显示名，避免出现同名的隐形语义）。
	if isReadmeDocName(name) && !isRootRelDir(path.Dir(filepath.ToSlash(d.Path))) {
		return fmt.Errorf("子目录里的 README.md 不作为文档条目显示，请换一个名称")
	}
	d.Name = name
	return c.SaveDoc(d)
}

// MoveDoc 把文档移动到目标分组目录（B19）：与 MoveRequest 同语义——目标经
// resolveFolderForMove 解析（拒 ../ 与绝对路径、目录不存在时按需补 folder.yml），
// 改 Path + os.Rename，uid 与文件名主干不变。
func (c *Collection) MoveDoc(uid, destFolder string) error {
	res, err := c.scan()
	if err != nil {
		return err
	}
	d, ok := res.docs[uid]
	if !ok {
		return errNotFound
	}
	dest, err := c.resolveFolderForMove(destFolder)
	if err != nil {
		return err
	}
	oldFull := filepath.Join(c.Dir, filepath.FromSlash(d.Path))
	oldDir := filepath.ToSlash(filepath.Dir(d.Path))
	if oldDir == "." {
		oldDir = ""
	}
	if oldDir == dest {
		return nil // 已在目标分组
	}
	newRel := joinRel(dest, filepath.Base(filepath.FromSlash(d.Path)))
	// 移到子目录后的 README.md 不会出现在树里（扫描排除）：直接拒绝，且不改动文件。
	if docHiddenPath(newRel) {
		return fmt.Errorf("子目录里的 README.md 不作为文档条目显示，请先改名再移动")
	}
	if err := os.MkdirAll(filepath.Join(c.Dir, filepath.FromSlash(dest)), 0o755); err != nil {
		return err
	}
	newFull := filepath.Join(c.Dir, filepath.FromSlash(newRel))
	if _, err := os.Stat(newFull); err == nil {
		return fmt.Errorf("目标分组已存在同名文档")
	}
	// 无 frontmatter 的文档 uid 是由路径哈希出来的（md-xxx）：移动前先把当前 uid 写回文件，
	// 否则 os.Rename 之后路径变了、重新扫描会算出新的 uid，已打开的页签/引用全部失效。
	// 已有合法 frontmatter（含 uid）的文件不动内容。
	if !hasDocFrontmatter(oldFull) {
		c.ignoreWrite(d.Path)
		if err := writeDocFile(oldFull, d); err != nil {
			return fmt.Errorf("写回文档 frontmatter: %w", err)
		}
	}
	c.ignoreWrite(newRel)
	if err := os.Rename(oldFull, newFull); err != nil {
		return fmt.Errorf("移动文档文件: %w", err)
	}
	return nil
}

// hasDocFrontmatter 文件是否已有「可解析且带 uid」的 frontmatter。
// 解析失败（README 里的 Markdown 分隔线等）按「没有 frontmatter」处理，与 parseDocFile 的回落一致。
func hasDocFrontmatter(full string) bool {
	data, err := os.ReadFile(full)
	if err != nil {
		return false
	}
	text := string(data)
	if !strings.HasPrefix(text, "---\n") {
		return false
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return false
	}
	var front docFront
	if err := yaml.Unmarshal([]byte(text[4:4+end]), &front); err != nil {
		return false
	}
	return front.UID != ""
}

// parseDocFile 解析文档字节（frontmatter + 正文）。
//
// 无 frontmatter 或 frontmatter 里没有 uid 的 .md（例如集合根目录的 README.md、
// 手工放进来的笔记）同样视为文档条目：uid 由相对路径哈希生成（"md-" + sha256 前 16 位），
// 同一个文件每次扫描得到同一个 uid；首次保存（SaveDoc → writeDocFile）会把带该 uid 的
// frontmatter 写回文件，此后即与手写 frontmatter 的文档完全一致。
//
// frontmatter 解析失败时**不丢弃整条文档**：README 里出现 Markdown 分隔线（---）时，
// 首个 `---\n...\n---` 之间的内容往往不是合法 YAML mapping，若直接报错，文件会从树里
// 静默消失。这里回落为「全文即正文、无 frontmatter」，继续走路径哈希 uid。
func parseDocFile(rel string, data []byte) (*DocEntry, error) {
	text := string(data)
	var front docFront
	body := text
	// frontmatter: ---\n...\n---
	if strings.HasPrefix(text, "---\n") {
		if end := strings.Index(text[4:], "\n---\n"); end >= 0 {
			var parsed docFront
			if err := yaml.Unmarshal([]byte(text[4:4+end]), &parsed); err == nil {
				front = parsed
				body = text[4+end+5:]
			}
		}
	}
	relSlash := filepath.ToSlash(rel)
	uid := front.UID
	if uid == "" {
		sum := sha256.Sum256([]byte(relSlash))
		uid = "md-" + hex.EncodeToString(sum[:])[:16]
	}
	name := front.Name
	if name == "" {
		base := filepath.Base(relSlash)
		name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	return &DocEntry{
		UID: uid, Name: name, Path: relSlash,
		Content: body, Icon: front.Icon, Seq: front.Seq,
	}, nil
}

// writeDocFile 写文档文件。
func writeDocFile(full string, d *DocEntry) error {
	fm := docFront{UID: d.UID, Name: d.Name, Icon: d.Icon, Seq: d.Seq}
	fmData, err := yaml.Marshal(fm)
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.Write(fmData)
	b.WriteString("---\n")
	b.WriteString(d.Content)
	return writeFileAtomic(full, []byte(b.String()), 0o644)
}

// ---- 附件离线映射（R9）----

// assetsDir 本地附件目录（按内容 hash 命名）。
const assetsDir = "assets"

// SaveAsset 保存附件（按 sha256 命名，去重）。
func (c *Collection) SaveAsset(hash string, data []byte) (string, error) {
	if hash == "" {
		sum := sha256.Sum256(data)
		hash = hex.EncodeToString(sum[:])
	}
	dir := filepath.Join(c.Dir, assetsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	full := filepath.Join(dir, hash)
	if _, err := os.Stat(full); err == nil {
		return full, nil // 已存在
	}
	if err := writeFileAtomic(full, data, 0o644); err != nil {
		return "", err
	}
	return full, nil
}

// AssetPath 本地附件路径。
func (c *Collection) AssetPath(hash string) string {
	return filepath.Join(c.Dir, assetsDir, hash)
}

// HasAsset 附件是否已本地化。
func (c *Collection) HasAsset(hash string) bool {
	if hash == "" {
		return false
	}
	_, err := os.Stat(c.AssetPath(hash))
	return err == nil
}

// DocImageURLs 提取文档正文里的图片 URL（http(s) 开头或 /uploads/ 开头）。
func DocImageURLs(content string) []string {
	var out []string
	// 简单匹配 ![](...) 与 <img src="...">
	for _, m := range imageURLPattern.FindAllStringSubmatch(content, -1) {
		u := m[1]
		if u != "" {
			out = append(out, u)
		}
	}
	return out
}

// imageURLPattern Markdown/HTML 图片地址。
var imageURLPattern = regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)|<img[^>]+src=["']([^"']+)["']`)
