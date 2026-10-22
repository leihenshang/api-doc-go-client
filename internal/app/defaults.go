package app

import (
	"api-doc-go-client/internal/collection"

	share "github.com/leihenshang/api-doc-go-share/collection"
)

// CollectionDefaults 集合级公共请求参数（前端弹窗读写契约）。
type CollectionDefaults struct {
	Headers []collection.KV  `json:"headers"`
	Auth    *collection.Auth `json:"auth,omitempty"`
}

// GetCollectionDefaults 读取当前集合的公共请求参数；未配置时返回空结构（headers 为空数组）。
func (a *App) GetCollectionDefaults() (*CollectionDefaults, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	out := &CollectionDefaults{Headers: []collection.KV{}}
	d := c.Defaults()
	if d == nil {
		return out, nil
	}
	if d.Headers != nil {
		out.Headers = d.Headers
	}
	out.Auth = d.Auth
	return out, nil
}

// SetCollectionDefaults 保存当前集合的公共请求参数（全空 = 清除 defaults 段）。
func (a *App) SetCollectionDefaults(d CollectionDefaults) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.SetDefaults(&share.Defaults{Headers: d.Headers, Auth: d.Auth})
}

// withCollectionDefaults 把当前集合的公共请求头/认证合并到请求副本上（HTTP 请求）。
// 发送、代码生成与 MCP 发送共用，保证「预览到的就是实际发出的」。
func (a *App) withCollectionDefaults(r *collection.Request) *collection.Request {
	if r == nil || r.IsGRPC() {
		return r
	}
	c, err := a.requireCollection()
	if err != nil {
		return r
	}
	return collection.MergeDefaults(r, c.Defaults())
}
