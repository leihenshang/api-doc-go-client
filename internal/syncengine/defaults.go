package syncengine

// U6：集合级公共参数（服务端 project.defaults，项目级单例）的只读镜像。
//
// 权威方是服务端（与全局变量、接口生命周期一致）：客户端**不回推**，
// 拿到就写进本地清单（manifest 的 defaults 段），发送时由 MergeDefaults 在
// 「请求自己没有配 header/auth」的前提下套用。
//
// 为什么不做双向 LWW：defaults 是整份 JSON，合并语义（按名字并集？按位置？）没有客观答案，
// 双方各改一份必然打架；只读镜像的语义最清晰，也不会把本地实验性改动推给全队。

import (
	"encoding/json"
	"fmt"
	"strings"

	sharecollection "github.com/leihenshang/api-doc-go-share/collection"
)

// applyDefaults 把服务端下发的公共参数写进本地清单。
//
// 空串 = 服务端清空：本地镜像也要跟着清空（否则队友删掉的公共头会一直生效）。
func (e *Engine) applyDefaults(p Payload) error {
	raw := strings.TrimSpace(p.Defaults)
	if raw == "" {
		return e.Coll.SetDefaults(nil)
	}
	var d sharecollection.Defaults
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return fmt.Errorf("集合公共参数解析失败：%w", err)
	}
	return e.Coll.SetDefaults(&d)
}
