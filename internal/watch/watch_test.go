package watch

import (
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
	return w
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
	w := newTestWatcher(t, dir)

	write(t, filepath.Join(dir, "api", "ping.yml"), "info:\n    name: ping\n")
	if !waitEvent(w, "api/ping.yml", 3*time.Second) {
		t.Fatal(".yml 变更应触发事件")
	}

	write(t, filepath.Join(dir, "docs", "note.md"), "# note\n")
	if !waitEvent(w, "docs/note.md", 3*time.Second) {
		t.Fatal(".md 变更应触发事件")
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
