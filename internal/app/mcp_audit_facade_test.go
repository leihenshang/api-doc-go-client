package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"api-doc-go-client/internal/mcpaudit"
)

// 审计门面：写入→查询→导出→清空 的最小闭环（隔离配置目录）。
func TestAuditFacadeLifecycle(t *testing.T) {
	isolateConfigDir(t)
	a := NewApp()

	aud := a.ensureAuditor()
	if aud == nil {
		t.Fatal("隔离配置目录下应能创建审计器")
	}
	aud.Log(mcpaudit.Event{TS: time.Now().Format(time.RFC3339Nano), TSMs: time.Now().UnixMilli(),
		Tool: "set_env_var", Risk: "high", ResultOK: false, ResultKind: "denied"})

	page, err := a.ListMCPAudit(AuditQueryDTO{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && page.Total == 0 {
		page, _ = a.ListMCPAudit(AuditQueryDTO{Limit: 50})
		time.Sleep(20 * time.Millisecond)
	}
	if page.Total != 1 {
		t.Fatalf("应查到 1 条审计，实际 %d", page.Total)
	}

	// 导出到指定文件（无 Wails ctx 的路径）
	dest := filepath.Join(t.TempDir(), "out.jsonl")
	got, err := a.ExportMCPAudit(dest)
	if err != nil {
		t.Fatal(err)
	}
	if got != dest {
		t.Fatalf("导出路径回显不对：%s", got)
	}
	if b, err := os.ReadFile(dest); err != nil || len(b) == 0 {
		t.Fatalf("导出文件应有内容：%v", err)
	}

	// 清空：当前文件归零，产生一份归档
	if err := a.ClearMCPAudit(); err != nil {
		t.Fatal(err)
	}
	page, _ = a.ListMCPAudit(AuditQueryDTO{Limit: 50})
	if page.Total != 0 {
		t.Fatalf("清空后应为 0 条，实际 %d", page.Total)
	}
	a.closeAuditor()
}

// 导出未给路径且无桌面 ctx 时应报错（devserver 必须显式传路径）。
func TestAuditExportRequiresPathHeadless(t *testing.T) {
	isolateConfigDir(t)
	a := NewApp()
	a.ensureAuditor()
	if _, err := a.ExportMCPAudit(""); err == nil {
		t.Fatal("无 ctx 时空路径应报错，而不是静默成功")
	}
	a.closeAuditor()
}
