package app

import (
	"encoding/json"
	"strings"
	"time"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/history"
	"api-doc-go-client/internal/runner"
)

// 发送历史：落盘单文件 JSONL，按「所属工作目录」隔离展示（item 19）。

// recordHistory 落一条历史：失败也记，便于回溯排查；带请求快照与响应快照供回看（item 19）。
//
// 条目带「所属工作目录」：多根并存后历史按目录隔离展示（切到哪个根只看哪个根的记录）。
func (a *App) recordHistory(r *collection.Request, res *runner.Result, sendErr error) {
	a.mu.Lock()
	limit := a.settings.HistoryLimit
	root := a.activeRoot
	a.mu.Unlock()

	entry := history.Entry{
		Time: time.Now().UnixMilli(), UID: r.UID, Name: r.Name,
		Method: strings.ToUpper(r.Method), URL: r.URL, Root: root,
	}
	if snap, err := json.Marshal(r); err == nil {
		entry.Request = snap
	}
	switch {
	case sendErr != nil:
		entry.Error = sendErr.Error()
	case res != nil:
		entry.URL, entry.Status = res.URL, res.Status
		entry.TimeMS, entry.Size = res.TimeMS, res.Size
		// 响应快照：body 截断到 8KB（历史是单文件，防大响应撑爆）；headers 全量
		resSnap := struct {
			Status  int             `json:"status"`
			Headers []collection.KV `json:"headers"`
			Body    string          `json:"body"`
		}{res.Status, res.Headers, res.Body}
		if len(resSnap.Body) > 8192 {
			resSnap.Body = resSnap.Body[:8192] + "\n…（已截断）"
		}
		if b, err := json.Marshal(resSnap); err == nil {
			entry.Response = b
		}
	}
	_ = history.Append(entry, limit)
}

// ListHistory 活动根最近的发送记录（新 → 旧）；升级前无归属的旧记录也一并展示。
func (a *App) ListHistory(limit int) ([]history.Entry, error) {
	return history.List(limit, a.activeRootID())
}

// ClearHistory 清空**活动根**的发送历史（其它工作目录的记录保留）。
func (a *App) ClearHistory() error {
	return history.Clear(a.activeRootID())
}
