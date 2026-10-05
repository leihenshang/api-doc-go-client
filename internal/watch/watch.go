// Package watch 监听集合目录的外部改动（D1/D2）。
// 自写回环抑制：App 写盘前调用 Ignore 临时屏蔽，避免自动保存触发「外部改动」。
package watch

import (
	"crypto/sha256"
	"encoding/hex"
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
	// ignore 窗口内忽略的路径（自写回环）
	ignoreUntil map[string]time.Time
	// ignoreHash 自写回环的内容指纹（rel → 我方写入内容的 sha256）：
	// 窗口内事件只有「内容仍与指纹一致」才丢弃，别人在同一窗口里改的照常上报。
	ignoreHash map[string]string
	// pending 防抖窗口内收集的变更
	pending map[string]struct{}
	timer   *time.Timer
	out     chan Event
	closed  bool
	done    chan struct{}   // Close 时关闭：消费方据此退出（out 通道本身不关闭，避免向已关闭通道发送）
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
		ignoreHash:  map[string]string{},
		pending:     map[string]struct{}{},
		out:         make(chan Event, 8),
		done:        make(chan struct{}),
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

// Done 关闭信号：Close 后立即可读。消费方 select 它即可退出，不必依赖 out 被关闭。
func (w *Watcher) Done() <-chan struct{} { return w.done }

// Ignore 自写回环（无指纹）：在 d 时长内忽略对 rel 的改动。
// 写盘方能给出内容哈希时优先用 IgnoreWrite —— 这个版本会把同一窗口内的**别人**的改动也一起吞掉。
func (w *Watcher) Ignore(rel string, d time.Duration) { w.IgnoreWrite(rel, "", d) }

// IgnoreWrite 自写回环（带内容指纹）：d 窗口内对 rel 的事件，只有「文件内容仍等于 hash」
// 才丢弃（确认是我方刚写的那份）；内容已变说明是别人改的，照常上报。
//
// hash 为空时退化为「窗口内一律忽略」（写盘方拿不到内容时的保守做法）。
func (w *Watcher) IgnoreWrite(rel, hash string, d time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	key := filepath.ToSlash(rel)
	w.ignoreUntil[key] = time.Now().Add(d)
	w.ignoreHash[key] = hash
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
	close(w.done) // 唤醒消费方（事件通道不关闭：flush 可能仍在锁外发送）
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

	// 新建目录补挂 watch。必须在扩展名过滤**之前**判断：目录名可以带点（如 v1.0），
	// 其「扩展名」是 .0 —— 先过滤就会漏挂，之后该目录下的改动再也收不到事件。
	if ev.Op&fsnotify.Create != 0 {
		if info, serr := os.Stat(ev.Name); serr == nil && info.IsDir() {
			base := filepath.Base(ev.Name)
			if !w.skip[base] && !strings.HasPrefix(base, ".") {
				_ = w.w.Add(ev.Name)
			}
		}
	}
	// 只关心集合文件类型：.proto / 证书 / 图片等一律不参与「外部改动」（D3）。
	// 无扩展名的路径（目录、被删除的目录）继续上报，保持既有行为 —— 目录事件同时也是
	// 「先建目录再写文件」的兜底：新建目录的 watch 若晚于其中的文件写入，仍会因目录事件重扫。
	if ext := strings.ToLower(filepath.Ext(ev.Name)); ext != "" && !w.exts[ext] {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	// 自写回环抑制：窗口内且内容与「我方写入的指纹」一致才算自己写的；否则放行（别人改的）
	if until, ok := w.ignoreUntil[rel]; ok && time.Now().Before(until) {
		if h := w.ignoreHash[rel]; h == "" || fileSHA256(ev.Name) == h {
			return
		}
		delete(w.ignoreUntil, rel)
		delete(w.ignoreHash, rel)
	}
	// 也匹配前缀（目录级 ignore）：目录级忽略拿不到单个文件的内容指纹，仍按时间窗口放行
	for p, until := range w.ignoreUntil {
		if time.Now().After(until) {
			delete(w.ignoreUntil, p)
			delete(w.ignoreHash, p)
			continue
		}
		if rel != p && strings.HasPrefix(rel, p+"/") {
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

// fileSHA256 文件内容哈希（读不到返回空串 = 与任何指纹都不相等，按外部改动处理）。
func fileSHA256(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
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
