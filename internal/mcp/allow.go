package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// AllowDir 一个被授权给 MCP 的工作目录。
//
// 为什么要白名单：MCP 的调用方是不可信的（AI 客户端、经 HTTP 端口连上来的任意进程），
// 而工具能读写集合目录里的文件。以前「项目根目录」由调用方给（-root / create_project 的 root 参数），
// 等于把「能写哪里」交给了调用方；现在改成：只有配置里列出的目录及其子目录可访问，
// 且逐条区分只读/可写。
type AllowDir struct {
	// Path 绝对路径（配置里存原样，使用时统一规范化为绝对路径）
	Path string `json:"path"`
	// Writable 是否允许写（新建/修改/删除）。false = 只读：读工具可用，写工具一律拒绝
	Writable bool `json:"writable"`
}

// NormalizeAllow 规范化白名单：绝对化、去尾分隔符、去重（Windows 忽略大小写）、丢弃空项。
func NormalizeAllow(list []AllowDir) []AllowDir {
	out := make([]AllowDir, 0, len(list))
	seen := map[string]int{}
	for _, a := range list {
		p := strings.TrimSpace(a.Path)
		if p == "" {
			continue
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		p = filepath.Clean(p)
		key := p
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if i, ok := seen[key]; ok {
			// 同一个目录出现多次：可写取「更宽松」的那个（用户多半是补加权限）
			if a.Writable && !out[i].Writable {
				out[i].Writable = true
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, AllowDir{Path: p, Writable: a.Writable})
	}
	return out
}

// within 判断 path 是否等于 dir 或位于 dir 之内（Windows 忽略大小写）。
//
// 先按字符串（相对路径）判断，再对**已存在**的路径做一次 EvalSymlinks 复核：
// 否则一个指向白名单外的符号链接就能绕过白名单。
func within(dir, path string) bool {
	if dir == "" || path == "" {
		return false
	}
	if withinLexical(dir, path) {
		// 目录本身可能就是软链（例如 macOS 的 /tmp → /private/tmp）：
		// 解析后再判断一次，两侧都解析，避免「一边是真路径一边是软链」导致误判。
		rd, err1 := filepath.EvalSymlinks(dir)
		rp, err2 := filepath.EvalSymlinks(path)
		if err1 == nil && err2 == nil {
			return withinLexical(rd, rp)
		}
		return true
	}
	// 词法不在目录内：还有一种是「path 经由软链指向目录内」，这里不再放行（保守拒绝）
	return false
}

func withinLexical(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	if filepath.IsAbs(rel) { // 不同卷（Windows 的 C: 与 D:）：Rel 会返回绝对路径
		return false
	}
	return true
}

// allowedFor 找覆盖 path 的白名单条目；needWrite 时要求可写。
func allowedFor(list []AllowDir, path string, needWrite bool) (*AllowDir, error) {
	if path == "" {
		return nil, fmt.Errorf("路径为空")
	}
	abs := path
	if a, err := filepath.Abs(path); err == nil {
		abs = filepath.Clean(a)
	}
	var hit *AllowDir
	for i := range list {
		if within(list[i].Path, abs) {
			hit = &list[i]
			break
		}
	}
	if hit == nil {
		return nil, fmt.Errorf("目录 %s 不在 MCP 授权的工作目录里：请在客户端设置里添加（或检查白名单）", abs)
	}
	if needWrite && !hit.Writable {
		return nil, fmt.Errorf("工作目录 %s 是只读授权（writable=false），不能新建/修改/删除内容", hit.Path)
	}
	return hit, nil
}

// isDir 存在且是目录。
func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
