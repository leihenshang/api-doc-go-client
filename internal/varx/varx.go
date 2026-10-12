// Package varx 变量替换：把文本中的 {{name}} 渲染为环境变量值。
// 占位符语法、优先级、内置动态变量与掩码规则统一由共享包 github.com/leihenshang/api-doc-go-share/varx 实现，
// 本包只保留既有的导出签名（成熟后调用方可直接换成共享包）。
//
// 已知差异：共享包的占位符正则只允许 [A-Za-z0-9_] 作为变量名，不支持连字符（-）与点（.）。
// 但用户在环境变量里经常使用 clxone-ipam 这类带连字符的名字，若直接交给共享包，
// {{clxone-ipam}} 会整体匹配不上——既不替换也不被识别为变量（悬停不提示、发送原样发出）。
// 因此本包在调用共享解析前，把含非法字符的变量名透明转写成合法别名
// （clxone-ipam -> clxone_ipam），解析语义（嵌套、$ 内置动态变量、掩码、缺失名单）完全由共享包负责，
// 缺失名单在返回前再还原成用户写的原名，保证 UI 提示准确。
package varx

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"

	share "github.com/leihenshang/api-doc-go-share/varx"
)

// broadNameRE 比共享包更宽：允许变量名含字母数字下划线、连字符、点。
// 三个捕获组：1=左空白 2=名字 3=右空白，便于原样保留 {{ ... }} 内的空格。
var broadNameRE = regexp.MustCompile(`\{\{(\s*)(\$?[A-Za-z_][A-Za-z0-9_.\-]*)(\s*)\}\}`)

// illegalCharRE 匹配共享包占位符正则不允许出现在变量名里的字符。
var illegalCharRE = regexp.MustCompile(`[^A-Za-z0-9_]`)

// Resolve 渲染文本；返回渲染结果与「引用了但未定义」的变量名（去重、按出现顺序）。
func Resolve(text string, vars map[string]string) (string, []string) {
	if text == "" {
		return text, nil
	}
	// 1) 收集正文与所有变量值里引用的变量名，建立「原名 -> 合法别名」映射。
	origToAlias, aliasToOrig := buildAliasMap(text, vars)
	// 2) 把正文与变量值里的占位符名字替换成别名（共享包正则只认合法名）。
	tt := rewrite(text, origToAlias)
	tv := make(map[string]string, len(vars))
	for k, v := range vars {
		tv[origToAlias[k]] = rewrite(v, origToAlias)
	}
	// 3) 交给共享包解析（别名都是合法名，嵌套 / 内置 / 掩码全部保留）。
	resolved, missingAliases := share.ResolveWithBuiltins(tt, table(tv), share.DefaultBuiltins())
	// 4) 共享包对未定义的占位符会原样保留（此时是别名形态），还原回用户写的原名，
	//    否则报错信息里会出现 {{clxone_ipam}} 这种用户从未写过的形态。已解析成功的片段不受影响。
	resolved = rewrite(resolved, aliasToOrig)
	// 5) 把缺失的别名还原成原名返回，UI 才能给出准确提示。
	return resolved, mapBack(missingAliases, aliasToOrig)
}

// CollectNames 收集文本中引用的普通变量名（不含内置变量），去重并按出现顺序返回。
// 用更宽的 broadNameRE，使 clxone-ipam 这类带连字符的名字也能被正确识别（供悬停展示取值）。
func CollectNames(text string) []string {
	if text == "" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, m := range broadNameRE.FindAllStringSubmatch(text, -1) {
		name := strings.TrimSpace(m[2])
		if name == "" || strings.HasPrefix(name, "$") {
			continue
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
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

// buildAliasMap 收集所有需要参与的变量名（正文引用、变量值引用、变量键本身），
// 为每个名字生成共享包正则允许的唯一别名，返回 原名->别名 与 别名->原名。
func buildAliasMap(text string, vars map[string]string) (map[string]string, map[string]string) {
	sources := []string{text}
	for _, v := range vars {
		sources = append(sources, v)
	}
	taken := map[string]bool{}
	origToAlias := map[string]string{}
	aliasToOrig := map[string]string{}
	add := func(name string) {
		if name == "" {
			return
		}
		if _, ok := origToAlias[name]; ok {
			return
		}
		if strings.HasPrefix(name, "$") {
			// 内置动态变量：共享包原样处理，名字本就不含非法字符，保持原名。
			origToAlias[name] = name
			aliasToOrig[name] = name
			return
		}
		alias := sanitizeName(name, taken)
		origToAlias[name] = alias
		aliasToOrig[alias] = name
	}
	for _, s := range sources {
		for _, m := range broadNameRE.FindAllStringSubmatch(s, -1) {
			add(strings.TrimSpace(m[2]))
		}
	}
	for k := range vars {
		add(k)
	}
	return origToAlias, aliasToOrig
}

// sanitizeName 把共享包不允许的字符（连字符、点等）替换成下划线，并保证别名在本次调用内唯一。
func sanitizeName(name string, taken map[string]bool) string {
	alias := illegalCharRE.ReplaceAllString(name, "_")
	if alias == "" {
		alias = "v"
	}
	if !taken[alias] {
		taken[alias] = true
		return alias
	}
	i := 2
	for taken[alias+"_"+strconv.Itoa(i)] {
		i++
	}
	alias = alias + "_" + strconv.Itoa(i)
	taken[alias] = true
	return alias
}

// rewrite 把文本里的 {{name}} 占位符名字替换成别名（保留左右空白与 $ 前缀）。
func rewrite(text string, origToAlias map[string]string) string {
	if text == "" {
		return text
	}
	return broadNameRE.ReplaceAllStringFunc(text, func(m string) string {
		sub := broadNameRE.FindStringSubmatch(m)
		name := strings.TrimSpace(sub[2])
		alias, ok := origToAlias[name]
		if !ok {
			return m
		}
		return "{{" + sub[1] + alias + sub[3] + "}}"
	})
}

// mapBack 把缺失的别名还原成原名，去重保序。
func mapBack(missingAliases []string, aliasToOrig map[string]string) []string {
	if len(missingAliases) == 0 {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, a := range missingAliases {
		orig := a
		if o, ok := aliasToOrig[a]; ok {
			orig = o
		}
		if !seen[orig] {
			seen[orig] = true
			out = append(out, orig)
		}
	}
	return out
}

// table 把「单层环境变量」映射为共享层解析表：客户端当前只有一层环境，按项目通用层参与（不挑环境）。
func table(vars map[string]string) *share.Table {
	list := make([]share.Var, 0, len(vars))
	for name, value := range vars {
		list = append(list, share.Var{Name: name, Value: value, Scope: share.ScopeProjectCommon, Enabled: true})
	}
	return share.Build(list, 0)
}
