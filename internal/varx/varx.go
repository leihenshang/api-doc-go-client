// Package varx 变量替换：把文本中的 {{name}} 渲染为环境变量值。
// 占位符语法、优先级、内置动态变量与掩码规则统一由共享包 github.com/leihenshang/api-doc-go-share/varx 实现，
// 本包只保留既有的导出签名（成熟后调用方可直接换成共享包）。
package varx

import (
	"crypto/rand"
	"encoding/hex"

	share "github.com/leihenshang/api-doc-go-share/varx"
)

// Resolve 渲染文本；返回渲染结果与「引用了但未定义」的变量名（去重、按出现顺序）。
func Resolve(text string, vars map[string]string) (string, []string) {
	return share.ResolveWithBuiltins(text, table(vars), share.DefaultBuiltins())
}

// CollectNames 收集文本中引用的变量名（含未定义），去重按出现顺序。
func CollectNames(text string) []string {
	return share.Names(text)
}

// CollectValues 收集文本中引用到、且已在 vars 中定义的变量取值（名字 → 值），
// 供前端在悬停变量时展示「会被替换成什么」。未定义的变量不出现（缺失名单由 Resolve 的 missing 给出）；
// 内置动态变量不参与（每次发送都会重新生成）。没有命中时返回 nil。
func CollectValues(text string, vars map[string]string) map[string]string {
	if text == "" || len(vars) == 0 {
		return nil
	}
	out := map[string]string{}
	for _, name := range CollectNames(text) {
		if v, ok := vars[name]; ok {
			out[name] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Builtins 内置动态变量的当前取值。
func Builtins() map[string]string {
	return share.DefaultBuiltins().Values()
}

// RandomHex 供测试或调试使用的小工具。
func RandomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// table 把「单层环境变量」映射为共享层解析表：客户端当前只有一层环境，按项目通用层参与（不挑环境）。
func table(vars map[string]string) *share.Table {
	list := make([]share.Var, 0, len(vars))
	for name, value := range vars {
		list = append(list, share.Var{Name: name, Value: value, Scope: share.ScopeProjectCommon, Enabled: true})
	}
	return share.Build(list, 0)
}
