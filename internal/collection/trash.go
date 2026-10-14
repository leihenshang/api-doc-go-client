package collection

// 回收站列出与恢复（实际问题记录 item 20）。
//
// 背景：moveToTrash 只把文件改名成「时间戳.原名」放进 .trash，原始相对路径就此丢失，
// 无法支撑「列出来 + 恢复回原位」的交互。现在移入时向 .trash/.index.jsonl 追加一条索引；
// 列表时索引优先、未覆盖的旧条目按文件名尽力还原（origin 未知 → 恢复到集合根）。

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	share "github.com/leihenshang/api-doc-go-share/collection"
)

// TrashEntry 一条回收站记录。
type TrashEntry struct {
	File      string `json:"file"`      // .trash 内的文件名
	Origin    string `json:"origin"`    // 原相对路径（slash）；空 = 旧条目未知
	Kind      string `json:"kind"`      // folder | env | request | file（仅展示用）
	Name      string `json:"name"`      // 展示名（原 basename）
	DeletedAt int64  `json:"deletedAt"` // Unix 毫秒
}

const (
	trashDirName   = ".trash"
	trashIndexName = ".index.jsonl"
)

// trashKind 按原相对路径与类型归类（展示用，恢复逻辑不依赖）。
func trashKind(rel string, isDir bool) string {
	if isDir {
		return "folder"
	}
	if strings.HasPrefix(rel, "environments/") {
		return "env"
	}
	if strings.HasSuffix(rel, ".yml") {
		return "request"
	}
	return "file"
}

// legacyTrashBase 从「时间戳.原名」还原原名与删除时间；解析失败时按整名返回。
func legacyTrashBase(name string) (base string, at int64) {
	i := strings.Index(name, ".")
	if i <= 0 {
		return name, 0
	}
	head, rest := name[:i], name[i+1:]
	if t, err := time.ParseInLocation("20060102-150405", head, time.Local); err == nil && rest != "" {
		return rest, t.UnixMilli()
	}
	return name, 0
}

// moveToTrash 把文件或目录移入 .trash 并记索引（索引尽力而为：失败不影响已完成的移动）。
func (c *Collection) moveToTrash(full string) error {
	trash := filepath.Join(c.Dir, trashDirName)
	if err := os.MkdirAll(trash, 0o755); err != nil {
		return err
	}
	st, err := os.Stat(full)
	if err != nil {
		return err
	}
	rel := ""
	if r, e := filepath.Rel(c.Dir, full); e == nil {
		rel = filepath.ToSlash(r)
	}
	name := share.TrashName(filepath.Base(full), time.Now())
	if err := os.Rename(full, filepath.Join(trash, name)); err != nil {
		return err
	}
	base, _ := legacyTrashBase(name)
	c.appendTrashIndex(TrashEntry{File: name, Origin: rel, Kind: trashKind(rel, st.IsDir()), Name: base, DeletedAt: time.Now().UnixMilli()})
	return nil
}

func (c *Collection) trashIndexFile() string {
	return filepath.Join(c.Dir, trashDirName, trashIndexName)
}

// appendTrashIndex 追加一条索引（O_APPEND JSONL）。
func (c *Collection) appendTrashIndex(e TrashEntry) {
	f, err := os.OpenFile(c.trashIndexFile(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	if b, err := json.Marshal(e); err == nil {
		_, _ = f.Write(append(b, '\n'))
	}
}

// readTrashIndex 读全量索引；文件不存在返回空表。
func (c *Collection) readTrashIndex() (map[string]TrashEntry, error) {
	out := map[string]TrashEntry{}
	b, err := os.ReadFile(c.trashIndexFile())
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e TrashEntry
		if json.Unmarshal([]byte(line), &e) == nil && e.File != "" {
			out[e.File] = e
		}
	}
	return out, nil
}

// writeTrashIndex 整份重写索引（恢复/清除后去掉对应行）。
func (c *Collection) writeTrashIndex(m map[string]TrashEntry) {
	var sb strings.Builder
	for _, e := range m {
		if b, err := json.Marshal(e); err == nil {
			sb.Write(b)
			sb.WriteByte('\n')
		}
	}
	_ = os.WriteFile(c.trashIndexFile(), []byte(sb.String()), 0o644)
}

// dropTrashIndex 删除索引里的一条（尽力而为）。
func (c *Collection) dropTrashIndex(file string) {
	m, err := c.readTrashIndex()
	if err != nil {
		return
	}
	delete(m, file)
	c.writeTrashIndex(m)
}

// ListTrash 列出回收站（新→旧）。索引未覆盖的历史条目也给出（origin 未知）。
func (c *Collection) ListTrash() ([]TrashEntry, error) {
	ents, err := os.ReadDir(filepath.Join(c.Dir, trashDirName))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	index, _ := c.readTrashIndex()
	out := make([]TrashEntry, 0, len(ents))
	for _, en := range ents {
		name := en.Name()
		if name == trashIndexName || strings.HasPrefix(name, ".") {
			continue
		}
		e, ok := index[name]
		if !ok {
			base, at := legacyTrashBase(name)
			e = TrashEntry{File: name, Name: base, Kind: trashKind("", en.IsDir()), DeletedAt: at}
			if e.DeletedAt == 0 {
				if st, err := en.Info(); err == nil {
					e.DeletedAt = st.ModTime().UnixMilli()
				}
			}
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeletedAt > out[j].DeletedAt })
	return out, nil
}

// TrashInfo 查单条元数据（含旧条目兜底解析）。
func (c *Collection) TrashInfo(file string) (TrashEntry, error) {
	if err := c.checkTrashFile(file); err != nil {
		return TrashEntry{}, err
	}
	if m, err := c.readTrashIndex(); err == nil {
		if e, ok := m[file]; ok {
			return e, nil
		}
	}
	base, at := legacyTrashBase(file)
	return TrashEntry{File: file, Name: base, DeletedAt: at}, nil
}

// TrashAbs 返回条目在 .trash 内的绝对路径。
func (c *Collection) TrashAbs(file string) string { return filepath.Join(c.Dir, trashDirName, file) }

// checkTrashFile 拒绝路径分隔与隐藏文件——只允许 .trash 直接子项。
func (c *Collection) checkTrashFile(file string) error {
	if file == "" || strings.ContainsAny(file, `/\`) || strings.HasPrefix(file, ".") {
		return fmt.Errorf("回收站文件名非法: %q", file)
	}
	if _, err := os.Stat(c.TrashAbs(file)); err != nil {
		return fmt.Errorf("回收站条目不存在: %s", file)
	}
	return nil
}

// safeJoinRel 把集合内相对路径安全拼到集合根（拒绝 .. 与绝对路径）。
func (c *Collection) safeJoinRel(rel string) (string, error) {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" {
		return "", fmt.Errorf("目标路径为空")
	}
	abs := filepath.Join(c.Dir, filepath.FromSlash(rel))
	if abs != filepath.Clean(abs) || !strings.HasPrefix(filepath.Clean(abs), filepath.Clean(c.Dir)+string(os.PathSeparator)) {
		return "", fmt.Errorf("目标路径越出集合目录: %s", rel)
	}
	return abs, nil
}

// TrashRestoreRel 计算恢复目标相对路径：targetRelDir 为空 = 原位置所在目录。
// 导出给跨工作区恢复（App 层）复用同一套落点规则。
func TrashRestoreRel(e TrashEntry, targetRelDir string) string {
	base := e.Name
	if base == "" {
		base, _ = legacyTrashBase(e.File)
	}
	dir := strings.Trim(targetRelDir, "/ ")
	if targetRelDir == "" {
		dir = strings.TrimSuffix(filepath.ToSlash(filepath.Dir(e.Origin)), ".")
		dir = strings.Trim(dir, "/")
	}
	if dir == "" {
		return base
	}
	return dir + "/" + base
}

// RestoreTrash 在本集合内恢复一条回收站记录；targetRelDir 为空 = 恢复到原路径。
// 目标已存在时拒绝（不覆盖）。返回恢复后的相对路径。
func (c *Collection) RestoreTrash(file, targetRelDir string) (string, error) {
	e, err := c.TrashInfo(file)
	if err != nil {
		return "", err
	}
	rel := TrashRestoreRel(e, targetRelDir)
	abs, err := c.safeJoinRel(rel)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err == nil {
		return "", fmt.Errorf("目标已存在同名文件，恢复被拒绝：%s", rel)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(c.TrashAbs(file), abs); err != nil {
		return "", err
	}
	c.dropTrashIndex(file)
	return rel, nil
}

// ImportFromAbs 把外部绝对路径（另一集合的 .trash 条目）复制进本集合的 rel 位置；
// 拒绝覆盖，自动建父目录；目录整棵递归复制。返回落定的相对路径。
func (c *Collection) ImportFromAbs(srcAbs, rel string) (string, error) {
	abs, err := c.safeJoinRel(rel)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err == nil {
		return "", fmt.Errorf("目标已存在同名文件，恢复被拒绝：%s", rel)
	}
	st, err := os.Stat(srcAbs)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	if st.IsDir() {
		return rel, copyTree(srcAbs, abs)
	}
	return rel, copyFile(srcAbs, abs)
}

// RemoveTrash 删除 .trash 里的一条（文件或目录）并清索引；跨工作区恢复的收尾。
func (c *Collection) RemoveTrash(file string) error {
	if err := c.checkTrashFile(file); err != nil {
		return err
	}
	if err := os.RemoveAll(c.TrashAbs(file)); err != nil {
		return err
	}
	c.dropTrashIndex(file)
	return nil
}

// TrashPreview 回收站条目的预览内容（item 21）。
type TrashPreview struct {
	Kind      string   `json:"kind"`      // file | folder
	Content   string   `json:"content"`   // 文件内容（超 TRASH_PREVIEW_MAX 截断）
	Files     []string `json:"files"`     // 目录条目的文件清单（相对该目录）
	Truncated bool     `json:"truncated"` // 内容或清单被截断
}

// 预览限额：单文件 64KB、目录清单 200 项——弹窗只用于人眼核对，不做全文检索。
const (
	trashPreviewMaxBytes = 64 << 10
	trashPreviewMaxFiles = 200
)

// PreviewTrash 预览一条回收站记录：文件给内容（截断），目录给文件清单。
func (c *Collection) PreviewTrash(file string) (*TrashPreview, error) {
	if err := c.checkTrashFile(file); err != nil {
		return nil, err
	}
	abs := c.TrashAbs(file)
	st, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		out := &TrashPreview{Kind: "folder"}
		_ = filepath.WalkDir(abs, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if len(out.Files) >= trashPreviewMaxFiles {
				out.Truncated = true
				return filepath.SkipAll
			}
			if rel, e := filepath.Rel(abs, p); e == nil {
				out.Files = append(out.Files, filepath.ToSlash(rel))
			}
			return nil
		})
		return out, nil
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, trashPreviewMaxBytes+1)
	n, _ := io.ReadFull(f, buf)
	if n > trashPreviewMaxBytes {
		n = trashPreviewMaxBytes
	}
	return &TrashPreview{Kind: "file", Content: string(buf[:n]), Truncated: st.Size() > int64(n)}, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	return out.Close()
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(p, target)
	})
}
