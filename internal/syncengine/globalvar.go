package syncengine

// R2：全局变量（服务端 scope=3，跨项目共享）的同步落地。
//
// 全局变量不属于任何项目：既不在项目变更流里，也不随项目快照按项目取数，
// 服务端因此在每次增量/全量响应里额外附带（见服务端 service/sync.go 的 globalVarItems）。
// 客户端把它落到集合根的 globals.yml，变量解析时优先级最低（低于环境变量）。
//
// 客户端不回推全局变量：它是服务端管理员维护的，客户端只读镜像。

import (
	"api-doc-go-client/internal/collection"
)

// serverScopeGlobal 服务端的「全局变量」作用域值（model.VarScopeGlobal = 3）。
//
// 注意与 share/varx.ScopeGlobal 区分：后者是客户端内部的解析排名（4），
// 两套编号含义不同，别混用。
const serverScopeGlobal = 3

// applyGlobalVar 把服务端下发的全局变量写进集合根 globals.yml。
func (e *Engine) applyGlobalVar(uid string, p Payload) error {
	if p.Name == "" {
		return nil
	}
	// H-11 同口径：服务端没下发明文（调用方无明文权限）时保留本地已有值，不用空值覆盖。
	if p.Redacted {
		return nil
	}
	value := p.Value
	if p.Secret {
		if dec, derr := e.cipher().Open(p.Value); derr == nil {
			value = dec
		}
	}
	enabled := p.Enabled == nil || *p.Enabled
	if _, err := e.Coll.UpsertGlobal(collection.Var{
		UID: uid, Name: p.Name, Value: value, Enabled: enabled, Secret: p.Secret,
	}); err != nil {
		return err // 落盘失败必须上报：静默会让本地与服务端值不一致
	}
	// 记基线：否则下一轮会把刚下发的全局变量当本地脏数据推回去。
	if e.Bind.VarHashes == nil {
		e.Bind.VarHashes = map[string]string{}
	}
	e.Bind.VarHashes[uid] = varHashOf(collection.Var{
		Name: p.Name, Value: value, Enabled: enabled, Secret: p.Secret,
	})
	return nil
}
