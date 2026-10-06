// Package history 本地请求历史：JSONL 落 <配置目录>/history.jsonl，超出上限自动截断。
package history

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"api-doc-go-client/internal/config"
)

const fileName = "history.jsonl"

// Entry 一次发送记录（失败时 Error 非空，Status 为 0）。
// Request 为发送时的请求快照，供「原样重放」（E22）。
type Entry struct {
	Time   int64  `json:"time"` // Unix 毫秒
	UID    string `json:"uid"`
	Name   string `json:"name"`
	Method string `json:"method"`
	URL    string `json:"url"`
	Status int    `json:"status"`
	TimeMS int64  `json:"timeMs"`
	Size   int    `json:"size"`
	Error  string `json:"error,omitempty"`
	// Root 该记录所属工作目录的标识（规范化绝对路径，与 App 侧的多根表同一套标识）。
	// 为什么不用集合 uid：两个工作目录完全可能是同一份集合的拷贝（uid 相同，路径不同），
	// 只有路径能区分它们。
	// 历史是升级前就有的单文件，旧条目没有这个字段（omitempty）→ 视为「无归属」，
	// 在任一目录下都可见（否则升级后旧历史会像凭空消失）。
	Root string `json:"root,omitempty"`
	// Request 发送时的草稿快照（JSON 的 collection.Request）；旧记录可为空
	Request json.RawMessage `json:"request,omitempty"`
}

func path() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// Append 追加一条记录，并把总量裁剪到 limit 以内。
func Append(e Entry, limit int) error {
	if limit <= 0 {
		return nil
	}
	items, err := readAll()
	if err != nil {
		return err
	}
	items = append(items, e)
	if len(items) > limit {
		items = items[len(items)-limit:]
	}
	return writeAll(items)
}

// belongs 该条目是否属于指定工作目录：条目自带的标识命中，或它是升级前的旧条目（无归属）。
func belongs(e Entry, root string) bool {
	if root == "" {
		return true
	}
	return e.Root == "" || e.Root == root
}

// List 返回最近的记录（新 → 旧），limit<=0 时返回全部。
// root 非空时只返回该工作目录的记录（含升级前无归属的旧记录）。
func List(limit int, root string) ([]Entry, error) {
	items, err := readAll()
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(items))
	for i := len(items) - 1; i >= 0; i-- {
		if !belongs(items[i], root) {
			continue
		}
		out = append(out, items[i])
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Clear 清空历史：root 非空时只清该工作目录的记录（含无归属的旧记录），其余保留。
func Clear(root string) error {
	if root == "" {
		return writeAll(nil)
	}
	items, err := readAll()
	if err != nil {
		return err
	}
	keep := make([]Entry, 0, len(items))
	for _, e := range items {
		if !belongs(e, root) {
			keep = append(keep, e)
		}
	}
	return writeAll(keep)
}

func readAll() ([]Entry, error) {
	p, err := path()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取历史: %w", err)
	}
	defer f.Close()

	var items []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue // 单行损坏不影响整体
		}
		items = append(items, e)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("扫描历史: %w", err)
	}
	return items, nil
}

func writeAll(items []Entry) error {
	p, err := path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("创建配置目录: %w", err)
	}
	// 0600：历史里存的是请求快照（含 Authorization / Basic / body 里的令牌），
	// 与 config.json / cookies.json / *.secrets.yml 保持同一权限，别让同机其他用户读到。
	f, err := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("写入历史: %w", err)
	}
	defer f.Close()
	_ = f.Chmod(0o600) // 老版本建的文件可能是 0644：这里顺手收权

	w := bufio.NewWriter(f)
	for _, e := range items {
		data, err := json.Marshal(e)
		if err != nil {
			continue
		}
		if _, err := w.Write(append(data, '\n')); err != nil {
			return fmt.Errorf("写入历史: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("刷新历史: %w", err)
	}
	return nil
}
