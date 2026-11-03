package collection

// 全局变量（服务端 scope=3，跨项目共享）的本地镜像。
//
// 为什么单独落 globals.yml：全局变量不属于任何环境，但客户端的 {{var}} 解析必须能命中它们
// （优先级最低：请求级 > 环境当前 > 环境通用 > 全局）。不同步的话，同一请求在 Web 与
// 客户端会跑出不同结果 —— 这是最容易被忽略的一类「两端不一致」。
//
// 谁维护：服务端管理员。客户端只读镜像，由同步下发覆盖，不在界面里编辑；
// 因此本文件不参与推送（推上去也会与服务端权威冲突）。

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// globalsFile 全局变量在集合根的文件名。
const globalsFile = "globals.yml"

// globalsDoc 全局变量文件结构（留出顶层对象便于后续扩展）。
type globalsDoc struct {
	Globals []Var `yaml:"globals"`
}

// ListGlobals 读取集合内的全局变量（文件不存在返回空列表）。
func (c *Collection) ListGlobals() ([]Var, error) {
	data, err := os.ReadFile(filepath.Join(c.Dir, globalsFile))
	if os.IsNotExist(err) {
		return []Var{}, nil
	}
	if err != nil {
		return nil, err
	}
	var doc globalsDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", globalsFile, err)
	}
	if doc.Globals == nil {
		return []Var{}, nil
	}
	return doc.Globals, nil
}

// SaveGlobals 整表写回全局变量（空列表 = 删除文件）。
func (c *Collection) SaveGlobals(items []Var) error {
	full := filepath.Join(c.Dir, globalsFile)
	if len(items) == 0 {
		if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	// 按名字排序落盘：手工编辑与 diff 都稳定。
	sorted := make([]Var, len(items))
	copy(sorted, items)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	data, err := yaml.Marshal(globalsDoc{Globals: sorted})
	if err != nil {
		return err
	}
	return writeFileAtomic(full, data, 0o644)
}

// UpsertGlobal 按 uid 新增或更新一条全局变量（uid 为空时生成）。
func (c *Collection) UpsertGlobal(v Var) (Var, error) {
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" {
		return v, fmt.Errorf("变量名不能为空")
	}
	items, err := c.ListGlobals()
	if err != nil {
		return v, err
	}
	if v.UID == "" {
		v.UID = NewUID()
	}
	for i, it := range items {
		if it.UID != "" && it.UID == v.UID {
			items[i] = v
			return v, c.SaveGlobals(items)
		}
		if it.Name == v.Name {
			// 同名不同 uid：按名字对齐（服务端改了变量名时不会留下重复条目）。
			v.UID = it.UID
			items[i] = v
			return v, c.SaveGlobals(items)
		}
	}
	return v, c.SaveGlobals(append(items, v))
}
