package watch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// waitEvent 在超时内等待一个命中 substr 的事件（忽略其它事件），返回是否等到。
func waitEvent(w *Watcher, substr string, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-w.Events():
			for _, p := range ev.Paths {
				if strings.Contains(p, substr) {
					return true
				}
			}
		case <-deadline:
			return false
		}
	}
}

func newTestWatcher(t *testing.T, dir string) *Watcher {
	t.Helper()
	w, err := New(dir)
	if err != nil {
		t.Skipf("fsnotify 不可用，跳过: %v", err)
	}
	w.debounce = 20 * time.Millisecond // 同包测试：缩短防抖，加速断言
	t.Cleanup(func() { _ = w.Close() })
	waitReady(t, w, dir)
	return w
}

// drainEvents 消费掉已排队的事件（避免上一动作的事件污染下一段断言）。
func drainEvents(w *Watcher, d time.Duration) {
	deadline := time.After(d)
	for {
		select {
		case <-w.Events():
		case <-deadline:
			return
		}
	}
}

// waitReady 等监听真正生效：Windows 上 New() 返回后 fsnotify 还可能要几十毫秒才开始投递事件，
// 紧接着写入会偶发漏事件（进程首跑必现）。用探针文件轮询到「能收到事件」为止并排干队列。
func waitReady(t *testing.T, w *Watcher, dir string) {
	t.Helper()
	probe := filepath.Join(dir, "probe.yml")
	for i := 0; i < 50; i++ {
		write(t, probe, fmt.Sprintf("probe-%d\n", i))
		if waitEvent(w, "probe.yml", 300*time.Millisecond) {
			drainEvents(w, 200*time.Millisecond)
			return
		}
	}
	t.Fatal("监听未在预期时间内生效")
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
}

// .proto（以及其它非集合文件）变更不得触发事件：设计文档 D3 的落地保证。
func TestWatcherIgnoresNonCollectionFiles(t *testing.T) {
	dir := t.TempDir()
	w := newTestWatcher(t, dir)

	write(t, filepath.Join(dir, "protos", "greeter.proto"), "syntax = \"proto3\";\n")
	if waitEvent(w, "greeter.proto", 500*time.Millisecond) {
		t.Fatal(".proto 变更不应触发集合外部改动事件")
	}

	// 同一时间点，证书类文件同样不参与
	write(t, filepath.Join(dir, "certs", "ca.pem"), "-----BEGIN CERTIFICATE-----\n")
	if waitEvent(w, "ca.pem", 300*time.Millisecond) {
		t.Fatal(".pem 变更不应触发集合外部改动事件")
	}
}

// 集合文件仍然照常触发（含子目录、docs 的 .md）。
func TestWatcherTriggersOnCollectionFiles(t *testing.T) {
	dir := t.TempDir()
	// 目录先建好：否则「新建目录 + 立刻写文件」这一拍里，操作系统可能只报目录事件
	// （新建目录的 watch 还没挂上），断言具体文件路径就会偶发失败 —— 见下一个用例对这条兜底的说明。
	for _, sub := range []string{"api", "docs"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("建目录失败: %v", err)
		}
	}
	w := newTestWatcher(t, dir)
	drainEvents(w, 200*time.Millisecond)

	write(t, filepath.Join(dir, "api", "ping.yml"), "info:\n    name: ping\n")
	if !waitEvent(w, "api/ping.yml", 3*time.Second) {
		t.Fatal(".yml 变更应触发事件")
	}

	write(t, filepath.Join(dir, "docs", "note.md"), "# note\n")
	if !waitEvent(w, "docs/note.md", 3*time.Second) {
		t.Fatal(".md 变更应触发事件")
	}
}

// 「先建目录再写文件」必须至少产生一个事件（目录或文件都算）：
// 消费方拿到任何事件都是整树重扫，所以目录事件足以兜底 —— 这条契约不能被悄悄改掉。
func TestWatcherReportsNewDirectoryAtLeastOnce(t *testing.T) {
	dir := t.TempDir()
	w := newTestWatcher(t, dir)

	sub := filepath.Join(dir, "brand-new")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	write(t, filepath.Join(sub, "a.yml"), "info:\n    name: a\n")
	// 匹配 "brand-new"：目录事件（brand-new）与文件事件（brand-new/a.yml）都能命中
	if !waitEvent(w, "brand-new", 3*time.Second) {
		t.Fatal("新建目录（含其中写文件）至少应产生一个事件")
	}
}

// 自写回环（无指纹）：窗口内一律忽略。
func TestIgnoreWindowSwallowsEvents(t *testing.T) {
	dir := t.TempDir()
	w := newTestWatcher(t, dir)
	write(t, filepath.Join(dir, "api", "keep.yml"), "a\n")

	w.Ignore("api/keep.yml", time.Second)
	drainEvents(w, 200*time.Millisecond) // 排干首写的事件，避免它被当成「窗口内的事件」
	write(t, filepath.Join(dir, "api", "keep.yml"), "changed\n")
	if waitEvent(w, "api/keep.yml", 500*time.Millisecond) {
		t.Fatal("窗口内的事件应被忽略")
	}
}

// 自写回环（带指纹）：自己写的那份被忽略，但同一窗口里**别人改的内容**必须上报 ——
// 否则「客户端刚保存完，编辑器立刻又改」会被静默吞掉（干净页签不刷新、也不标冲突）。
func TestIgnoreWriteKeepsExternalChanges(t *testing.T) {
	dir := t.TempDir()
	w := newTestWatcher(t, dir)
	path := filepath.Join(dir, "api", "ping.yml")
	write(t, path, "mine\n")

	// 先声明「我写了 mine」→ 自己的写事件被忽略
	drainEvents(w, 200*time.Millisecond) // 先排干上面那次写入（它不是本段要断言的对象）
	w.IgnoreWrite("api/ping.yml", fileSHA256(path), time.Second)
	write(t, path, "mine\n")
	if waitEvent(w, "api/ping.yml", 400*time.Millisecond) {
		t.Fatal("与我方指纹一致的内容应被忽略")
	}

	// 同一窗口内别人改成不同内容 → 必须上报
	w.IgnoreWrite("api/ping.yml", fileSHA256(path), time.Second)
	write(t, path, "someone-else\n")
	if !waitEvent(w, "api/ping.yml", 3*time.Second) {
		t.Fatal("窗口内内容已变的事件应上报（别人的改动不能被吞掉）")
	}
}

func TestExtensionSet(t *testing.T) {
	set := extensionSet([]string{"YML", " .yaml ", "", ".md"})
	for _, want := range []string{".yml", ".yaml", ".md"} {
		if !set[want] {
			t.Errorf("集合缺少 %s: %v", want, set)
		}
	}
	if len(set) != 3 {
		t.Errorf("集合大小 = %d，期望 3: %v", len(set), set)
	}
}
