package mcpaudit

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func newTestAuditor(t *testing.T, opts ...Option) *JSONLAuditor {
	t.Helper()
	path := filepath.Join(t.TempDir(), "audit.log.jsonl")
	a, err := New(path, nil, opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func sample(tool string, ok bool, ms int64) Event {
	k := KindOK
	if !ok {
		k = KindBizError
	}
	return Event{TSMs: ms, Tool: tool, Risk: "high", Project: "demo",
		ResultOK: ok, ResultKind: string(k), ResultSummary: "done"}
}

// waitN 等 ring 出现 n 条（写循环异步）。
func waitN(t *testing.T, a *JSONLAuditor, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		got := len(a.ring)
		a.mu.Unlock()
		if got >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("2s 内未落满 %d 条", n)
}

func TestAuditAppendAndList(t *testing.T) {
	a := newTestAuditor(t)
	a.Log(sample("create_request", true, 1000))
	a.Log(sample("delete_request", false, 2000))
	a.Log(sample("list_envs", true, 3000))
	waitN(t, a, 3)
	_ = a.Close() // 强制 flush

	// 重新打开（模拟重启）：历史可查、权限 0600
	reopened, err := New(a.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	page, err := reopened.List(context.Background(), Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 {
		t.Fatalf("应读到 3 条，得到 %d", page.Total)
	}
	if page.Events[0].TSMs != 3000 || page.Events[2].TSMs != 1000 {
		t.Fatalf("应倒序排列：%d %d %d", page.Events[0].TSMs, page.Events[1].TSMs, page.Events[2].TSMs)
	}
	// Windows/NTFS 不体现 unix 权限位（Go 创建文件恒显 0666），仅类 Unix 校验 0600。
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(a.path); st.Mode().Perm() != fileMode {
			t.Fatalf("权限应为 0600，实际 %v", st.Mode().Perm())
		}
	}
	if page.Events[0].Sequence == 0 {
		t.Fatal("seq 应单调赋值")
	}
}

func TestAuditQueryFilters(t *testing.T) {
	a := newTestAuditor(t)
	a.Log(sample("create_request", true, 1000))
	a.Log(sample("delete_request", false, 2000))
	a.Log(sample("delete_request", true, 2500))
	waitN(t, a, 3)

	ok := true
	if p, _ := a.List(context.Background(), Query{Tool: "delete_request", Limit: 50}); p.Total != 2 {
		t.Fatalf("tool 过滤应 2 条，得到 %d", p.Total)
	}
	if p, _ := a.List(context.Background(), Query{Ok: &ok, Limit: 50}); p.Total != 2 {
		t.Fatalf("ok 过滤应 2 条，得到 %d", p.Total)
	}
	if p, _ := a.List(context.Background(), Query{Risk: "low", Limit: 50}); p.Total != 0 {
		t.Fatalf("risk=low 应 0 条")
	}
	if p, _ := a.List(context.Background(), Query{Search: "done", Limit: 50}); p.Total != 3 {
		t.Fatalf("search 应 3 条，得到 %d", p.Total)
	}
	if p, _ := a.List(context.Background(), Query{Since: time.UnixMilli(1500), Limit: 50}); p.Total != 2 {
		t.Fatalf("since 过滤应 2 条，得到 %d", p.Total)
	}
	if p, _ := a.List(context.Background(), Query{Limit: 1, Offset: 1}); len(p.Events) != 1 || p.Total != 3 {
		t.Fatalf("分页 total=3、页内 1 条，得到 total=%d n=%d", p.Total, len(p.Events))
	}
}

func TestAuditSubscribeFanout(t *testing.T) {
	a := newTestAuditor(t)
	ch1, cancel1 := a.Subscribe()
	ch2, cancel2 := a.Subscribe()
	defer cancel2()
	a.Log(sample("send_request", true, 1))
	select {
	case e := <-ch1:
		if e.Tool != "send_request" {
			t.Fatal(e.Tool)
		}
	case <-time.After(time.Second):
		t.Fatal("订阅者1未收到")
	}
	select {
	case <-ch2:
	case <-time.After(time.Second):
		t.Fatal("订阅者2未收到")
	}
	cancel1()
	a.Log(sample("send_request", true, 2))
	select {
	case _, ok := <-ch1:
		if ok {
			t.Fatal("退订后不应再收到")
		}
	default:
	}
}

func TestAuditRotateAndPrune(t *testing.T) {
	// 8KB 轮转、保留 2 份：写约 40KB 应产生多份归档且只剩 2 份
	a := newTestAuditor(t, WithMaxFileBytes(8*1024), WithKeepArchives(2))
	big := strings.Repeat("x", 500)
	for i := 0; i < 80; i++ {
		ev := sample("create_request", true, int64(1000+i))
		ev.ResultSummary = big
		a.Log(ev)
	}
	// 8KB/份 + 保留 2 份：写到 40KB 期间应已发生轮转并持续剪枝。
	// 等出现归档文件即可（写入总量远超阈值），不按总条数等（旧归档会被剪掉）。
	globPat := filepath.Join(filepath.Dir(a.path), "audit.log.*.jsonl")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if m, _ := filepath.Glob(globPat); len(m) > 0 {
			goto rotated
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("3s 内未发生轮转")
rotated:
	if err := a.Clear(); err != nil { // Clear 也会归档一次，顺带验证
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(a.path), "audit.log.*.jsonl"))
	if len(matches) > 2 {
		t.Fatalf("归档最多保留 2 份，实际 %d：%v", len(matches), matches)
	}
	if len(matches) == 0 {
		t.Fatal("应至少产生过归档")
	}
}

func TestAuditClearKeepsArchive(t *testing.T) {
	a := newTestAuditor(t)
	a.Log(sample("set_env_var", false, 1))
	waitN(t, a, 1)
	if err := a.Clear(); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(a.path), "audit.log.*.jsonl"))
	if len(matches) != 1 {
		t.Fatalf("清空前应归档 1 份，实际 %d", len(matches))
	}
	st, err := os.Stat(a.path)
	if err != nil || st.Size() != 0 {
		t.Fatalf("清空后当前文件应为空，err=%v", err)
	}
}

func TestAuditCorruptLineSkipped(t *testing.T) {
	a := newTestAuditor(t)
	a.Log(sample("list_envs", true, 1))
	waitN(t, a, 1)
	_ = a.Close()
	// 追加一行坏 JSON
	f, err := os.OpenFile(a.path, os.O_APPEND|os.O_WRONLY, fileMode)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("{not json\n")
	_ = f.Close()

	reopened, err := New(a.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	page, err := reopened.List(context.Background(), Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Corrupt != 1 || page.Total != 1 {
		t.Fatalf("坏行应跳过计 1、正常 1 条，得到 corrupt=%d total=%d", page.Corrupt, page.Total)
	}
}

func TestAuditExport(t *testing.T) {
	a := newTestAuditor(t)
	a.Log(sample("send_request", true, 1))
	waitN(t, a, 1)
	dest := filepath.Join(t.TempDir(), "export.jsonl")
	if err := a.ExportTo(dest); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "send_request") {
		t.Fatal("导出内容不对")
	}
}

func TestRedactText(t *testing.T) {
	in := "Authorization: Bearer abc.def-123 ok"
	out := RedactText(in)
	if strings.Contains(out, "abc.def-123") {
		t.Fatalf("Bearer 令牌应被掩码：%s", out)
	}
	if !strings.Contains(out, "••••••") {
		t.Fatalf("应包含掩码：%s", out)
	}
}
