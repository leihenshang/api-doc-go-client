package collection

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IsBrunoDir 判断目录是否为原生 Bruno 集合（含 bruno.json 或 .bru 文件）。
// 这类目录不能直接打开，须走导入流程转换为本客户端格式。
func IsBrunoDir(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "bruno.json")); err == nil {
		return true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".bru") {
			return true
		}
		if e.IsDir() && e.Name() != "node_modules" && e.Name() != ".git" {
			if IsBrunoDir(filepath.Join(dir, e.Name())) {
				return true
			}
		}
	}
	return false
}

// ImportBruno 把原生 Bruno 集合导入到当前集合（按本客户端格式解析）。
// bruno.json → 不读；.bru 文件 → 请求；目录 → 分组。
func (c *Collection) ImportBruno(srcDir string) (*ImportSummary, error) {
	if !IsBrunoDir(srcDir) {
		return nil, fmt.Errorf("不是 Bruno 集合目录")
	}
	sum := &ImportSummary{}
	keys := c.existingKeys()
	err := filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(srcDir, path)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			// 建对应分组
			if rel != "." {
				if _, err := c.ensureImportFolder("", rel); err != nil {
					sum.addFailure(fmt.Sprintf("建分组 %s: %v", rel, err))
				}
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".bru") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			sum.addFailure(fmt.Sprintf("读 %s: %v", rel, err))
			return nil
		}
		req, name, ok := parseBru(data)
		if !ok {
			sum.addFailure(fmt.Sprintf("解析 %s 失败", rel))
			return nil
		}
		// 目标分组 = .bru 所在目录
		parent := filepath.ToSlash(filepath.Dir(rel))
		if parent == "." {
			parent = ""
		}
		c.saveImported(parent, name, req, keys, sum)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return sum, nil
}

// parseBru 解析 .bru 文件（YAML 风格，节以 meta:/http:/params: 等开头）。
func parseBru(data []byte) (*Request, string, bool) {
	lines := strings.Split(string(data), "\n")
	var (
		name    string
		method  string
		url     string
		docs    string
		section string
		params  []KV
		headers []KV
		inDocs  bool
	)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// 节标题
		if strings.HasSuffix(trimmed, ":") && !strings.HasPrefix(trimmed, " ") && !strings.HasPrefix(trimmed, "\t") {
			section = strings.TrimSuffix(trimmed, ":")
			inDocs = section == "docs" || section == "script:docs"
			continue
		}
		// 空行结束 docs
		if inDocs && trimmed == "" {
			inDocs = false
		}
		if inDocs {
			docs += line + "\n"
			continue
		}
		// key: value
		if idx := strings.Index(trimmed, ": "); idx > 0 {
			key := trimmed[:idx]
			val := strings.Trim(trimmed[idx+2:], `"'`)
			switch section {
			case "meta":
				if key == "name" {
					name = val
				}
			case "http":
				if key == "method" {
					method = val
				}
				if key == "url" {
					url = val
				}
			case "params:query", "params":
				if key == "name" || key != "" {
					// Bruno params 是列表格式，简化处理
				}
			case "headers":
				// 同上
			}
		}
	}
	if name == "" || url == "" {
		return nil, "", false
	}
	r := &Request{
		Method:  strings.ToUpper(method),
		URL:     url,
		Params:  params,
		Headers: headers,
		Docs:    strings.TrimSpace(docs),
		Body:    Body{Type: "none"},
	}
	return r, name, true
}
