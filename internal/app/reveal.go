package app

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// RevealInFolder 在系统文件管理器中打开指定工作目录（root 为空 = 活动根）。
//
// 平台分派：Windows 走 explorer.exe、macOS 走 open、Linux 走 xdg-open。
func (a *App) RevealInFolder(root string) error {
	c, err := a.collOf(root)
	if err != nil {
		return err
	}
	dir := strings.TrimSpace(c.Dir)
	if dir == "" {
		return errors.New("集合目录为空")
	}
	cmd, err := revealCmd(runtime.GOOS, dir)
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("打开文件管理器失败: %w", err)
	}
	// Windows 的 explorer.exe 即便成功打开目录也返回退出码 1，不能拿退出码当失败 → 不等待。
	if runtime.GOOS == "windows" {
		go func() { _ = cmd.Wait() }()
		return nil
	}
	// 其余平台：进程**立刻退出且非 0** 就是没打开（典型：xdg-open 参数不被接受、系统没有可用
	// 文件管理器）。等一小会儿把这种失败如实报出来，而不是"点了没反应"。
	// 超过这个时间还在跑就是正常情况（文件管理器已被拉起或移交给常驻进程），直接返回。
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			if msg := firstLine(strings.TrimSpace(stderr.String())); msg != "" {
				return fmt.Errorf("打开文件管理器失败: %v（%s）", err, msg)
			}
			return fmt.Errorf("打开文件管理器失败: %w", err)
		}
		return nil
	case <-time.After(1500 * time.Millisecond):
		return nil
	}
}

// revealCmd 构造"在文件管理器中打开目录"的命令。
//
// 纯函数、不启动进程，便于单测锁住参数形状。
//
// **不要给 xdg-open / open 传 `--`**：绝对路径以 "/"（或盘符）开头，永远不会被当成命令行选项，
// 而 xdg-open 并不认 `--` —— 它会直接 `unexpected option '--'` 退出 1，等于每次都打不开
// （这正是 P2-10 「加 -- 断开选项解析」引入的回归：接口返回成功，界面却毫无反应）。
func revealCmd(goos, dir string) (*exec.Cmd, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("解析目录路径: %w", err)
	}
	abs = filepath.Clean(abs)
	if !filepath.IsAbs(abs) || strings.ContainsAny(abs, "\r\n\x00") {
		return nil, errors.New("目录路径非法")
	}
	switch goos {
	case "windows":
		return exec.Command("explorer.exe", abs), nil
	case "darwin":
		return exec.Command("open", abs), nil
	default:
		return exec.Command("xdg-open", abs), nil
	}
}

// firstLine 取报错的第一行（命令行工具的提示通常只有一行有用，避免弹窗被整段日志撑开）。
func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}
