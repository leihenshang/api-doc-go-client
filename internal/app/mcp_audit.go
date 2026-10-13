package app

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"api-doc-go-client/internal/config"
	"api-doc-go-client/internal/mcpaudit"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// auditFileName 审计日志文件名（落用户配置目录；0600，不进集合/不同步）。
const auditFileName = "audit.log.jsonl"

// ensureAuditor 惰性创建进程级审计器并把实时事件桥到应用事件总线。
// 并发安全；创建失败返回 nil（审计 fail-open，绝不影响工具调用）。
func (a *App) ensureAuditor() mcpaudit.Auditor {
	a.authzMu.Lock()
	defer a.authzMu.Unlock()
	if a.auditor != nil {
		return a.auditor
	}
	dir, err := config.Dir()
	if err != nil {
		return nil
	}
	aud, err := mcpaudit.New(filepath.Join(dir, auditFileName), nil)
	if err != nil {
		return nil
	}
	a.auditor = aud
	// 实时扇出：审计事件 → Wails 事件 + devserver SSE（前端按 mcp:audit 增量刷新）。
	ch, cancel := aud.Subscribe()
	a.auditCancel = cancel
	go func() {
		for ev := range ch {
			a.emitAppEvent("mcp:audit", ev)
		}
	}()
	return aud
}

// AuditQueryDTO 审计查询入参（Wails/反射桥用；时间用 unix 毫秒）。
type AuditQueryDTO struct {
	Tool    string `json:"tool"`
	Project string `json:"project"`
	Risk    string `json:"risk"`
	Search  string `json:"search"`
	Ok      *bool  `json:"ok,omitempty"`
	SinceMS int64  `json:"sinceMs,omitempty"`
	UntilMS int64  `json:"untilMs,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	Offset  int    `json:"offset,omitempty"`
}

// ListMCPAudit 按条件倒序分页查询审计记录。
func (a *App) ListMCPAudit(q AuditQueryDTO) (*mcpaudit.Page, error) {
	aud := a.ensureAuditor()
	if aud == nil {
		return nil, errors.New("审计器不可用（配置目录不可写）")
	}
	q2 := mcpaudit.Query{
		Tool: q.Tool, Project: q.Project, Risk: q.Risk, Search: q.Search,
		Ok: q.Ok, Limit: q.Limit, Offset: q.Offset,
	}
	if q.SinceMS > 0 {
		q2.Since = time.UnixMilli(q.SinceMS)
	}
	if q.UntilMS > 0 {
		q2.Until = time.UnixMilli(q.UntilMS)
	}
	return aud.List(context.Background(), q2)
}

// ExportMCPAudit 导出当前审计日志。dest 为空时弹系统保存框（桌面态）；
// devserver/测试可直接传目标路径。返回最终保存路径；用户取消返回空串且不视为错误。
func (a *App) ExportMCPAudit(dest string) (string, error) {
	aud := a.ensureAuditor()
	if aud == nil {
		return "", errors.New("审计器不可用（配置目录不可写）")
	}
	if dest == "" {
		a.mu.Lock()
		ctx := a.ctx
		a.mu.Unlock()
		if ctx == nil {
			return "", errors.New("当前形态不支持系统保存框，请传入目标路径")
		}
		chosen, err := runtime.SaveFileDialog(ctx, runtime.SaveDialogOptions{
			Title:           "导出 MCP 审计日志",
			DefaultFilename: auditFileName,
			Filters: []runtime.FileFilter{{
				DisplayName: "JSON Lines (*.jsonl)",
				Pattern:     "*.jsonl",
			}},
		})
		if err != nil {
			return "", err
		}
		dest = chosen // 用户取消时为空
	}
	if dest == "" {
		return "", nil
	}
	if err := aud.ExportTo(dest); err != nil {
		return "", err
	}
	return dest, nil
}

// ClearMCPAudit 归档当前审计文件并新开空文件（先留痕再清）。
func (a *App) ClearMCPAudit() error {
	aud := a.ensureAuditor()
	if aud == nil {
		return errors.New("审计器不可用（配置目录不可写）")
	}
	return aud.Clear()
}

// closeAuditor 退出时刷新并关闭审计文件（OnShutdown 调用）。
func (a *App) closeAuditor() {
	a.authzMu.Lock()
	aud := a.auditor
	cancel := a.auditCancel
	a.auditor = nil
	a.auditCancel = nil
	a.authzMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if aud != nil {
		_ = aud.Close()
	}
}
