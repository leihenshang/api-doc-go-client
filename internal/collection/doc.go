package collection

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// DocEntry 一条文档条目（B13）：本地落 docs/<name>.md，带 YAML frontmatter。
type DocEntry struct {
	UID     string `json:"uid"`
	Name    string `json:"name"`
	Path    string `json:"path"` // 相对集合根
	Content string `json:"content"`
	Icon    string `json:"icon,omitempty"`
}

// docFront 文档 frontmatter。
type docFront struct {
	UID   string         `yaml:"uid"`
	Name  string         `yaml:"name"`
	Icon  string         `yaml:"icon,omitempty"`
	Extra map[string]any `yaml:"-"`
}

// docsDir 文档条目目录（B13）。
const docsDir = "docs"

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
func (c *Collection) CreateDoc(name, uid string) (*DocEntry, error) {
	if !validEntryName(name) {
		return nil, fmt.Errorf("名称含非法字符或为空")
	}
	if uid == "" {
		uid = uuid.NewString()
	}
	if err := os.MkdirAll(filepath.Join(c.Dir, docsDir), 0o755); err != nil {
		return nil, err
	}
	base := sanitizeFileName(name)
	rel := filepath.ToSlash(filepath.Join(docsDir, base+".md"))
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(c.Dir, filepath.FromSlash(rel))); os.IsNotExist(err) {
			break
		}
		rel = filepath.ToSlash(filepath.Join(docsDir, fmt.Sprintf("%s-%d.md", base, i)))
	}
	d := &DocEntry{UID: uid, Name: name, Path: rel, Content: ""}
	if err := c.SaveDoc(d); err != nil {
		return nil, err
	}
	return d, nil
}

// SaveDoc 写回文档文件。Path 必须落在集合的 docs/ 目录内（防越界写盘）。
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

// docPath 校验并解析文档相对路径：必须是集合内 docs/ 下的 .md 文件。
//
// 为什么必须校验：DocEntry 经 App 门面由前端 / devserver 桥传入，Path 属于外部输入；
// 不校验就等于提供了「任意路径写 .md」的能力（参见 SaveRequest 的同类校验）。
func (c *Collection) docPath(rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(strings.TrimSpace(rel)))
	if clean == "." || clean == "" || filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("非法的文档路径: %s", rel)
	}
	if !strings.EqualFold(filepath.Ext(clean), ".md") {
		return "", fmt.Errorf("文档必须是 .md 文件: %s", rel)
	}
	full := filepath.Join(c.Dir, clean)
	base := filepath.Clean(filepath.Join(c.Dir, docsDir))
	if full != base && !strings.HasPrefix(full, base+string(filepath.Separator)) {
		return "", fmt.Errorf("文档必须位于 %s 目录内: %s", docsDir, rel)
	}
	return full, nil
}

// DeleteDoc 删除文档（移入 .trash）。
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
	c.recordPendingDelete(uid)
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
	d.Name = name
	return c.SaveDoc(d)
}

// readDocFile 解析文档文件（frontmatter + 正文）。
func readDocFile(path string) (*DocEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseDocFile(path, data)
}

// parseDocFile 解析文档字节。
func parseDocFile(rel string, data []byte) (*DocEntry, error) {
	text := string(data)
	var front docFront
	body := text
	// frontmatter: ---\n...\n---
	if strings.HasPrefix(text, "---\n") {
		end := strings.Index(text[4:], "\n---\n")
		if end >= 0 {
			fm := text[4 : 4+end]
			if err := yaml.Unmarshal([]byte(fm), &front); err != nil {
				return nil, err
			}
			body = text[4+end+5:]
		}
	}
	if front.UID == "" {
		// 无 uid 的文档文件不是同步条目，跳过
		return nil, fmt.Errorf("缺少 uid")
	}
	name := front.Name
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(rel), ".md")
	}
	return &DocEntry{
		UID: front.UID, Name: name, Path: filepath.ToSlash(rel),
		Content: body, Icon: front.Icon,
	}, nil
}

// writeDocFile 写文档文件。
func writeDocFile(full string, d *DocEntry) error {
	fm := docFront{UID: d.UID, Name: d.Name, Icon: d.Icon}
	fmData, err := yaml.Marshal(fm)
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.Write(fmData)
	b.WriteString("---\n")
	b.WriteString(d.Content)
	return os.WriteFile(full, []byte(b.String()), 0o644)
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
	if err := os.WriteFile(full, data, 0o644); err != nil {
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
