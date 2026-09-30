// Package syncengine 客户端 SyncEngine（C7）：绑定/拉取/推送/冲突。
// 协议对齐服务端 /api/sync/* 与《客户端与同步架构设计》§4。
package syncengine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		Device:  device,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
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
	raw, err := io.ReadAll(resp.Body)
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
	raw, _ := io.ReadAll(resp.Body)
	var wrap struct {
		Code int  `json:"code"`
		Data Meta `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
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
