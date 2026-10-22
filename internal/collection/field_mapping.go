package collection

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// FieldMapping 项目字段字典条目（与服务端 field_mapping 表双向同步）。
// 字段路径（如 data.list[].id）→ 类型与中文说明，用于生成接口的返回参数说明。
type FieldMapping struct {
	UID         string `yaml:"uid" json:"uid"`
	Field       string `yaml:"field" json:"field"`
	Type        string `yaml:"type" json:"type"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// fieldMappingsFile 字段字典的集合内文件（项目级单文件，随集合进版本库）。
const fieldMappingsFile = "field_mappings.yml"

// fieldMappingDoc 字段字典文件结构（留出顶层对象以便后续扩展）。
type fieldMappingDoc struct {
	Mappings []FieldMapping `yaml:"mappings"`
}

// fieldMappingLimits 与服务端列宽对齐（超出会在 push 时被拒，这里提前拦截）。
const (
	fieldMappingFieldMax = 100
	fieldMappingTypeMax  = 100
	fieldMappingDescMax  = 100
)

// ListFieldMappings 读取集合内的字段字典（文件不存在返回空列表）。
func (c *Collection) ListFieldMappings() ([]FieldMapping, error) {
	data, err := os.ReadFile(filepath.Join(c.Dir, fieldMappingsFile))
	if os.IsNotExist(err) {
		return []FieldMapping{}, nil
	}
	if err != nil {
		return nil, err
	}
	var doc fieldMappingDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", fieldMappingsFile, err)
	}
	if doc.Mappings == nil {
		return []FieldMapping{}, nil
	}
	return doc.Mappings, nil
}

// SaveFieldMappings 整表写回字段字典（空列表 = 删除文件）。
func (c *Collection) SaveFieldMappings(items []FieldMapping) error {
	full := filepath.Join(c.Dir, fieldMappingsFile)
	if len(items) == 0 {
		if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	// 按字段路径排序落盘：手工编辑与 diff 都稳定。
	sorted := make([]FieldMapping, len(items))
	copy(sorted, items)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Field < sorted[j].Field })
	data, err := yaml.Marshal(fieldMappingDoc{Mappings: sorted})
	if err != nil {
		return err
	}
	return writeFileAtomic(full, data, 0o644)
}

// UpsertFieldMapping 新增或更新一条字段字典（uid 为空时生成；field 在集合内唯一）。
func (c *Collection) UpsertFieldMapping(fm FieldMapping) (*FieldMapping, error) {
	fm.Field = strings.TrimSpace(fm.Field)
	fm.Type = strings.TrimSpace(fm.Type)
	fm.Description = strings.TrimSpace(fm.Description)
	if err := validateFieldMapping(fm); err != nil {
		return nil, err
	}
	items, err := c.ListFieldMappings()
	if err != nil {
		return nil, err
	}
	if fm.UID == "" {
		fm.UID = NewUID()
	}
	for i, it := range items {
		if it.UID == fm.UID {
			items[i] = fm
			return &fm, c.SaveFieldMappings(items)
		}
		if it.Field == fm.Field {
			return nil, fmt.Errorf("字段 %q 已存在", fm.Field)
		}
	}
	return &fm, c.SaveFieldMappings(append(items, fm))
}

// DeleteFieldMapping 删除一条字段字典（不存在返回 errNotFound）。
func (c *Collection) DeleteFieldMapping(uid string) error {
	items, err := c.ListFieldMappings()
	if err != nil {
		return err
	}
	kept := make([]FieldMapping, 0, len(items))
	found := false
	for _, it := range items {
		if it.UID == uid {
			found = true
			continue
		}
		kept = append(kept, it)
	}
	if !found {
		return errNotFound
	}
	return c.SaveFieldMappings(kept)
}

// FieldMappingHash 条目内容摘要（同步脏检测基线；与文件 hash 无关，
// 单文件多条目时避免「改一条 → 全量重推」）。
func FieldMappingHash(fm FieldMapping) string {
	sum := sha256.Sum256([]byte(fm.UID + "\x00" + fm.Field + "\x00" + fm.Type + "\x00" + fm.Description))
	return hex.EncodeToString(sum[:])
}

// validateFieldMapping 与服务端列宽/必填对齐。
func validateFieldMapping(fm FieldMapping) error {
	if fm.Field == "" || fm.Type == "" {
		return fmt.Errorf("字段路径与类型不能为空")
	}
	if utf8.RuneCountInString(fm.Field) > fieldMappingFieldMax {
		return fmt.Errorf("字段路径不能超过 %d 个字符", fieldMappingFieldMax)
	}
	if utf8.RuneCountInString(fm.Type) > fieldMappingTypeMax {
		return fmt.Errorf("类型不能超过 %d 个字符", fieldMappingTypeMax)
	}
	if utf8.RuneCountInString(fm.Description) > fieldMappingDescMax {
		return fmt.Errorf("说明不能超过 %d 个字符", fieldMappingDescMax)
	}
	return nil
}
