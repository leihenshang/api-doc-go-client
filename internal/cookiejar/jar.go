// Package cookiejar 简易持久化 Cookie 罐：实现 http.CookieJar，落 <配置目录>/cookies.json。
// 只做主机/路径/过期匹配，不实现 Public Suffix 等完整语义（客户端场景够用）。
package cookiejar

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"api-doc-go-client/internal/config"
)

const fileName = "cookies.json"

type stored struct {
	Host   string      `json:"host"`
	Cookie http.Cookie `json:"cookie"`
}

// Jar 并发安全的 Cookie 罐；persist 为 true 时变更即落盘。
type Jar struct {
	mu      sync.Mutex
	path    string
	persist bool
	items   []stored
}

// Info 暴露给 UI 的 Cookie 视图。
type Info struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Domain  string `json:"domain"`
	Path    string `json:"path"`
	Expires int64  `json:"expires"` // Unix 毫秒；0 表示会话 Cookie
}

// New 创建 Cookie 罐；persist 为 true 时从磁盘恢复。
func New(persist bool) (*Jar, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	j := &Jar{path: filepath.Join(dir, fileName), persist: persist}
	if persist {
		j.load()
	}
	return j, nil
}

// SetCookies 记录响应下发的 Cookie（Max-Age<0 表示删除）。
func (j *Jar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if u == nil || len(cookies) == 0 {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, c := range cookies {
		j.set(u, c)
	}
	j.dropExpired()
	j.flush()
}

func (j *Jar) set(u *url.URL, c *http.Cookie) {
	host := u.Hostname()
	if c.Domain != "" {
		host = strings.TrimPrefix(strings.ToLower(c.Domain), ".")
	}
	j.remove(host, c.Name, c.Path)
	if c.MaxAge < 0 {
		return
	}
	j.items = append(j.items, stored{Host: host, Cookie: *c})
}

// Cookies 返回可用于该 URL 的 Cookie。
func (j *Jar) Cookies(u *url.URL) []*http.Cookie {
	if u == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]*http.Cookie, 0, len(j.items))
	for _, s := range j.items {
		if j.match(u, s) {
			c := s.Cookie
			out = append(out, &c)
		}
	}
	return out
}

func (j *Jar) match(u *url.URL, s stored) bool {
	host := strings.ToLower(u.Hostname())
	if host != s.Host && !strings.HasSuffix(host, "."+s.Host) {
		return false
	}
	p := s.Cookie.Path
	if p == "" {
		p = "/"
	}
	if !strings.HasPrefix(u.Path, p) {
		return false
	}
	return !expired(s.Cookie)
}

func expired(c http.Cookie) bool {
	return !c.Expires.IsZero() && c.Expires.Before(time.Now())
}

func (j *Jar) remove(host, name, path string) {
	kept := j.items[:0]
	for _, s := range j.items {
		if s.Host == host && s.Cookie.Name == name && s.Cookie.Path == path {
			continue
		}
		kept = append(kept, s)
	}
	j.items = kept
}

func (j *Jar) dropExpired() {
	kept := j.items[:0]
	for _, s := range j.items {
		if !expired(s.Cookie) {
			kept = append(kept, s)
		}
	}
	j.items = kept
}

// List 列出全部 Cookie（新值优先，供 UI 展示）。
func (j *Jar) List() []Info {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.dropExpired()
	out := make([]Info, 0, len(j.items))
	for _, s := range j.items {
		var exp int64
		if !s.Cookie.Expires.IsZero() {
			exp = s.Cookie.Expires.UnixMilli()
		}
		out = append(out, Info{
			Name: s.Cookie.Name, Value: s.Cookie.Value, Domain: s.Host,
			Path: s.Cookie.Path, Expires: exp,
		})
	}
	return out
}

// Clear 清空内存与磁盘。
func (j *Jar) Clear() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.items = nil
	j.flush()
	return nil
}

func (j *Jar) load() {
	data, err := os.ReadFile(j.path)
	if err != nil {
		return
	}
	var items []stored
	if err := json.Unmarshal(data, &items); err != nil {
		return
	}
	j.items = items
	j.dropExpired()
}

func (j *Jar) flush() {
	if !j.persist {
		return
	}
	data, err := json.MarshalIndent(j.items, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(j.path), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(j.path, data, 0o600); err != nil {
		return
	}
}
