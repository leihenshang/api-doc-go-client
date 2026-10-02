// Package watch 监听集合目录的外部改动（D1/D2）。
// 自写回环抑制：App 写盘前调用 Ignore 临时屏蔽，避免自动保存触发「外部改动」。
package watch

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// DefaultExtensions 参与「集合外部改动」的文件类型（与集合扫描器一致）。
// 其余文件——最典型的是集合里的 .proto 定义、证书、图片等——不触发事件：
// 设计文档 D3 明确 proto 变更**不参与监听**，更新完全由用户在界面上显式「导入 / 更新定义」触发。
var DefaultExtensions = []string{".yml", ".yaml", ".md"}

// Event 一次有意义的外部改动（已防抖合并）。
type Event struct {
	// Paths 变更的相对路径集合（斜杠分隔）；空表示「需要全量重扫」
	Paths []string
}

// Watcher 集合目录监听器。
type Watcher struct {
	mu       sync.Mutex
	w        *fsnotify.Watcher
	debounce time.Duration
	// ignore 窗口内忽略的路径前缀（自写回环）
	ignoreUntil map[string]time.Time
	// pending 防抖窗口内收集的变更
	pending map[string]struct{}
	timer   *time.Timer
	out     chan Event
	closed  bool
	skip    map[string]bool // 忽略的目录名（.trash 等）
	exts    map[string]bool // 关心的小写扩展名（DefaultExtensions）
}

// DefaultDebounce 合并窗口：连续写盘（自动保存/批量导入）只触发一次事件。
const DefaultDebounce = 300 * time.Millisecond

// New 创建监听器并开始监听 root。
func New(root string) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		w:           fw,
		debounce:    DefaultDebounce,
		ignoreUntil: map[string]time.Time{},
		pending:     map[string]struct{}{},
		out:         make(chan Event, 8),
		skip: map[string]bool{
			".trash": true, ".conflicts": true, "node_modules": true,
			".git": true,
		},
		exts: extensionSet(DefaultExtensions),
	}
	// 递归加 watch（目录树不深；新建子目录时也会补挂）
	if err := w.addRecursive(root); err != nil {
		_ = fw.Close()
		return nil, err
	}
	go w.loop(root)
	return w, nil
}

// Events 只读事件通道（防抖后的合并事件）。
func (w *Watcher) Events() <-chan Event { return w.out }

// Ignore 自写回环：在 d 时长内忽略对 rel 的改动。
func (w *Watcher) Ignore(rel string, d time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ignoreUntil[filepath.ToSlash(rel)] = time.Now().Add(d)
}

// Close 停止监听。
func (w *Watcher) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	if w.timer != nil {
		w.timer.Stop()
	}
	w.mu.Unlock()
	return w.w.Close()
}

func (w *Watcher) addRecursive(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 权限等错误跳过
		}
		if !d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if w.skip[base] || strings.HasPrefix(base, ".") {
			if path != root {
				return filepath.SkipDir
			}
		}
		return w.w.Add(path)
	})
}

func (w *Watcher) loop(root string) {
	for {
		select {
		case ev, ok := <-w.w.Events:
			if !ok {
				return
			}
			w.onFS(root, ev)
		case _, ok := <-w.w.Errors:
			if !ok {
				return
			}
			// 记录但不断流：错误不打断监听
		}
	}
}

func (w *Watcher) onFS(root string, ev fsnotify.Event) {
	// 只关心写/建/删/改名
	if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
		return
	}
	rel, err := filepath.Rel(root, ev.Name)
	if err != nil {
		return
	}
	rel = filepath.ToSlash(rel)

	// 只关心集合文件类型：.proto / 证书 / 图片等一律不参与「外部改动」（D3）。
	// 无扩展名的路径（目录、被删除的目录）继续上报，保持既有行为 —— 目录事件同时也是
	// 「先建目录再写文件」的兜底：新建目录的 watch 若晚于其中的文件写入，仍会因目录事件重扫。
	if ext := strings.ToLower(filepath.Ext(ev.Name)); ext != "" && !w.exts[ext] {
		return
	}
	// 新建目录补挂 watch
	if ev.Op&fsnotify.Create != 0 {
		if info, serr := os.Stat(ev.Name); serr == nil && info.IsDir() {
			_ = w.w.Add(ev.Name)
		}
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	// 自写回环抑制
	if until, ok := w.ignoreUntil[rel]; ok && time.Now().Before(until) {
		return
	}
	// 也匹配前缀（目录级 ignore）
	for p, until := range w.ignoreUntil {
		if time.Now().After(until) {
			delete(w.ignoreUntil, p)
			continue
		}
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return
		}
	}

	w.pending[rel] = struct{}{}
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = time.AfterFunc(w.debounce, w.flush)
}

func (w *Watcher) flush() {
	w.mu.Lock()
	paths := make([]string, 0, len(w.pending))
	for p := range w.pending {
		paths = append(paths, p)
	}
	w.pending = map[string]struct{}{}
	w.mu.Unlock()
	if len(paths) == 0 {
		return
	}
	select {
	case w.out <- Event{Paths: paths}:
	default:
		// 消费者忙：丢掉最旧（下次扫描会兜底）
	}
}

// extensionSet 扩展名列表 → 小写集合（自动补前导点）。
func extensionSet(exts []string) map[string]bool {
	out := make(map[string]bool, len(exts))
	for _, e := range exts {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		out[e] = true
	}
	return out
}
