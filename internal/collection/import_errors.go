package collection

import (
	"errors"
	"fmt"
)

// ErrImportInvalid 导入内容/格式不合法（调用方可以修：换文件、改 format、升级客户端）。
//
// 为什么要哨兵：MCP 层需要把「格式不对 / 文件为空 / 版本过高」映射成 [INVALID]，
// 把「写盘失败」映射成 [IO]。以前靠 strings.Contains(err.Error(), "没有") 这类中文子串判断，
// 任何含「没有权限」的 IO 错误都会被误判成参数问题。
//
// 约定：只在**解析与前置校验**处包装本哨兵；writeFileAtomic / os.MkdirAll 等落盘错误保持原样。
var ErrImportInvalid = errors.New("导入内容不合法")

// invalidImport 给导入期「调用方能修」的错误加上哨兵（保留原始错误链）。
func invalidImport(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrImportInvalid) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrImportInvalid, err)
}
