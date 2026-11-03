package syncengine

// R3：接口生命周期元数据（草稿 / 已发布 / 已废弃 + 版本 + 废弃时间）的同步落地。
//
// 约定：
//   - 服务端是这类状态的唯一权威（Web 端可标记「已废弃」，客户端没有对应设置项）。
//   - 缺键不改：旧服务端不带这三个键时，本地已有状态保持不变（不因同步被清空）。
//   - 客户端不回推：推送载荷不带这三个键，服务端状态不会被客户端同步覆盖。

import (
	"api-doc-go-client/internal/collection"
)

// applyLifecycle 把服务端下发的生命周期元数据写进请求对象。
func applyLifecycle(r *collection.Request, p Payload) {
	if r == nil {
		return
	}
	if p.ApiStatus != 0 {
		r.Status = p.ApiStatus
	}
	if p.Version != "" {
		r.Version = p.Version
	}
	if p.DeprecatedAt != "" {
		r.DeprecatedAt = p.DeprecatedAt
	}
}
