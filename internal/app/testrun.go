package app

// 集合运行器（RM-02）的桌面端门面：复用 internal/testrun，与 CLI/CI 同一条执行链路。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"api-doc-go-client/internal/testrun"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// RunCollectionOptions 桌面端运行集合的参数（JSON tag 与前端字段对应）。
type RunCollectionOptions struct {
	EnvName    string `json:"env_name"`
	Filter     string `json:"filter"`
	Bail       bool   `json:"bail"`
	TimeoutSec int    `json:"timeout_sec"`
}

// RunCollection 按集合树顺序运行当前集合，结果保存在 lastRun 供导出。
func (a *App) RunCollection(opts RunCollectionOptions) (*testrun.Report, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	a.ensureJar()
	var timeout time.Duration
	if opts.TimeoutSec > 0 {
		timeout = time.Duration(opts.TimeoutSec) * time.Second
	}
	rep, err := testrun.Run(context.Background(), c, testrun.Options{
		EnvName: opts.EnvName,
		Filter:  opts.Filter,
		Bail:    opts.Bail,
		Timeout: timeout,
		Runner:  a.sendOptions(),
	})
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.lastRun = rep
	a.mu.Unlock()
	return rep, nil
}

// ExportRunReport 导出最近一次运行报告：junit | json | text。
// 与 ExportDoc 一样支持桌面保存对话框与 devserver/headless 目录。
func (a *App) ExportRunReport(format, defaultName string) (string, error) {
	a.mu.Lock()
	rep := a.lastRun
	a.mu.Unlock()
	if rep == nil {
		return "", errors.New("还没有运行结果，请先运行集合")
	}

	var data []byte
	var ext string
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "junit", "xml":
		var err error
		data, err = rep.JUnit()
		ext = ".xml"
		if err != nil {
			return "", err
		}
	case "json":
		var err error
		data, err = rep.JSON()
		ext = ".json"
		if err != nil {
			return "", err
		}
	case "text", "txt":
		data = []byte(rep.Text())
		ext = ".txt"
	default:
		return "", fmt.Errorf("不支持的运行报告格式：%s（junit / json / text）", format)
	}

	name := strings.TrimSpace(defaultName)
	if name == "" {
		name = sanitizeFileName(rep.Collection) + ext
	}
	var path string
	if a.headlessDir != "" {
		path = filepath.Join(a.headlessDir, filepath.Base(name))
	} else if ctx := a.appCtx(); ctx != nil {
		picked, err := runtime.SaveFileDialog(ctx, runtime.SaveDialogOptions{
			Title:           "导出运行报告",
			DefaultFilename: name,
		})
		if err != nil {
			return "", err
		}
		if picked == "" {
			return "", nil
		}
		path = picked
	} else {
		return "", errors.New("应用未就绪")
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("写入运行报告: %w", err)
	}
	return path, nil
}

// sanitizeFileName 把集合名转为**运行报告导出**的安全文件名（空则回退 report）。
//
// 注意作用域：它只用于「导出 HTML/JSON 报告」这类一次性文件名，不参与集合落盘。
// 集合内的条目（请求 / 分组 / 文档 / 响应示例）一律走 collection.SanitizeFileName，
// 免得两套规则漂移出「名字与文件名不同源」。
func sanitizeFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "report"
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "-", "?", "-", "\"", "-", "<", "-", ">", "-", "|", "-")
	return replacer.Replace(name)
}
