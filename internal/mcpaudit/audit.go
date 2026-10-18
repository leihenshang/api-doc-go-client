// Package mcpaudit 是 MCP 工具调用审计的纯逻辑层：JSON Lines 追加写、按大小轮转、
// 异步队列、订阅扇出与查询过滤。
//
// 单独成叶子包（同 mcpauth）：internal/mcp 的 guard 写审计、internal/app 的门面读审计，
// 二者都不能让 mcp 反向依赖 app。
package mcpaudit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// 文件权限：审计含集合结构与操作意图，按密钥文件同级敏感度处理。
	fileMode = 0o600

	maxFileBytes = 5 << 20 // 单文件 5 MiB，达到即轮转
	keepArchives = 3       // 最多保留 3 份归档
	queueDepth   = 1024    // 异步队列容量
	ringCapacity = 1000    // 内存环形缓冲（近期查询加速）
	maxLineBytes = 16 << 10
	resultCap    = 1024 // resultSummary 上限
	argsCap      = 2048
	subBuffer    = 256 // 单个订阅者的缓冲；慢订阅者丢事件不阻塞落盘

	// 查询扫描保护：坏行/超大文件不拖垮门面。
	maxScanLines = 200_000
	maxScanBytes = 50 << 20
)

// ResultKind 一次工具调用的出口分类。
type ResultKind string

const (
	KindOK       ResultKind = "ok"        // 业务成功
	KindBizError ResultKind = "biz_error" // 业务错误（IsError 文本）
	KindDenied   ResultKind = "denied"    // T2 授权拒绝
	KindPanic    ResultKind = "panic"     // handler panic 被 recover
)

// Event 一条审计记录（落盘与实时事件同一 schema）。
type Event struct {
	TS            string `json:"ts"`
	TSMs          int64  `json:"tsMs"`
	SessionID     string `json:"sessionId,omitempty"`
	Transport     string `json:"transport,omitempty"`
	Client        string `json:"client,omitempty"`
	Tool          string `json:"tool"`
	Risk          string `json:"risk"`
	Project       string `json:"project,omitempty"`
	RequestID     string `json:"requestId,omitempty"`
	Approved      *bool  `json:"approved,omitempty"`
	GrantedBy     string `json:"grantedBy,omitempty"`
	ArgsSummary   string `json:"argsSummary,omitempty"`
	ResultOK      bool   `json:"ok"`
	ResultKind    string `json:"resultKind"`
	ResultSummary string `json:"resultSummary,omitempty"`
	DurationMs    int64  `json:"durationMs"`
	Sequence      int64  `json:"seq"`
}

// Query 审计查询（空字段 = 不过滤）。
type Query struct {
	Tool    string
	Project string
	Risk    string
	Ok      *bool
	Search  string
	Since   time.Time
	Until   time.Time
	Limit   int
	Offset  int
	// IncludeArchives 是否连归档文件一起扫；默认 false（只看当前活动日志，
	// 「清空」后面板即为空；归档供人工取证，深翻页时可显式打开）。
	IncludeArchives bool
}

// Page 查询结果。
type Page struct {
	Total     int      `json:"total"`
	Events    []*Event `json:"events"`
	File      string   `json:"file"`
	Truncated bool     `json:"true"` // 扫描触及行数/字节上限
	Corrupt   int      `json:"corrupt"`
	Dropped   int64    `json:"dropped"` // 异步队列丢弃的事件数（队列满）
}

// Auditor 审计器契约（nil 安全由调用方判断；guard 里 ts.audit==nil 即跳过）。
type Auditor interface {
	Log(ev Event)
	Subscribe() (<-chan Event, func())
	List(context.Context, Query) (*Page, error)
	Path() string
	ExportTo(dest string) error
	Clear() error // 归档当前文件并新开空文件
	Close() error
}

// bearerRe 结果文本里可能被上游回显的 Bearer 令牌（结果文本的二次脱敏）。
var bearerRe = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._\-]+`)

// RedactText 对结果/错误文本做二次脱敏（入参摘要在 mcp 层已脱敏）。
func RedactText(s string) string {
	return bearerRe.ReplaceAllString(s, "${1}••••••")
}

// Clamp 截断摘要并标注。
func Clamp(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…<truncated>"
}

// Option 测试/特殊部署用的可选参数。
type Option func(*JSONLAuditor)

// WithMaxFileBytes 覆盖单文件轮转阈值（测试用小阈值；生产用默认 5MiB）。
func WithMaxFileBytes(n int64) Option {
	return func(a *JSONLAuditor) { a.maxFileBytes = n }
}

// WithKeepArchives 覆盖归档保留份数。
func WithKeepArchives(n int) Option {
	return func(a *JSONLAuditor) { a.keepArchives = n }
}

// JSONLAuditor 单消费者 JSONL 审计器。
type JSONLAuditor struct {
	path string
	logf func(string, ...any)

	maxFileBytes int64
	keepArchives int

	mu      sync.Mutex // 守护 size/seq/ring/subs/dropped（写循环内也持短锁取资源）
	size    int64
	seq     int64
	ring    []*Event
	subs    map[int]chan Event
	subSeq  int
	dropped int64

	queue chan Event
	ctl   chan func() // 在唯一写 goroutine 上执行的文件操作（rotate/export 同步）
	done  chan struct{}

	fileMu sync.RWMutex // rotate（写锁）与 export 拷贝（读锁）互斥；普通 append 不持锁

	closeOnce sync.Once
}

// New 打开（或创建）审计文件并启动单消费者写循环。
func New(path string, logger func(string, ...any), opts ...Option) (*JSONLAuditor, error) {
	if logger == nil {
		logger = func(string, ...any) {}
	}
	a := &JSONLAuditor{
		path:         path,
		logf:         logger,
		maxFileBytes: maxFileBytes,
		keepArchives: keepArchives,
		subs:         map[int]chan Event{},
		queue:        make(chan Event, queueDepth),
		ctl:          make(chan func(), 4),
		done:         make(chan struct{}),
	}
	for _, o := range opts {
		o(a)
	}
	if err := a.initFile(); err != nil {
		return nil, err
	}
	go a.run()
	return a, nil
}

// initFile 确保目录/文件存在并读取当前大小；不长期持有 fd。
func (a *JSONLAuditor) initFile() error {
	if err := os.MkdirAll(filepath.Dir(a.path), 0o755); err != nil {
		return fmt.Errorf("创建审计目录: %w", err)
	}
	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, fileMode)
	if err != nil {
		return fmt.Errorf("打开审计文件: %w", err)
	}
	st, _ := f.Stat()
	_ = f.Close()
	// 权限收敛：文件可能由旧版本/其它工具以更宽权限创建
	// 审计文件权限收紧：某些文件系统/挂载点不支持 chmod，失败不影响审计本身。
	_ = os.Chmod(a.path, fileMode)
	if st != nil {
		a.size = st.Size()
	}
	return nil
}

// Log 异步入队：绝不阻塞调用线程；队列满时丢最旧一条并计数。
func (a *JSONLAuditor) Log(ev Event) {
	select {
	case a.queue <- ev:
	default:
		// 队列满：挤出一条最旧事件给新事件腾位（审计不能拖死工具主流程）
		select {
		case <-a.queue:
			a.bumpDropped()
		default:
		}
		select {
		case a.queue <- ev:
		default:
			a.bumpDropped()
		}
	}
}

func (a *JSONLAuditor) bumpDropped() {
	a.mu.Lock()
	a.dropped++
	a.mu.Unlock()
}

// Subscribe 订阅实时审计事件（缓冲满时该订阅者被跳过，不影响落盘）。
func (a *JSONLAuditor) Subscribe() (<-chan Event, func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.subSeq++
	id := a.subSeq
	ch := make(chan Event, subBuffer)
	a.subs[id] = ch
	cancel := func() {
		a.mu.Lock()
		// 关闭通道唤醒消费者（`for ev := range ch` 随即退出），否则订阅 goroutine 会永久阻塞。
		// 用 map 命中判定防重复关闭：Close() 里也会对仍在册的通道关闭，二者不会二次 close。
		if c, ok := a.subs[id]; ok {
			delete(a.subs, id)
			close(c)
		}
		a.mu.Unlock()
	}
	return ch, cancel
}

// Path 当前活动审计文件绝对路径。
func (a *JSONLAuditor) Path() string { return a.path }

func (a *JSONLAuditor) run() {
	defer close(a.done)
	// 每条事件独立 open-append-close：审计低频（人工/AI 调用级），换来无长持 fd ——
	// Windows 下不会锁住配置目录（测试/卸载友好），多进程追加也天然安全。
	for {
		select {
		case ev, ok := <-a.queue:
			if !ok {
				return
			}
			a.writeOne(ev)
		case fn := <-a.ctl:
			fn()
		}
	}
}

// onWriter 在唯一写 goroutine 上执行 fn（rotate/clear 等文件结构变更）。
func (a *JSONLAuditor) onWriter(fn func() error) error {
	errc := make(chan error, 1)
	a.ctl <- func() { errc <- fn() }
	return <-errc
}

// writeOne 序列化→追加→扇出→轮转。全部在单写 goroutine 内串行，行不会交错。
func (a *JSONLAuditor) writeOne(evIn Event) {
	a.mu.Lock()
	a.seq++
	evIn.Sequence = a.seq
	a.mu.Unlock()

	b, err := json.Marshal(evIn)
	if err != nil {
		b = []byte(fmt.Sprintf(`{"tsMs":%d,"tool":%q,"resultKind":"audit_error","seq":%d}`,
			evIn.TSMs, evIn.Tool, evIn.Sequence))
	}
	if len(b) > maxLineBytes {
		evIn.ArgsSummary = Clamp(evIn.ArgsSummary, argsCap/2)
		evIn.ResultSummary = Clamp(RedactText(evIn.ResultSummary), resultCap/2)
		if b2, err := json.Marshal(evIn); err == nil {
			b = b2
		}
	}

	// 单条事件独立打开追加并立即关闭（写 goroutine 串行，无需额外写锁）。
	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, fileMode)
	if err != nil {
		a.logf("audit write: %v", err)
		return
	}
	_, werr := f.Write(append(b, '\n'))
	if cerr := f.Close(); cerr != nil && werr == nil {
		werr = cerr
	}
	if werr != nil {
		a.logf("audit write: %v", werr)
		return
	}
	// 权限收紧失败（文件系统不支持 chmod）不影响审计写入本身。
	_ = os.Chmod(a.path, fileMode)

	needRotate := false
	a.mu.Lock()
	a.size += int64(len(b)) + 1
	needRotate = a.size >= a.maxFileBytes
	a.ring = appendRing(a.ring, &evIn)
	// 扇出必须与「关闭订阅通道」（cancel / Close）互斥，否则可能向已关闭通道发送而 panic；
	// 二者都在 a.mu 下完成即可。发送为非阻塞（满则跳过），持锁时间有界（订阅者数量很小）。
	for _, ch := range a.subs {
		select {
		case ch <- evIn:
		default: // 慢订阅者跳过
		}
	}
	a.mu.Unlock()
	if needRotate {
		if err := a.rotateNow(); err != nil {
			a.logf("audit rotate: %v", err)
		}
	}
}

func appendRing(ring []*Event, ev *Event) []*Event {
	ring = append(ring, ev)
	if len(ring) > ringCapacity {
		ring = ring[len(ring)-ringCapacity:]
	}
	return ring
}

// rotateNow 归档当前文件（rename）并新开空文件，再清理超量旧归档。
// 在写 goroutine 上执行（writeOne 直接调；外部请求经 onWriter），并用 fileMu 挡住导出拷贝。
func (a *JSONLAuditor) rotateNow() error {
	a.fileMu.Lock()
	defer a.fileMu.Unlock()
	arch := fmt.Sprintf("%s.%d.jsonl", strings.TrimSuffix(a.path, ".jsonl"), time.Now().UnixNano())
	if err := os.Rename(a.path, arch); err != nil && !errors.Is(err, os.ErrNotExist) {
		// rename 失败也尽量继续往下开新文件，避免审计停摆
		a.logf("audit rename: %v", err)
	}
	if err := a.initFile(); err != nil {
		return err
	}
	// 旧事件已进归档：内存环只加速「当前文件」的近期查询，必须随轮转清空，
	// 否则默认（不含归档）查询会把归档里的事件再返回一遍。
	a.mu.Lock()
	a.ring = nil
	a.size = 0
	a.mu.Unlock()
	a.pruneArchives()
	return nil
}

// Clear 归档当前文件并新开空文件（门面「清空」：先留痕再清，防误删导致完全无痕）。
func (a *JSONLAuditor) Clear() error {
	return a.onWriter(a.rotateNow)
}

// pruneArchives 删除超出 keepArchives 的最旧归档（按修改时间）。
func (a *JSONLAuditor) pruneArchives() {
	pattern := strings.TrimSuffix(a.path, ".jsonl") + ".*.jsonl"
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) <= a.keepArchives {
		return
	}
	type fi struct {
		path string
		mod  time.Time
	}
	items := make([]fi, 0, len(matches))
	for _, m := range matches {
		if st, err := os.Stat(m); err == nil {
			items = append(items, fi{m, st.ModTime()})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].mod.Before(items[j].mod) })
	for i := 0; i < len(items)-a.keepArchives; i++ {
		_ = os.Remove(items[i].path)
	}
}

// ExportTo 把当前活动日志拷贝到用户指定路径（取消/空路径返回错误由门面处理）。
// 与 rotate 互斥：拷贝期间不会发生 rename，保证读到完整文件。
func (a *JSONLAuditor) ExportTo(dest string) error {
	if strings.TrimSpace(dest) == "" {
		return errors.New("导出路径为空")
	}
	a.fileMu.RLock()
	defer a.fileMu.RUnlock()
	src, err := os.Open(a.path)
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fileMode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, src)
	return err
}

// Close 停止写循环并 flush（最多等待 2s）。幂等，可重复调用。
func (a *JSONLAuditor) Close() error {
	a.closeOnce.Do(func() { close(a.queue) })
	stopped := false
	select {
	case <-a.done:
		stopped = true
	case <-time.After(2 * time.Second):
	}
	// 写循环已确定退出（不再有扇出发送）后，关闭仍在册的订阅通道，唤醒未显式退订的消费者。
	// 超时未退出时不关闭（避免与残留发送竞态）——此时正常路径的 cancel() 已各自关闭。
	if stopped {
		a.mu.Lock()
		for id, ch := range a.subs {
			close(ch)
			delete(a.subs, id)
		}
		a.mu.Unlock()
	}
	return nil
}

// List 扫描当前文件做倒序过滤分页（近期可由内存环加速；归档文件在 offset 跨越时才读）。
func (a *JSONLAuditor) List(_ context.Context, q Query) (*Page, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	page := &Page{File: a.path}

	// 近期事件优先取内存环（无需读盘）；不足再读文件补齐。
	a.mu.Lock()
	ring := make([]*Event, len(a.ring))
	copy(ring, a.ring)
	page.Dropped = a.dropped
	a.mu.Unlock()

	matched := make([]*Event, 0, len(ring))
	seen := map[string]bool{}
	for _, ev := range ring {
		if matchEvent(ev, q) {
			matched = append(matched, ev)
			seen[dedupKey(ev)] = true
		}
	}

	// 读盘补充（含归档：按修改时间新→旧；触及上限置 Truncated）。
	corrupt, scanned, bytes := 0, 0, int64(0)
	files := []string{a.path}
	pattern := strings.TrimSuffix(a.path, ".jsonl") + ".*.jsonl"
	if q.IncludeArchives {
		if arches, err := filepath.Glob(pattern); err == nil {
			// 归档按修改时间新→旧（深翻页时先扫更近的归档）
			sort.Slice(arches, func(i, j int) bool {
				fi, ei := os.Stat(arches[i])
				fj, ej := os.Stat(arches[j])
				if ei != nil || ej != nil {
					return arches[i] > arches[j]
				}
				return fi.ModTime().After(fj.ModTime())
			})
			files = append(files, arches...)
		}
	}
	for _, file := range files {
		if len(matched) >= q.Offset+limit && file != a.path {
			break // 归档只在需要深翻页时扫描
		}
		evs, bad, n, sz, trunc := scanFile(file, seen)
		corrupt += bad
		scanned += n
		bytes += sz
		if trunc {
			page.Truncated = true
		}
		for _, ev := range evs {
			if matchEvent(ev, q) {
				matched = append(matched, ev)
			}
		}
		if scanned >= maxScanLines || bytes >= maxScanBytes {
			page.Truncated = true
			break
		}
	}
	page.Corrupt = corrupt

	// 新→旧：tsMs 为主、seq 兜底（多进程同写时 seq 各自单调，文件顺序由追加时序体现）。
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].TSMs != matched[j].TSMs {
			return matched[i].TSMs > matched[j].TSMs
		}
		return matched[i].Sequence > matched[j].Sequence
	})
	page.Total = len(matched)
	if q.Offset >= len(matched) {
		page.Events = []*Event{}
		return page, nil
	}
	end := q.Offset + limit
	if end > len(matched) {
		end = len(matched)
	}
	page.Events = matched[q.Offset:end]
	return page, nil
}

// dedupKey 内存环与文件之间的去重键：seq 各进程独立，必须搭配 tsMs 防跨进程撞车。
func dedupKey(ev *Event) string {
	return fmt.Sprintf("%d|%d", ev.TSMs, ev.Sequence)
}

// scanFile 逐行解析一个 jsonl；坏行计 corrupt 不致命。返回按文件顺序的事件与扫描统计。
func scanFile(path string, skip map[string]bool) (evs []*Event, corrupt, lines int, bytes int64, truncated bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes*2)
	for sc.Scan() {
		line := sc.Bytes()
		lines++
		bytes += int64(len(line)) + 1
		if lines > maxScanLines || bytes > maxScanBytes {
			truncated = true
			return
		}
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		ev := &Event{}
		if err := json.Unmarshal(line, ev); err != nil {
			corrupt++
			continue
		}
		if skip[dedupKey(ev)] {
			continue
		}
		evs = append(evs, ev)
	}
	return
}

func matchEvent(ev *Event, q Query) bool {
	if q.Tool != "" && ev.Tool != q.Tool {
		return false
	}
	if q.Project != "" && ev.Project != q.Project {
		return false
	}
	if q.Risk != "" && ev.Risk != q.Risk {
		return false
	}
	if q.Ok != nil && ev.ResultOK != *q.Ok {
		return false
	}
	if !q.Since.IsZero() && ev.TSMs < q.Since.UnixMilli() {
		return false
	}
	if !q.Until.IsZero() && ev.TSMs > q.Until.UnixMilli() {
		return false
	}
	if s := strings.TrimSpace(q.Search); s != "" {
		if !strings.Contains(ev.ArgsSummary, s) && !strings.Contains(ev.ResultSummary, s) &&
			!strings.Contains(ev.Tool, s) && !strings.Contains(ev.Project, s) {
			return false
		}
	}
	return true
}
