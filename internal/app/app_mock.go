package app

// 本地 Mock 服务的启动/停止与状态（P2-8：从 app.go 按子域拆出）。

import (
	"api-doc-go-client/internal/mocksrv"
)

// ---- 本地 Mock 服务（H7）----

// StartMock 在**活动根**上启动本地 Mock（port<=0 随机端口）。
//
// 同一时刻只允许一个 Mock：它绑着某个集合的请求定义，若在 A 启动后切到 B 再启动，
// 先把 A 那份停掉（端口/状态栏都只反映一个），避免「看起来在跑、匹配的却是另一个目录」。
func (a *App) StartMock(port int) (*mocksrv.Status, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	root := rootKey(c.Dir)
	a.mu.Lock()
	var old *mocksrv.Server
	if a.mock != nil && a.mockRoot != root {
		old, a.mock, a.mockRoot = a.mock, nil, ""
	}
	if a.mock == nil {
		a.mock = mocksrv.New(c)
		a.mockRoot = root
	}
	m := a.mock
	a.mu.Unlock()
	if old != nil {
		_ = old.Stop()
	}
	return m.Start(port)
}

// StopMock 停止本地 Mock（无论它当前服务哪个根）。
func (a *App) StopMock() error {
	a.mu.Lock()
	m := a.mock
	a.mock, a.mockRoot = nil, ""
	a.mu.Unlock()
	if m == nil {
		return nil
	}
	return m.Stop()
}

// MockStatus Mock 运行状态：只反映「活动根」上的 Mock（属于别的根的实例不算在活动根头上）。
func (a *App) MockStatus() *mocksrv.Status {
	a.mu.Lock()
	m, root, active := a.mock, a.mockRoot, a.activeRoot
	a.mu.Unlock()
	if m == nil || root != active {
		return &mocksrv.Status{Running: false}
	}
	return m.Status()
}
