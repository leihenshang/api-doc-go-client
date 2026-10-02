package proto

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// Cache 编译结果缓存。
//
// 失效策略是**显式**的（设计文档 D3：不监听 proto 变更）：
//   - key 只由「文件列表 + import 路径」构成，命中即复用（成功与失败都缓存，
//     避免每次发送都对同一份坏定义重复编译、重复刷日志）；
//   - 只有用户「导入 / 更新定义」时调用 Invalidate 才会重新编译；
//   - 不读 mtime、不做内容比对。
//
// 已知取舍：并发首次编译同一 key 时可能编译两次（桌面端单用户场景，不做 singleflight）。
type Cache struct {
	mu      sync.Mutex
	entries map[string]*entry
}

type entry struct {
	result *Result
	err    error
}

// NewCache 创建空缓存。
func NewCache() *Cache {
	return &Cache{entries: map[string]*entry{}}
}

// Compile 命中缓存直接返回；未命中则编译并写回（失败结果同样缓存）。
func (c *Cache) Compile(ctx context.Context, files, importPaths []string) (*Result, error) {
	key := cacheKey(files, importPaths)

	c.mu.Lock()
	if e, ok := c.entries[key]; ok {
		c.mu.Unlock()
		return e.result, e.err
	}
	c.mu.Unlock()

	result, err := Compile(ctx, files, importPaths)

	c.mu.Lock()
	c.entries[key] = &entry{result: result, err: err}
	c.mu.Unlock()
	return result, err
}

// Invalidate 清空缓存：用户重新导入 / 更新定义后调用。
func (c *Cache) Invalidate() {
	c.mu.Lock()
	c.entries = map[string]*entry{}
	c.mu.Unlock()
}

// Len 当前缓存条目数（状态展示与测试用）。
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// cacheKey 由文件与 import 路径构成：排序 + 去重，保证同一份配置（顺序不同、import 路径重复）
// 命中同一个 key。
func cacheKey(files, importPaths []string) string {
	return strings.Join(dedupeSorted(files), "\x00") + "\x01" + strings.Join(dedupeSorted(importPaths), "\x00")
}

// dedupeSorted 排序并去重（不改动入参切片）。
func dedupeSorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	uniq := out[:0]
	for i, s := range out {
		if i > 0 && s == out[i-1] {
			continue
		}
		uniq = append(uniq, s)
	}
	return uniq
}
