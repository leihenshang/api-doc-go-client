// Package syncengine 客户端 SyncEngine（C7）：绑定/拉取/推送/冲突。
// 协议对齐服务端 /api/sync/* 与《客户端与同步架构设计》§4。
package syncengine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Mode 同步方式（opencollection.yml 的 sync.mode）。
const (
	ModeAuto   = "auto"   // 双向
	ModeManual = "manual" // 自动推、手动拉
	ModeMirror = "mirror" // 只拉
)

// BindInfo 集合绑定信息（写在 opencollection.yml 的 sync 段；PAT 不入库）。
type BindInfo struct {
	Linked    bool   `json:"linked"`
	ServerURL string `json:"serverUrl"`
	ProjectID uint64 `json:"projectId"`
	Mode      string `json:"mode"` // auto | manual | mirror
	// Cursor 本地已应用的 change_log 游标
	Cursor int64 `json:"cursor"`
}

// Client 同步 HTTP 客户端。
type Client struct {
	BaseURL string
	Token   string // PAT 或 JWT
	Device  string
	http    *http.Client
}

// New 创建客户端。
func New(baseURL, token, device string) *Client {
	base, err := ValidateServerURL(baseURL)
	if err != nil {
		// 校验失败时保留原值：调用方（App.SetSyncBind / RunSync）已经在入口处做过校验并给出错误，
		// 这里只兜底，避免把非法地址悄悄改写成别的地址。
		base = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	}
	return &Client{
		BaseURL: base,
		Token:   token,
		Device:  device,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// maxSyncBody 单次同步响应的读取上限（防异常服务端用超大响应把内存吃光）。
const maxSyncBody = 32 << 20 // 32MB

// ValidateServerURL 校验并规范化服务端地址。
//
// 规则：必须显式带 http/https；**非回环地址禁止明文 http** —— 同步请求会带
// `Authorization: Bearer <PAT>`，走 http 等于把长期令牌明文放在网络上。
// 本机开发服务（127.0.0.1 / localhost / ::1）允许 http，否则没法联调。
func ValidateServerURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("服务端地址为空")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("服务端地址无法解析: %w", err)
	}
	if u.Host == "" {
		return "", fmt.Errorf("服务端地址缺少主机名（需要形如 https://host:port）: %s", trimmed)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return "", fmt.Errorf("服务端地址 %s 使用明文 http，会把访问令牌明文发到网络上；请改用 https（本机回环地址除外）", trimmed)
		}
	default:
		return "", fmt.Errorf("服务端地址只支持 http/https: %s", trimmed)
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// isLoopbackHost 主机名是否指向本机。
func isLoopbackHost(host string) bool {
	switch strings.ToLower(strings.Trim(host, "[]")) {
	case "", "localhost":
		return true
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// ---- 协议 DTO（与 service.Sync* 对齐）----

// Meta 能力协商。
type Meta struct {
	ServerVersion string   `json:"server_version"`
	SyncProto     int      `json:"sync_proto"`
	Features      []string `json:"features"`
}

// State 游标探测。
type State struct {
	Cursor          int64 `json:"cursor"`
	OldestAvailable int64 `json:"oldest_available"`
}

// Item 同步条目。
type Item struct {
	Type    string          `json:"type"`
	UID     string          `json:"uid"`
	Op      string          `json:"op"`
	Payload json.RawMessage `json:"payload"`
	ItemRev int64           `json:"item_rev"`
	Deleted bool            `json:"deleted"`
}

// Changes 增量响应。
type Changes struct {
	Cursor  int64  `json:"cursor"`
	Items   []Item `json:"items"`
	HasMore bool   `json:"has_more"`
	Gap     bool   `json:"gap"`
}

// Snapshot 全量响应。
type Snapshot struct {
	Cursor  int64  `json:"cursor"`
	Items   []Item `json:"items"`
	HasMore bool   `json:"has_more"`
	Offset  int    `json:"offset"`
}

// Op push 一条。
type Op struct {
	OpID    string          `json:"op_id"`
	Op      string          `json:"op"`
	Type    string          `json:"type"`
	UID     string          `json:"uid"`
	BaseRev int64           `json:"base_rev"`
	Payload json.RawMessage `json:"payload"`
}

// OpResult 逐条结果。
type OpResult struct {
	OpID    string          `json:"op_id"`
	Status  string          `json:"status"`
	ItemRev int64           `json:"item_rev"`
	Reason  string          `json:"reason"`
	Server  json.RawMessage `json:"server"`
}

// PushReq / PushResp。
type PushReq struct {
	ProjectID  uint64 `json:"project_id"`
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	Ops        []Op   `json:"ops"`
}

type PushResp struct {
	Results []OpResult `json:"results"`
	Cursor  int64      `json:"cursor"`
}

// Payload 条目载荷。
type Payload struct {
	Title    string `json:"title,omitempty"`
	PID      string `json:"parent_uid,omitempty"`
	Priority int    `json:"priority,omitempty"`
	Name     string `json:"name,omitempty"`
	Method   string `json:"method,omitempty"`
	URL      string `json:"url,omitempty"`
	GroupUID string `json:"group_uid,omitempty"`
	Extra    string `json:"extra,omitempty"`
	Content  string `json:"content,omitempty"`
	Icon     string `json:"icon,omitempty"`
	Deleted  bool   `json:"deleted,omitempty"`
}

// ---- HTTP ----

func (c *Client) do(method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("同步请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxSyncBody))
	if err != nil {
		return err
	}
	// 统一响应 {code,msg,data}
	var wrap struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return fmt.Errorf("响应解析失败: %s", truncate(string(raw), 200))
	}
	if wrap.Code != 200 {
		return fmt.Errorf("服务端返回 %d: %s", wrap.Code, wrap.Msg)
	}
	if out != nil && len(wrap.Data) > 0 {
		return json.Unmarshal(wrap.Data, out)
	}
	return nil
}

// Meta 能力协商。
func (c *Client) Meta() (*Meta, error) {
	var m Meta
	// meta 免登录，但仍带 token 无害
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+"/api/meta", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxSyncBody))
	var wrap struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data Meta   `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}
	// 与 do() 保持一致：业务码非 200 必须报错，否则会把「服务端拒绝」当成能力协商成功
	if wrap.Code != 200 {
		return nil, fmt.Errorf("服务端返回 %d: %s", wrap.Code, wrap.Msg)
	}
	m = wrap.Data
	return &m, nil
}

// State 游标。
func (c *Client) State(projectID uint64) (*State, error) {
	var s State
	err := c.do(http.MethodGet, fmt.Sprintf("/api/sync/state?project_id=%d", projectID), nil, &s)
	return &s, err
}

// Changes 增量。
func (c *Client) Changes(projectID uint64, since int64, limit int) (*Changes, error) {
	var ch Changes
	err := c.do(http.MethodGet,
		fmt.Sprintf("/api/sync/changes?project_id=%d&since=%d&limit=%d", projectID, since, limit),
		nil, &ch)
	return &ch, err
}

// Snapshot 全量。
func (c *Client) Snapshot(projectID uint64, offset, limit int) (*Snapshot, error) {
	var sn Snapshot
	err := c.do(http.MethodGet,
		fmt.Sprintf("/api/sync/snapshot?project_id=%d&offset=%d&limit=%d", projectID, offset, limit),
		nil, &sn)
	return &sn, err
}

// Push 提交。
func (c *Client) Push(req *PushReq) (*PushResp, error) {
	if req.DeviceID == "" {
		req.DeviceID = c.Device
	}
	var pr PushResp
	err := c.do(http.MethodPost, "/api/sync/push", req, &pr)
	return &pr, err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
