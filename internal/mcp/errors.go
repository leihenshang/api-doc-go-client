package mcp

// 工具错误的机器可判别前缀（外部评审 T2）：
// 调用方此前只能匹配中文散文来区分「不存在 / 冲突 / 校验失败」，加上稳定前缀后可以
// 直接按 [CODE] 分流；文本其余部分保持原样，旧调用方不受影响。
//
// 约定：所有面向 MCP 的错误最终由 toolSet.wrap 输出为 `失败: [CODE] 说明`。
// 未显式分类的错误统一 [INTERNAL]，不要把内部实现细节（sqlite/文件路径）当成契约。

import "errors"

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
