package collection

// 模块（分组）与集合级维护：MCP 的 rename_module / delete_module / rename_project /
// prune_empty_modules 落点。删除走既有 .trash（DeleteFolder / DeleteFolderTree），
// 这里只补「按路径找 uid」「改集合显示名」「清理空模块目录」。

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// reservedTopDirs 不参与「空模块清理」的顶层目录（它们不是用户建的模块）。
var reservedTopDirs = map[string]bool{
	".trash":       true,
	"assets":       true,
	"environments": true,
	"examples":     true,
	".git":         true,
	".conflicts":   true,
	".sync":        true,
}

// FolderUIDByPath 相对路径（如 api/user）→ 分组 uid；集合根或不存在返回 false。
// 先 Clean 再拒 ../，避免路径穿越去读集合外的 folder.yml。
func (c *Collection) FolderUIDByPath(dir string) (string, bool) {
	d := strings.TrimSpace(strings.ReplaceAll(dir, "\\", "/"))
	d = strings.Trim(d, "/")
	if d == "" || d == "." {
		return "", false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(d)))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return c.folderUID(clean)
}

// RenameCollection 改集合清单（opencollection.yml）里的显示名；目录与 uid 不变。
func (c *Collection) RenameCollection(name string) error {
	if !validEntryName(name) {
		return fmt.Errorf("名称含非法字符或为空")
	}
	full := filepath.Join(c.Dir, "opencollection.yml")
	data, err := os.ReadFile(full)
	if err != nil {
		return fmt.Errorf("读取集合清单: %w", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("解析集合清单: %w", err)
	}
	info, _ := doc["info"].(map[string]any)
	if info == nil {
		info = map[string]any{}
	}
	info["name"] = name
	doc["info"] = info
	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("序列化集合清单: %w", err)
	}
	return writeFileAtomic(full, out, 0o644)
}

// PruneEmptyFolders 自底向上删除「空模块」目录（只有 folder.yml、没有请求/文档/子项），
// 返回删除的目录数。集合根与保留目录（.trash/assets/environments/examples…）不动；
// 空目录本身没有内容，直接 Remove（不需要进回收站）。
func (c *Collection) PruneEmptyFolders() (int, error) {
	var modules []string
	err := filepath.WalkDir(c.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(c.Dir, p)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") || reservedTopDirs[name] {
			return filepath.SkipDir
		}
		if _, ok := folderUIDAt(p); ok {
			modules = append(modules, rel)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	// 深的先删：父目录只有子目录被删掉后才可能变空
	sort.Slice(modules, func(i, j int) bool {
		return strings.Count(filepath.ToSlash(modules[i]), "/") > strings.Count(filepath.ToSlash(modules[j]), "/")
	})
	removed := 0
	for _, rel := range modules {
		full := filepath.Join(c.Dir, filepath.FromSlash(rel))
		entries, err := os.ReadDir(full)
		if err != nil {
			continue
		}
		onlyFolderYML := true
		for _, e := range entries {
			name := strings.ToLower(e.Name())
			if name == "folder.yml" || name == "folder.yaml" {
				continue
			}
			onlyFolderYML = false // 还有请求/文档/子目录：不是空模块
			break
		}
		if !onlyFolderYML {
			continue
		}
		for _, e := range entries {
			_ = os.Remove(filepath.Join(full, e.Name()))
		}
		if err := os.Remove(full); err != nil {
			return removed, fmt.Errorf("删除空模块 %s: %w", rel, err)
		}
		removed++
	}
	return removed, nil
}

// folderUIDAt 读取目录下的 folder.yml/.yaml 的 uid（与 folderUID 同口径，但接受绝对路径）。
func folderUIDAt(dir string) (string, bool) {
	for _, ext := range []string{".yml", ".yaml"} {
		data, err := os.ReadFile(filepath.Join(dir, "folder"+ext))
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

// ErrProjectNameRequired 删除项目必须显式确认（防误删）。
var ErrProjectNameRequired = errors.New("delete_project 需要 confirm 参数（填项目名以确认）")
