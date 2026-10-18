package app

// 「打开所在目录」的命令构造（P2-10 回归：给 xdg-open 传 `--` 导致每次都被拒绝）。
// 这里只测参数形状，不真的启动文件管理器。

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRevealCmdNoOptionTerminator(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		cmd, err := revealCmd(goos, "/tmp/work/demo")
		if err != nil {
			t.Fatalf("%s: revealCmd 失败: %v", goos, err)
		}
		if len(cmd.Args) != 2 {
			t.Fatalf("%s: 参数个数异常: %v", goos, cmd.Args)
		}
		// 回归点：绝不能出现 `--`（xdg-open 会 "unexpected option '--'" 直接退出 1）
		for _, a := range cmd.Args {
			if a == "--" {
				t.Errorf("%s: 不应传 --：%v", goos, cmd.Args)
			}
		}
		if dir := cmd.Args[len(cmd.Args)-1]; !filepath.IsAbs(dir) {
			t.Errorf("%s: 目录必须是绝对路径（绝对路径不会被当成选项）: %q", goos, dir)
		}
	}
}

func TestRevealCmdProgram(t *testing.T) {
	cases := map[string]string{
		"windows": "explorer.exe",
		"darwin":  "open",
		"linux":   "xdg-open",
	}
	for goos, want := range cases {
		cmd, err := revealCmd(goos, "/tmp/x")
		if err != nil {
			t.Fatalf("%s: %v", goos, err)
		}
		if !strings.HasSuffix(cmd.Path, want) {
			t.Errorf("%s: 期望调用 %s，实际 %s", goos, want, cmd.Path)
		}
	}
}

func TestRevealCmdRejectsControlChars(t *testing.T) {
	if _, err := revealCmd("linux", "/tmp/a\nb"); err == nil {
		t.Error("含换行的路径应被拒绝")
	}
}
