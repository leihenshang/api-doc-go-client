package app

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// RevealInFolder 在系统文件管理器中打开指定工作目录（root 为空 = 活动根）。
//
// 平台分派：Windows 走 explorer.exe、macOS 走 open、Linux 走 xdg-open。
// 只启动进程、不等待退出——文件管理器本就是常驻程序，Wait 会长时间挂住；
// 另外 Windows 的 explorer.exe 即便成功打开目录也返回退出码 1，不能拿它当失败。
func (a *App) RevealInFolder(root string) error {
	c, err := a.collOf(root)
	if err != nil {
		return err
	}
	dir := strings.TrimSpace(c.Dir)
	if dir == "" {
		return errors.New("集合目录为空")
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = filepath.Clean(abs)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer.exe", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("打开文件管理器失败: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
