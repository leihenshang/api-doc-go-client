// Package varx 变量替换：把文本中的 {{name}} 渲染为环境变量值。
// 规则与设计文档《客户端与同步架构设计》一致：
//   - 未定义/未启用的变量保留原样，并把名字返回给调用方（UI 做告警）；
//   - 以 $ 开头的是内置动态变量（{{$uuid}} 等），不依赖环境。
package varx

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"
)

var pattern = regexp.MustCompile(`\{\{\s*(\$?[A-Za-z_][A-Za-z0-9_$]*)\s*\}\}`)

// Builtins 内置动态变量。
func Builtins() map[string]string {
	b := make(map[string]string, 4)
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	b["$uuid"] = fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
	now := time.Now()
	b["$timestamp"] = fmt.Sprintf("%d", now.Unix())
	b["$isoTimestamp"] = now.UTC().Format(time.RFC3339Nano)
	b["$randomInt"] = fmt.Sprintf("%d", now.UnixNano()%1000)
	return b
}

// Resolve 渲染文本；返回渲染结果与「引用了但未定义」的变量名（去重、按出现顺序）。
func Resolve(text string, vars map[string]string) (string, []string) {
	var missing []string
	seen := map[string]bool{}
	out := pattern.ReplaceAllStringFunc(text, func(m string) string {
		name := pattern.FindStringSubmatch(m)[1]
		if name == "" {
			return m
		}
		if name[0] == '$' {
			if v, ok := Builtins()[name]; ok {
				return v
			}
			return m
		}
		if v, ok := vars[name]; ok {
			return v
		}
		if !seen[name] {
			seen[name] = true
			missing = append(missing, name)
		}
		return m
	})
	return out, missing
}

// CollectNames 收集文本中引用的变量名（含未定义），去重按出现顺序。
func CollectNames(text string) []string {
	_, missing := Resolve(text, map[string]string{})
	return missing
}

// RandomHex 供测试或调试使用的小工具。
func RandomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
