package mcp

// 工具错误的机器可判别前缀（外部评审 T2）：
// 调用方此前只能匹配中文散文来区分「不存在 / 冲突 / 校验失败」，加上稳定前缀后可以
// 直接按 [CODE] 分流；文本其余部分保持原样，旧调用方不受影响。
//
// 约定：所有面向 MCP 的错误最终由 toolSet.wrap 输出为 `失败: [CODE] 说明`。
// 未显式分类的错误统一 [INTERNAL]，不要把内部实现细节（sqlite/文件路径）当成契约。

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	CodeNotFound  = "NOT_FOUND" // 项目/文档/接口/示例/变量不存在
	CodeConflict  = "CONFLICT"  // 同名冲突、ifMatch 不匹配
	CodeInvalid   = "INVALID"   // 参数缺失/取值非法
	CodeReadOnly  = "READONLY"  // 工作目录只读授权
	CodeForbidden = "FORBIDDEN" // 不在白名单 / 无权限（破坏性更名：原 AUTH）
	CodeLimit     = "LIMIT"     // 批量或分页上限
	CodeIO        = "IO"        // 读写磁盘失败
	CodeInternal  = "INTERNAL"  // 其它内部错误
)

// codedError 带错误码的包装（不改变底层错误链，错误判定仍可用 errors.Is/As）。
type codedError struct {
	code string
	err  error
}

func (e *codedError) Error() string { return "[" + e.code + "] " + e.err.Error() }
func (e *codedError) Unwrap() error { return e.err }

// coded 给错误加码；已带码的错误保持不变（避免重复前缀）。
func coded(code string, err error) error {
	if err == nil {
		return nil
	}
	var ce *codedError
	if errors.As(err, &ce) {
		return err
	}
	return &codedError{code: code, err: err}
}

// CodeOf 取错误码（无码时返回 INTERNAL）。
func CodeOf(err error) string {
	var ce *codedError
	if errors.As(err, &ce) {
		return ce.code
	}
	return CodeInternal
}

// withNotFound 只给「不存在」类错误加码，其它保持原样交给上层。
// （读文件失败在 MCP 语境里基本等价于条目不存在：权限问题已在 resolveWrite 拦掉。）
func withNotFound(err error) error { return coded(CodeNotFound, err) }

// withInvalid 参数/契约类错误。
func withInvalid(err error) error { return coded(CodeInvalid, err) }

// withLimit 批量/分页上限。
func withLimit(err error) error { return coded(CodeLimit, err) }

// ---- SDK schema 校验错误的中文转述 ----
//
// 原文形如：
//
//	validating "arguments": validating root: required: missing properties: ["uid"]
//	validating "arguments": validating root: unexpected additional properties ["uids"]
//
// 校验库只在「必填缺失 / 多余参数」两类里带参数名（嵌套的类型/枚举错误不带），
// 拿不到参数名时如实附上精简后的原文，好过让调用方去猜 "validating root" 是什么。

var (
	schemaMissingRe = regexp.MustCompile(`missing properties: \[(.*?)\]`)
	schemaExtraRe   = regexp.MustCompile(`unexpected additional properties \[(.*?)\]`)
	schemaQuotedRe  = regexp.MustCompile(`"([^"]+)"`)
)

// humanizeSchemaError 把 SDK 的英文入参校验错误翻成中文（tool 为空时用「本工具」）。
func humanizeSchemaError(tool, raw string) string {
	if tool == "" {
		tool = "本工具"
	}
	body := strings.TrimPrefix(raw, `validating "arguments": `)
	body = strings.TrimPrefix(body, "validating root: ")
	switch {
	case schemaMissingRe.MatchString(body):
		if m := schemaMissingRe.FindStringSubmatch(body); m != nil {
			return fmt.Sprintf("缺少必填参数：%s（工具 %s）", quotedList(m[1]), tool)
		}
	case schemaExtraRe.MatchString(body):
		if m := schemaExtraRe.FindStringSubmatch(body); m != nil {
			return fmt.Sprintf("传入了 %s 不接受的参数：%s（请对照该工具的入参说明）", tool, quotedList(m[1]))
		}
	case strings.HasPrefix(body, "enum:"):
		return fmt.Sprintf("参数取值不在允许范围内（工具 %s）：%s", tool, body)
	case strings.Contains(body, "has type"):
		return fmt.Sprintf("参数类型不对（工具 %s）：%s", tool, body)
	}
	return fmt.Sprintf("入参校验失败（工具 %s）：%s", tool, body)
}

// quotedList 把校验器输出的参数名列表转成「uid、name」。
//
// 校验库两种形态都出现过：JSON 风格 ["uid","name"] 与 Go %q 风格 ["uid" "name"]。
// 直接从引号里取值，避免把 [ ] , 之类的标点带进中文报错。
func quotedList(s string) string {
	if m := schemaQuotedRe.FindAllStringSubmatch(s, -1); len(m) > 0 {
		parts := make([]string, 0, len(m))
		for _, g := range m {
			if v := strings.TrimSpace(g[1]); v != "" {
				parts = append(parts, v)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "、")
		}
	}
	// 没有引号（非预期格式）：退化为按括号外空白/逗号切分，至少不把标点原样抛给调用方。
	trimmed := strings.Trim(strings.TrimSpace(s), "[]")
	fields := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	return strings.Join(fields, "、")
}
