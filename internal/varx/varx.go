// Package varx 变量替换：把文本中的 {{name}} 渲染为环境变量值。
//
// 历史：共享包占位符正则一度只允许 [A-Za-z0-9_]，用户在环境里常用的 clxone-ipam、
// a.b 等宽名会被整串漏匹配（既不替换也不报缺失），本包曾为此自建 alias 改写 shim。
// 审核 H-4（2026-10-19）把宽名字符集（含 `-`/`.`）连同默认值段统一收进了共享包
// share/varx，两端解析语义就此归一——本包收缩为**薄转发层**，不再有任何私有语义。
package varx

import (
	"crypto/rand"
	"encoding/hex"

	share "github.com/leihenshang/api-doc-go-share/varx"
)

// Resolve 渲染文本；返回渲染结果与「引用了但未定义」的变量名（去重、按出现顺序）。
// 支持嵌套引用（值里再含 {{...}}，环保留原文并记缺失）与默认值段 {{name:默认值}}。
func Resolve(text string, vars map[string]string) (string, []string) {
	return share.ResolveWithBuiltins(text, table(vars), share.DefaultBuiltins())
}

// CollectNames 收集文本中引用的普通变量名（不含内置 $变量），去重并按出现顺序返回。
func CollectNames(text string) []string {
	return share.Names(text)
}

// CollectValues 收集文本中引用到、且已在 vars 中定义的变量取值（名字 → 值），
// 供前端在悬停变量时展示「会被替换成什么」。未定义的变量不出现（缺失名单由 Resolve 给出）；
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
