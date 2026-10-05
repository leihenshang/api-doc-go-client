package syncengine

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"api-doc-go-client/internal/collection"
)

// Engine 一轮同步的编排：先 pull 后 push。
type Engine struct {
	Coll *collection.Collection
	Bind BindInfo
	CLI  *Client
}

// DeviceID 生成/读取设备标识（存配置目录）。
func DeviceID() string {
	// 简单实现：随机一次 + 本机持久化交给调用方；此处生成随机即可
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "dev-" + hex.EncodeToString(b)
}

// Report 一轮同步的结果摘要。
type Report struct {
	Pulled    int      `json:"pulled"`
	Pushed    int      `json:"pushed"`
	Conflicts int      `json:"conflicts"`
	Rejected  int      `json:"rejected"`
	Errors    []string `json:"errors,omitempty"`
	Cursor    int64    `json:"cursor"`
	Gap       bool     `json:"gap"`
}

// RunOne 执行一轮：pull → apply → push dirty。
func (e *Engine) RunOne() (*Report, error) {
	rep := &Report{}
	// 1. 探测游标
	st, err := e.CLI.State(e.Bind.ProjectID)
	if err != nil {
		return rep, err
	}
	// 2. 增量 / 或 gap 时全量
	if e.Bind.Cursor > 0 && st.Cursor > e.Bind.Cursor {
		ch, err := e.CLI.Changes(e.Bind.ProjectID, e.Bind.Cursor, 500)
		if err != nil {
			return rep, err
		}
		if ch.Gap {
			rep.Gap = true
			if err := e.pullSnapshot(rep); err != nil {
				return rep, err
			}
		} else {
			if err := e.applyItems(ch.Items, rep); err != nil {
				return rep, err
			}
			e.Bind.Cursor = ch.Cursor
			// has_more 时继续拉（带上限：服务端恒返回 has_more 或游标不前进时不能无限循环）
			for pages := 1; ch.HasMore; pages++ {
				if pages >= maxPullPages {
					rep.Errors = append(rep.Errors, fmt.Sprintf("增量拉取超过 %d 页，已停止（服务端可能未推进游标）", maxPullPages))
					return rep, nil
				}
				next, cerr := e.CLI.Changes(e.Bind.ProjectID, e.Bind.Cursor, 500)
				if cerr != nil {
					break
				}
				_ = e.applyItems(next.Items, rep)
				if next.Cursor <= e.Bind.Cursor {
					// 游标没有推进：继续循环只会重复拉同一批数据
					rep.Errors = append(rep.Errors, "服务端游标未推进，已停止增量拉取")
					break
				}
				ch, e.Bind.Cursor = next, next.Cursor
			}
		}
	} else if e.Bind.Cursor == 0 {
		if err := e.pullSnapshot(rep); err != nil {
			return rep, err
		}
	}

	// 3. push（mirror 只拉不推）
	if e.Bind.Mode != ModeMirror {
		if err := e.pushDirty(rep); err != nil {
			return rep, err
		}
	}
	rep.Cursor = e.Bind.Cursor
	return rep, nil
}

// maxPullPages 单轮同步最多拉取的页数（增量与全量共用）。
const maxPullPages = 200

func (e *Engine) pullSnapshot(rep *Report) error {
	offset := 0
	for pages := 0; ; pages++ {
		if pages >= maxPullPages {
			rep.Errors = append(rep.Errors, fmt.Sprintf("全量拉取超过 %d 页，已停止（服务端可能未推进 offset）", maxPullPages))
			return nil
		}
		sn, err := e.CLI.Snapshot(e.Bind.ProjectID, offset, 500)
		if err != nil {
			return err
		}
		if err := e.applyItems(sn.Items, rep); err != nil {
			return err
		}
		e.Bind.Cursor = sn.Cursor
		if !sn.HasMore {
			break
		}
		offset += 500
	}
	return nil
}

// applyItems 两阶段：先分组，再条目（R7）。
func (e *Engine) applyItems(items []Item, rep *Report) error {
	// 按 type 排序：group → api / doc
	sorted := make([]Item, len(items))
	copy(sorted, items)
	sort.SliceStable(sorted, func(i, j int) bool {
		return typeRank(sorted[i].Type) < typeRank(sorted[j].Type)
	})
	for _, it := range sorted {
		if it.UID == "" {
			continue
		}
		if it.Deleted || it.Op == "delete" {
			e.applyDelete(it, rep)
			continue
		}
		if err := e.applyUpsert(it, rep); err != nil {
			rep.Errors = append(rep.Errors, err.Error())
		} else {
			rep.Pulled++
		}
	}
	return nil
}

func typeRank(t string) int {
	switch t {
	case "group":
		return 0
	case "api":
		return 1
	case "doc":
		return 2
	default:
		return 9
	}
}

func (e *Engine) applyUpsert(it Item, rep *Report) error {
	var p Payload
	_ = json.Unmarshal(it.Payload, &p)

	// 拉取前若本地已 dirty，先把本地版本存进 .conflicts/（R8 / §4.4）
	if it.Type == "api" {
		e.saveLocalIfDirty(it.UID, it.ItemRev)
	}

	var err error
	switch it.Type {
	case "group":
		err = e.applyGroup(it.UID, p)
	case "api":
		err = e.applyAPI(it.UID, p)
	case "doc":
		err = e.applyDoc(it.UID, p)
	}
	if err != nil {
		return err
	}
	// 拉取应用后立即固化：用文件最新 hash（索引里还是旧值）
	if req, err := e.Coll.ReadRequest(it.UID); err == nil {
		if h := e.Coll.FileHashOf(req.Path); h != "" {
			_ = e.Coll.MarkSynced(it.UID, h, it.ItemRev)
		}
	} else {
		// 分组等无请求文件的条目：用哨兵值标记已同步
		_ = e.Coll.MarkSynced(it.UID, "synced", it.ItemRev)
	}
	return nil
}

// saveLocalIfDirty 本地有未推送改动时，先落冲突副本再让远端覆盖（§4.4 LWW + 副本）。
func (e *Engine) saveLocalIfDirty(uid string, serverRev int64) {
	nodes, err := e.Coll.DirtyNodes()
	if err != nil {
		return
	}
	for _, n := range nodes {
		if n.UID != uid {
			continue
		}
		if req, err := e.Coll.ReadRequest(uid); err == nil {
			_, _ = e.Coll.SaveConflictCopy(req, serverRev, nil)
		}
		return
	}
}

func (e *Engine) applyGroup(uid string, p Payload) error {
	// 找已有分组
	dir := e.findDirByUID(uid)
	parent := ""
	if p.PID != "" {
		parent = e.findDirByUID(p.PID)
	}
	if dir == "" {
		// 新建：在父下建目录
		name := p.Title
		if name == "" {
			name = "未命名目录"
		}
		return e.Coll.CreateFolder(parent, name)
	}
	// 父级变了 → 移动分组
	curParent := filepath.ToSlash(filepath.Dir(dir))
	if curParent == "." {
		curParent = ""
	}
	if parent != curParent {
		if err := e.Coll.MoveFolder(uid, parent); err != nil {
			return err
		}
	}
	// 显示名更新
	if p.Title != "" {
		return e.Coll.RenameFolder(uid, p.Title)
	}
	return nil
}

func (e *Engine) applyAPI(uid string, p Payload) error {
	r, err := e.Coll.ReadRequest(uid)
	if err != nil {
		// 新建：用服务端 uid 落盘
		folder := ""
		if p.GroupUID != "" {
			folder = e.findDirByUID(p.GroupUID)
		}
		name := p.Name
		if name == "" {
			name = "未命名请求"
		}
		method := p.Method
		if method == "" {
			method = "GET"
		}
		created, err := e.Coll.CreateRequestWithUID(folder, name, method, uid)
		if err != nil {
			return err
		}
		created.URL = p.URL
		applyExtra(created, p.Extra)
		return e.Coll.SaveRequest(created)
	}
	// 更新：改名 / 移动 / 内容
	if p.Name != "" && p.Name != r.Name {
		if err := e.Coll.RenameRequest(uid, p.Name); err == nil {
			// RenameRequest 只改显示名，Path 不变；再读回最新
			if nr, err := e.Coll.ReadRequest(uid); err == nil {
				r = nr
			}
		}
	}
	if p.GroupUID != "" {
		folder := e.findDirByUID(p.GroupUID)
		if folder != "" {
			// 目标分组与当前不同时移动
			if filepath.ToSlash(filepath.Dir(r.Path)) != folder {
				_ = e.Coll.MoveRequest(uid, folder)
				if nr, err := e.Coll.ReadRequest(uid); err == nil {
					r = nr
				}
			}
		}
	}
	if p.Method != "" {
		r.Method = strings.ToUpper(p.Method)
	}
	if p.URL != "" {
		r.URL = p.URL
	}
	applyExtra(r, p.Extra)
	return e.Coll.SaveRequest(r)
}

// applyExtra 从服务端 extra 还原客户端字段（v1：desc → docs）。
func applyExtra(r *collection.Request, extra string) {
	if extra == "" {
		return
	}
	var m map[string]any
	if json.Unmarshal([]byte(extra), &m) != nil {
		return
	}
	if v, ok := m["desc"].(string); ok {
		r.Docs = v
	}
}

// applyDoc 文档条目（B13）：docs/*.md 落盘。
func (e *Engine) applyDoc(uid string, p Payload) error {
	d, err := e.Coll.ReadDoc(uid)
	if err != nil {
		// 新建
		name := p.Name
		if name == "" {
			name = "未命名文档"
		}
		created, err := e.Coll.CreateDoc(name, uid)
		if err != nil {
			return err
		}
		created.Content = p.Content
		created.Icon = p.Icon
		return e.Coll.SaveDoc(created)
	}
	if p.Name != "" && p.Name != d.Name {
		d.Name = p.Name
	}
	d.Content = p.Content
	d.Icon = p.Icon
	return e.Coll.SaveDoc(d)
}

func (e *Engine) applyDelete(it Item, rep *Report) {
	switch it.Type {
	case "api":
		_ = e.Coll.DeleteRequest(it.UID)
	case "group":
		// 组删除只移除 folder.yml 语义，保留子文件（§4.5）
		if dir := e.findDirByUID(it.UID); dir != "" {
			_ = os.Remove(filepath.Join(e.Coll.Dir, filepath.FromSlash(dir), "folder.yml"))
		}
	}
}

// findDirByUID 扫描找分组目录相对路径。
func (e *Engine) findDirByUID(uid string) string {
	info, err := e.Coll.Info()
	if err != nil {
		return ""
	}
	var walk func(nodes []*collection.Node) string
	walk = func(nodes []*collection.Node) string {
		for _, n := range nodes {
			if n.Type == "folder" && n.UID == uid {
				return n.Path
			}
			if n.Children != nil {
				if p := walk(n.Children); p != "" {
					return p
				}
			}
		}
		return ""
	}
	return walk(info.Tree)
}

// pushDirty 只推送 hash 变化的节点（精确 dirty 集）+ 待推送删除（tombstone）。
// 拓扑序：分组先于请求；成功后 MarkSynced 固化 hash。
func (e *Engine) pushDirty(rep *Report) error {
	dirty, err := e.Coll.DirtyNodes()
	if err != nil {
		return err
	}
	pendingDel := e.Coll.LoadPendingDeletes()
	if len(dirty) == 0 && len(pendingDel) == 0 {
		return nil
	}
	// 拓扑序：folder 先，request 后
	sort.SliceStable(dirty, func(i, j int) bool {
		return typeRank(dirty[i].Type) < typeRank(dirty[j].Type)
	})

	ops := make([]Op, 0, len(dirty)+len(pendingDel))
	meta := make(map[string]dirtyMeta, len(dirty)+len(pendingDel))
	for _, n := range dirty {
		if n.UID == "" {
			continue
		}
		var (
			op      Op
			payload Payload
		)
		switch n.Type {
		case "folder":
			payload = Payload{Title: n.Title}
			op = Op{OpID: newOpID(), Op: "upsert", Type: "group", UID: n.UID}
		case "request":
			req, err := e.Coll.ReadRequest(n.UID)
			if err != nil {
				continue
			}
			payload = Payload{Name: req.Name, Method: req.Method, URL: req.URL}
			if req.Docs != "" {
				extra := map[string]any{"desc": req.Docs}
				eb, _ := json.Marshal(extra)
				payload.Extra = string(eb)
			}
			op = Op{OpID: newOpID(), Op: "upsert", Type: "api", UID: n.UID, BaseRev: n.BaseRev}
		default:
			continue
		}
		b, _ := json.Marshal(payload)
		op.Payload = b
		ops = append(ops, op)
		meta[op.OpID] = dirtyMeta{uid: n.UID, hash: n.Hash, typ: n.Type}
	}
	// tombstone：本地已删除的请求/文档
	for _, uid := range pendingDel {
		oid := newOpID()
		ops = append(ops, Op{OpID: oid, Op: "delete", Type: "api", UID: uid})
		meta[oid] = dirtyMeta{uid: uid, typ: "api", isDelete: true}
	}
	// 文档条目（B13）
	docs, _ := e.Coll.ListDocs()
	for _, d := range docs {
		if d.UID == "" {
			continue
		}
		// 只推有变化的（用内容 hash）
		oid := newOpID()
		payload := Payload{Name: d.Name, Content: d.Content, Icon: d.Icon}
		b, _ := json.Marshal(payload)
		ops = append(ops, Op{OpID: oid, Op: "upsert", Type: "doc", UID: d.UID, Payload: b})
		meta[oid] = dirtyMeta{uid: d.UID, hash: e.Coll.FileHashOf(d.Path), typ: "doc"}
	}
	if len(ops) == 0 {
		return nil
	}
	resp, err := e.CLI.Push(&PushReq{
		ProjectID:  e.Bind.ProjectID,
		DeviceID:   e.CLI.Device,
		DeviceName: e.CLI.Device,
		Ops:        ops,
	})
	if err != nil {
		return err
	}
	allOk := true
	for _, r := range resp.Results {
		m := meta[r.OpID]
		switch r.Status {
		case "ok":
			rep.Pushed++
			if m.uid != "" && !m.isDelete {
				h := m.hash
				if h == "" {
					h = "synced" // 分组等无文件 hash 的条目用哨兵值
				}
				_ = e.Coll.MarkSynced(m.uid, h, r.ItemRev)
			}
		case "conflict":
			rep.Conflicts++
			e.writeConflictCopy(m.uid, r)
			// 冲突不 MarkSynced，下次仍 dirty
			if m.isDelete {
				allOk = false
			}
		default:
			rep.Rejected++
			if r.Reason != "" {
				rep.Errors = append(rep.Errors, fmt.Sprintf("%s: %s", m.uid, r.Reason))
			}
			if m.isDelete {
				allOk = false
			}
		}
	}
	// 删除全部推送成功后才清 pendingDeletes
	if allOk && len(pendingDel) > 0 {
		e.Coll.ClearPendingDeletes()
	}
	if resp.Cursor > e.Bind.Cursor {
		e.Bind.Cursor = resp.Cursor
	}
	return nil
}

// dirtyMeta 一条 dirty 节点的推送元信息。
type dirtyMeta struct {
	uid      string
	hash     string
	typ      string
	isDelete bool
}

// writeConflictCopy 冲突：本地版本进 .conflicts/（R8，不带可同步 uid）。
func (e *Engine) writeConflictCopy(uid string, r OpResult) {
	if uid == "" {
		return
	}
	req, err := e.Coll.ReadRequest(uid)
	if err != nil {
		return
	}
	_, _ = e.Coll.SaveConflictCopy(req, r.ItemRev, r.Server)
}

func newOpID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// LoadBind 从 .sync.json 读绑定（PAT 不入库）。
func LoadBind(dir string) (BindInfo, error) {
	var b BindInfo
	data, err := os.ReadFile(filepath.Join(dir, ".sync.json"))
	if err != nil {
		return b, err
	}
	err = json.Unmarshal(data, &b)
	return b, err
}

// SaveBind 写绑定。
func SaveBind(dir string, b BindInfo) error {
	data, _ := json.MarshalIndent(b, "", "  ")
	return os.WriteFile(filepath.Join(dir, ".sync.json"), data, 0o600)
}

// IsLinked 集合是否已关联服务端。
func IsLinked(dir string) bool {
	b, err := LoadBind(dir)
	return err == nil && b.Linked && b.ServerURL != "" && b.ProjectID > 0
}
