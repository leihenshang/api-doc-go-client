package syncengine

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/config"
	"api-doc-go-client/internal/index"
	"api-doc-go-client/internal/keychain"
	"api-doc-go-client/internal/patvault"
	"api-doc-go-client/internal/secrets"
)

// Engine 一轮同步的编排：先 pull 后 push。
type Engine struct {
	Coll *collection.Collection
	Bind BindInfo
	CLI  *Client
	// Cipher secret 变量值的口令加解密器；nil/空口令时 secret 不出本机（不同步该条）。
	Cipher *secrets.Cipher
	// snapped 本轮已留底的冲突副本（kind+路径）：同一环境文件被多个变量的更新
	// 连续覆盖时只留一份，避免刷出一堆内容相同的副本。
	snapped map[string]bool
}

// cipher 返回生效的加解密器：显式注入优先，否则读本机持久化口令（App 每次同步新建 Engine）。
func (e *Engine) cipher() *secrets.Cipher {
	if e.Cipher != nil {
		return e.Cipher
	}
	return secrets.NewCipher(SyncPassphrase())
}

// SyncPassphrase 读取本机同步口令（未设置返回空串）。口令用于 secret 变量值的加解密。
// G6：优先 OS 密钥环；未迁入时回退配置目录 0600 文件，且文件有值会**择机迁入密钥环**
// （确认写入成功才删文件）。换机需输入同一口令才能解开既有密文。
func SyncPassphrase() string {
	if v, err := keychain.Get(passphraseKeyringUser); err == nil {
		return v
	}
	p := syncPassphrasePath()
	if p == "" {
		return ""
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	pass := string(data)
	if pass != "" && keychain.Set(passphraseKeyringUser, pass) == nil {
		_ = os.Remove(p) // 迁移成功：磁盘不再保留口令文件
	}
	return pass
}

// passphraseKeyringUser 是密钥环中同步口令的条目名。
const passphraseKeyringUser = "sync_passphrase"

// SetSyncPassphrase 设置口令（传空串 = 清除）。优先写密钥环并删除文件；
// 密钥环不可用（ErrNotAvailable 等）时保持历史行为：配置目录 0600 文件。
func SetSyncPassphrase(pass string) error {
	if pass == "" {
		if err := keychain.Delete(passphraseKeyringUser); err != nil && !errors.Is(err, keychain.ErrNotFound) {
			return err
		}
		p := syncPassphrasePath()
		if p == "" {
			return nil
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := keychain.Set(passphraseKeyringUser, pass); err == nil {
		if p := syncPassphrasePath(); p != "" {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return nil
	}
	p := syncPassphrasePath()
	if p == "" {
		return fmt.Errorf("无法定位配置目录")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(pass), 0o600) // 刻意不加尾换行：口令原样存取
}

func syncPassphrasePath() string {
	dir, err := config.Dir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "sync_passphrase")
}

// envHashOf / varHashOf：环境与变量条目的内容摘要（sha-256 hex），做推送脏检测用。
// secret 值以本地明文参与摘要（摘要不泄露明文），改名/改值/启停/敏感标记都会触发重推。
func envHashOf(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])
}

func varHashOf(v collection.Var) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%v\x00%v\x00%s", v.Name, v.Enabled, v.Secret, v.Value)))
	return hex.EncodeToString(sum[:])
}

// DeviceID 读取本机设备标识：首次生成后持久化到配置目录（device_id 文件），跨重启保持不变。
// 设备标识随同步请求上报（change_log.device），用于服务端区分写入来源，必须是稳定值。
func DeviceID() string {
	if id := readDeviceID(); id != "" {
		return id
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	id := "dev-" + hex.EncodeToString(b)
	_ = writeDeviceID(id) // 落盘失败不阻断：退回每次随机，仍可用
	return id
}

// deviceIDPath 设备标识文件路径（与 config.json 同目录）。
func deviceIDPath() string {
	dir, err := config.Dir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "device_id")
}

func readDeviceID() string {
	p := deviceIDPath()
	if p == "" {
		return ""
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func writeDeviceID(id string) error {
	p := deviceIDPath()
	if p == "" {
		return fmt.Errorf("无法定位配置目录")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(id+"\n"), 0o600)
}

// Report 一轮同步的结果摘要。
type Report struct {
	Pulled    int `json:"pulled"`
	Pushed    int `json:"pushed"`
	Conflicts int `json:"conflicts"`
	Rejected  int `json:"rejected"`
	// Assets 文档图片离线映射（E19）本轮新镜像的附件数（已存在的旧附件不计）。
	Assets int      `json:"assets,omitempty"`
	Errors []string `json:"errors,omitempty"`
	Cursor int64    `json:"cursor"`
	Gap    bool     `json:"gap"`
}

// RunOne 执行一轮：pull → apply → push dirty。
func (e *Engine) RunOne() (*Report, error) {
	rep := &Report{}
	// 0. 能力协商（S9）：先确认服务端支持同步协议与所需能力，避免带过期协议白跑。
	if err := e.negotiate(); err != nil {
		return rep, err
	}
	// 1~2. 探测游标并按增量/全量拉取（stop=true 表示超页数上限，提前结束本轮、不 push）。
	if stop, err := e.pull(rep); err != nil {
		return rep, err
	} else if stop {
		return rep, nil
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

// pull 探测服务端游标并按需拉取：本地游标落后则增量（gap 降级全量），首次（cursor=0）全量。
// 返回 stop=true 表示增量翻页超上限，RunOne 应提前结束本轮（不再 push）。
func (e *Engine) pull(rep *Report) (bool, error) {
	st, err := e.CLI.State(e.Bind.ProjectID)
	if err != nil {
		return false, err
	}
	if e.Bind.Cursor > 0 && st.Cursor > e.Bind.Cursor {
		return e.pullIncremental(rep)
	}
	if e.Bind.Cursor == 0 {
		if err := e.pullSnapshot(rep); err != nil {
			return false, err
		}
	}
	return false, nil
}

// pullIncremental 按游标增量拉取；服务端返回 gap 时降级为全量快照。
// 翻页带上限（maxPullPages）：服务端恒返回 has_more 或游标不前进时不能无限循环。
func (e *Engine) pullIncremental(rep *Report) (bool, error) {
	ch, err := e.CLI.Changes(e.Bind.ProjectID, e.Bind.Cursor, 500)
	if err != nil {
		return false, err
	}
	if ch.Gap {
		rep.Gap = true
		if err := e.pullSnapshot(rep); err != nil {
			return false, err
		}
		return false, nil
	}
	if err := e.applyItems(ch.Items, rep); err != nil {
		return false, err
	}
	e.Bind.Cursor = ch.Cursor
	// has_more 时继续拉（带上限：服务端恒返回 has_more 或游标不前进时不能无限循环）
	for pages := 1; ch.HasMore; pages++ {
		if pages >= maxPullPages {
			rep.Errors = append(rep.Errors, fmt.Sprintf("增量拉取超过 %d 页，已停止（服务端可能未推进游标）", maxPullPages))
			return true, nil
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
	return false, nil
}

// maxPullPages 单轮同步最多拉取的页数（增量与全量共用）。
const maxPullPages = 200

// requiredFeatures 客户端同步所需的服务端能力（S9 能力协商）。
var requiredFeatures = []string{"sync", "change_log"}

// negotiate 一轮同步前的能力协商：调 /api/meta 并校验协议版本与关键能力。
func (e *Engine) negotiate() error {
	meta, err := e.CLI.Meta()
	if err != nil {
		return err
	}
	if meta.SyncProto < 1 {
		return fmt.Errorf("服务端同步协议过旧（sync_proto=%d），请升级服务端", meta.SyncProto)
	}
	for _, f := range requiredFeatures {
		if !slices.Contains(meta.Features, f) {
			return fmt.Errorf("服务端缺少同步能力 %q（features=%v），无法同步", f, meta.Features)
		}
	}
	return nil
}

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
	case "example":
		return 3 // 示例依赖请求文件，排在其后
	case "variable_env":
		return 4 // 环境先于变量（变量按 env_uid 挂接）
	case "variable":
		return 5
	case "field_mapping":
		return 6 // 项目级字典，无父依赖，放最后
	default:
		return 9
	}
}

func (e *Engine) applyUpsert(it Item, rep *Report) error {
	var p Payload
	// 载荷解析失败必须中止本条应用：忽略 err 会让 p 保持零值，
	// 随后 applyAPI/applyDoc 会把一个空条目写盘，等于用垃圾覆盖本地真实数据。
	if err := json.Unmarshal(it.Payload, &p); err != nil {
		return fmt.Errorf("解析 %s %s 的同步载荷失败: %w", it.Type, it.UID, err)
	}

	// 字段字典：单文件多条目，按 uid 就地 upsert，成功后记条目摘要基线。
	if it.Type == "field_mapping" {
		return e.applyFieldMapping(it.UID, p)
	}
	// 响应示例：写 examples/ 文件即可，不进索引/冲突副本体系。
	if it.Type == "example" {
		e.snapshotExampleIfDirty(rep, it.UID, p)
		return e.applyExample(it.UID, p)
	}
	// 环境/变量：落 environments/ 文件，摘要记录在 Bind，不进索引体系。
	// 环境只有在「服务端改名」时才会动本地文件，那种情况下本地的命名意图会被静默吸收，
	// 所以覆盖成功后补一份副本（P1-7，见 applyVarEnvWithSnapshot）；
	// 同名/空名/本地没有该环境都不存在覆盖，不留。
	if it.Type == "variable_env" {
		return e.applyVarEnvWithSnapshot(rep, it.UID, p)
	}
	if it.Type == "variable" {
		e.snapshotVarIfDirty(rep, it.UID, p)
		return e.applyVariable(it.UID, p)
	}

	// 拉取前若本地已 dirty，先把本地版本存进 .conflicts/（R8 / §4.4）。
	// it.Payload 即服务端版本快照，一并存入副本，应用内解决冲突时才能「采用服务器版本」。
	if it.Type == "api" {
		e.saveLocalIfDirty(rep, it.UID, it.ItemRev, it.Payload)
	}

	var err error
	switch it.Type {
	case "group":
		err = e.applyGroup(it.UID, p)
	case "api":
		err = e.applyAPI(it.UID, p)
	case "doc":
		// 文档不进索引，本地 dirty 只能靠 DocHashes 判断：覆盖前先把本地版本留底（P0）
		e.snapshotDocIfDirty(rep, it.UID, p)
		err = e.applyDoc(it.UID, p)
		if err == nil {
			e.rememberDocHash(it.UID)
		}
	}
	if err != nil {
		return err
	}
	// 拉取应用后立即固化：用文件最新 hash（索引里还是旧值）
	if req, err := e.Coll.ReadRequest(it.UID); err == nil {
		if h := e.Coll.FileHashOf(req.Path); h != "" {
			// 标记同步状态失败最多让下一轮重复拉取一次（幂等），不该中断整轮 pull。
			_ = e.Coll.MarkSynced(it.UID, h, it.ItemRev)
		}
	} else {
		// 分组等无请求文件的条目：用哨兵值标记已同步
		_ = e.Coll.MarkSynced(it.UID, "synced", it.ItemRev)
	}
	return nil
}

// saveLocalIfDirty 本地有未推送改动时，先落冲突副本再让远端覆盖（§4.4 LWW + 副本）。
// serverPayload 为该条目的服务端版本（SyncPayload JSON），缺它则副本只能选本地。
func (e *Engine) saveLocalIfDirty(rep *Report, uid string, serverRev int64, serverPayload []byte) {
	nodes, err := e.Coll.DirtyNodes()
	if err != nil {
		return
	}
	for _, n := range nodes {
		if n.UID != uid {
			continue
		}
		if req, err := e.Coll.ReadRequest(uid); err == nil {
			// 冲突副本是本地改动的唯一留底：写失败必须让用户看到，否则改动会被远端版本直接盖掉。
			if _, cerr := e.Coll.SaveConflictCopy(req, serverRev, serverPayload); cerr != nil && rep != nil {
				rep.Errors = append(rep.Errors, req.UID+": 保存冲突副本失败（本地改动可能未留存）："+cerr.Error())
			}
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

// applyExtra 从服务端 extra 还原客户端字段（说明 / 请求头 / 请求参数 / JSON 请求体）。
func applyExtra(r *collection.Request, extra string) {
	collection.ApplySyncExtra(r, extra)
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
		// J11-2.3：按 group_uid 落到对应分组目录；根级/未分组为空串 → docs/ 默认位。
		// 组本地不存在时目录为空（拓扑序保证 group 先到，此处兜底不报错）。
		folder := ""
		if p.GroupUID != "" {
			if dir := e.findDirByUID(p.GroupUID); dir != "" {
				folder = dir
			}
		}
		created, err := e.Coll.CreateDoc(name, uid, folder)
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

// ---- 拉取覆盖前的本地版本留底（P0）----
//
// pull 的语义是「服务端版本覆盖本地」，但只有 api 在覆盖前存了 .conflicts/。
// 文档 / 响应示例 / 变量同样可能是用户在本地改过、还没推上去的，直接覆盖就是静默丢改动。
// 这里统一按「上次同步基线」判断 dirty，dirty 才留底：整文件快照（原样还原），
// 不做字段级合并——与「不做字段级三方合并」的既定边界一致。

// snapshotOnce 每个条目（kind+路径）本轮只留一份副本，并计入 Report.Conflicts。
func (e *Engine) snapshotOnce(rep *Report, kind, uid, name, rel, remote string, local []byte) {
	if len(local) == 0 || rel == "" {
		return
	}
	if e.snapped == nil {
		e.snapped = map[string]bool{}
	}
	key := kind + "\x00" + rel
	if e.snapped[key] {
		return
	}
	if _, err := e.Coll.SaveConflictSnapshot(kind, uid, name, rel, local, remote, 0); err != nil {
		if rep != nil {
			// 副本是本地改动的唯一留底：写失败必须让用户看到，否则改动会被远端版本直接盖掉。
			rep.Errors = append(rep.Errors,
				fmt.Sprintf("%s %s: 保存本地版本副本失败（本地改动可能未留存）：%v", kind, name, err))
		}
		return
	}
	e.snapped[key] = true
	if rep != nil {
		rep.Conflicts++
	}
}

// snapshotDocIfDirty 文档在本地有未推送改动时，覆盖前把本地文件原样留底。
// 基线是 Bind.DocHashes（文档不进索引，没有 synced_hash 可用）。
func (e *Engine) snapshotDocIfDirty(rep *Report, uid string, p Payload) {
	d, err := e.Coll.ReadDoc(uid)
	if err != nil {
		return // 本地没有这份文档：不存在覆盖
	}
	h := e.Coll.FileHashOf(d.Path)
	if prev, seen := e.Bind.DocHashes[uid]; seen && prev == h {
		return // 与上次同步一致：没有本地改动
	}
	local, err := os.ReadFile(filepath.Join(e.Coll.Dir, filepath.FromSlash(d.Path)))
	if err != nil {
		return
	}
	e.snapshotOnce(rep, collection.ConflictKindDoc, uid, d.Name, d.Path,
		"文档「"+p.Name+"」已被远端更新（正文见本地副本）", local)
}

// rememberDocHash 拉取应用成功后把基线前移到远端内容：否则下一轮会把它当成
// 本地改动再推回去（mirror 模式下更会每轮重复产生「伪冲突」）。
func (e *Engine) rememberDocHash(uid string) {
	d, err := e.Coll.ReadDoc(uid)
	if err != nil {
		return
	}
	h := e.Coll.FileHashOf(d.Path)
	if h == "" {
		return
	}
	if e.Bind.DocHashes == nil {
		e.Bind.DocHashes = map[string]string{}
	}
	e.Bind.DocHashes[uid] = h
}

// snapshotExampleIfDirty 响应示例的本地改动留底（基线 Bind.ExampleHashes）。
func (e *Engine) snapshotExampleIfDirty(rep *Report, uid string, p Payload) {
	list, err := e.Coll.ListAllResponseExamples()
	if err != nil {
		return
	}
	for _, ex := range list {
		if ex.UID != uid {
			continue
		}
		h := e.Coll.FileHashOf(ex.Path)
		if prev, seen := e.Bind.ExampleHashes[uid]; seen && prev == h {
			return
		}
		local, rerr := os.ReadFile(filepath.Join(e.Coll.Dir, filepath.FromSlash(ex.Path)))
		if rerr != nil {
			return
		}
		e.snapshotOnce(rep, collection.ConflictKindExample, uid, ex.Name, ex.Path,
			"响应示例「"+p.Name+"」已被远端更新", local)
		return
	}
}

// snapshotVarIfDirty 变量被远端覆盖前，把所属环境文件整份留底。
// 变量和环境共用一份 environments/<name>.yml，所以粒度是「环境文件」而不是单个变量；
// 远端值只在非 secret 时写进摘要，避免把敏感值复制到 .conflicts/。
func (e *Engine) snapshotVarIfDirty(rep *Report, uid string, p Payload) {
	env, _, err := e.envByUID(p.EnvUID)
	if err != nil || env.Name == "" {
		return // 本地没有这个环境：不存在覆盖
	}
	var localVar *collection.Var
	for i := range env.Vars {
		if env.Vars[i].UID == uid {
			localVar = &env.Vars[i]
			break
		}
	}
	if localVar == nil {
		return // 本地没有这个变量：不存在覆盖
	}
	if prev, seen := e.Bind.VarHashes[uid]; seen && prev == varHashOf(*localVar) {
		return
	}
	rel := path.Join("environments", env.Name+".yml")
	local, rerr := os.ReadFile(filepath.Join(e.Coll.Dir, filepath.FromSlash(rel)))
	if rerr != nil {
		return
	}
	remote := fmt.Sprintf("变量 %s 的远端值（敏感值不在此展示）", p.Name)
	if !p.Secret {
		remote = fmt.Sprintf("变量 %s = %s", p.Name, p.Value)
	}
	e.snapshotOnce(rep, collection.ConflictKindEnv, env.UID, env.Name, rel, remote, local)
}

// envSnapshotDraft 一次「服务端改名覆盖本地」的待写副本：旧名字、旧路径、旧文件字节。
// 字节必须在 applyVarEnv 之前读好——改名会把 environments/<old>.yml 搬进 .trash，之后读不到。
type envSnapshotDraft struct {
	name   string
	rel    string
	remote string
	local  []byte
}

// pendingEnvSnapshot 判定本轮是否真的会发生「服务端改名覆盖本地」，会则把旧文件读进草稿。
//
// 窄口子是刻意的，四种情况一律返回 nil（不读、不写、不计数）：
//   - p.Name 为空：applyVarEnv 直接返回，本地名字不变，什么都没被覆盖；
//   - 本地没有该环境：这是新建，不存在覆盖，也就没有「被吸收的本地意图」；
//   - 同名：applyVarEnv 是 no-op；
//   - 旧主文件读不出来：没有可留底的东西（主文件被外部删掉的情形）。
//
// 会被静默吸收的只有**本地的命名意图**：文件内容本身不会丢——applyVarEnv 走
// collection.RenameEnv，它把 environments/<old>.yml 与 <old>.secrets.yml 一起搬到新名字
// （旧名进 .trash）。所以副本的用途是「留下本地命名 + 主文件当时的旧样子」。
//
// 刻意**不**复制 .secrets.yml：
//   - 副本会被用户/界面/导出流程直接打开、贴进工单，明文密钥的扩散面就此变大；
//   - 副本是长期驻留的（环境删掉、改名回滚都不会连带清它），密钥会活得比环境更久；
//   - 副本要证明的东西不需要密钥——旧名字与主文件内容已足够说明「服务端改了什么」，
//     而 secret 值本身随 RenameEnv 一起平移，没有丢，需要时看新名字的 secrets 文件即可。
func (e *Engine) pendingEnvSnapshot(uid string, p Payload) *envSnapshotDraft {
	if p.Name == "" {
		return nil // 空名是 no-op：applyVarEnv 直接返回，本地名字不会变
	}
	env, _, err := e.envByUID(uid)
	if err != nil || env.Name == "" || env.Name == p.Name {
		return nil // 本地没有这个环境（新建）或同名（no-op）：不存在覆盖
	}
	rel := path.Join("environments", env.Name+".yml")
	local, rerr := os.ReadFile(filepath.Join(e.Coll.Dir, filepath.FromSlash(rel)))
	if rerr != nil {
		return nil
	}
	return &envSnapshotDraft{
		name:   env.Name,
		rel:    rel,
		remote: fmt.Sprintf("服务端把环境 %s 改名为 %s（敏感变量值不在副本内）", env.Name, p.Name),
		local:  local,
	}
}

// applyVarEnvWithSnapshot 应用环境条目，并在**覆盖真的发生之后**才留副本（P1-7）。
//
// 两段式是必须的：副本是「本地被覆盖」的证据，只有 applyVarEnv 成功返回时才有资格写。
// 先写后判会造出假冲突——除了 pendingEnvSnapshot 挡掉的空名/同名/新建，
// RenameEnv 也可能失败（例如目标名已被本地另一个环境占用），那时本地同样没被覆盖，
// 却已经多出一份 .conflicts/ 副本和一个 Report.Conflicts 计数，用户会看到并不存在的冲突。
func (e *Engine) applyVarEnvWithSnapshot(rep *Report, uid string, p Payload) error {
	draft := e.pendingEnvSnapshot(uid, p) // 旧字节必须在 apply 之前读
	if err := e.applyVarEnv(uid, p); err != nil {
		return err // 未发生覆盖：不留副本，错误照原样往上返回
	}
	if draft == nil {
		return nil
	}
	e.snapshotOnce(rep, collection.ConflictKindEnv, uid, draft.name, draft.rel, draft.remote, draft.local)
	return nil
}

// envByUID 按同步 uid 找本地环境（ListEnvs 已合并 secret 值）。
func (e *Engine) envByUID(uid string) (collection.Env, int, error) {
	envs, err := e.Coll.ListEnvs()
	if err != nil {
		return collection.Env{}, -1, err
	}
	for i, env := range envs {
		if env.UID == uid {
			return env, i, nil
		}
	}
	return collection.Env{}, -1, nil
}

// applyVarEnv 应用环境：uid 缺失则新建；名称不一致则重命名（RenameEnv 保证两文件同搬）；一致则仅记录摘要。
func (e *Engine) applyVarEnv(uid string, p Payload) error {
	if p.Name == "" {
		return nil
	}
	env, _, err := e.envByUID(uid)
	if err != nil {
		return err
	}
	switch {
	case env.Name == "": // 本地无此环境：按服务端 uid 新建
		if err := e.Coll.SaveEnv(collection.Env{UID: uid, Name: p.Name}); err != nil {
			return err
		}
	case env.Name == p.Name: // 同名：no-op
	default: // 改名：走原子重命名（旧文件进 .trash）
		if err := e.Coll.RenameEnv(env.Name, p.Name, ""); err != nil {
			return err
		}
	}
	if e.Bind.EnvHashes == nil {
		e.Bind.EnvHashes = map[string]string{}
	}
	e.Bind.EnvHashes[uid] = envHashOf(p.Name)
	return nil
}

// applyVariable 应用变量：定位所属环境（环境条目排序在前，正常必然先落好）；
// secret 值尝试口令解密——未设/错口令时按密文原样落 secrets 文件（不丢数据，设好口令后下轮重新对齐），
// 之后整份 SaveEnv（名称/启停/敏感标记/值一并覆盖）。
func (e *Engine) applyVariable(uid string, p Payload) error {
	if p.Name == "" || p.EnvUID == "" {
		return nil
	}
	// H-11：Redacted 表示服务端未下发明文敏感值（调用方没有明文权限）。
	// 本地已有值必须保留——绝不能用空值/掩码覆盖；本地没有该变量时直接跳过，
	// 不能凭空造一个空 secret（否则用户下次有写权限时会把空值推回服务端）。
	if p.Redacted {
		return nil
	}
	env, _, err := e.envByUID(p.EnvUID)
	if err != nil {
		return err
	}
	if env.Name == "" {
		return fmt.Errorf("变量 %s 的所属环境（uid=%s）尚未同步", p.Name, p.EnvUID)
	}
	value := p.Value
	if p.Secret {
		if dec, derr := e.cipher().Open(p.Value); derr == nil {
			value = dec
		}
	}
	enabled := p.Enabled == nil || *p.Enabled
	found := false
	for i, v := range env.Vars {
		if v.UID != "" && v.UID == uid {
			env.Vars[i].Name, env.Vars[i].Value = p.Name, value
			env.Vars[i].Enabled, env.Vars[i].Secret = enabled, p.Secret
			found = true
			break
		}
	}
	if !found {
		env.Vars = append(env.Vars, collection.Var{
			UID: uid, Name: p.Name, Value: value, Enabled: enabled, Secret: p.Secret,
		})
	}
	if err := e.Coll.SaveEnv(env); err != nil {
		return err // 变量落盘失败必须上报：静默会让本地与服务端值不一致
	}
	if e.Bind.VarHashes == nil {
		e.Bind.VarHashes = map[string]string{}
	}
	e.Bind.VarHashes[uid] = varHashOf(collection.Var{
		Name: p.Name, Value: value, Enabled: enabled, Secret: p.Secret,
	})
	return nil
}

// applyFieldMapping 应用字段字典条目：单文件多条目，按 uid 就地 upsert。
// 本地已有同名字段但 uid 不同时以服务端为准（服务端保证项目内 field 唯一）；
// 旧 uid 的推送基线一并清掉，避免下轮把重复条目当「本地删除」反推给服务端。
func (e *Engine) applyFieldMapping(uid string, p Payload) error {
	if p.Field == "" {
		return nil
	}
	items, err := e.Coll.ListFieldMappings()
	if err != nil {
		return err
	}
	fm := collection.FieldMapping{UID: uid, Field: p.Field, Type: p.FieldType, Description: p.Description}
	// 手工编辑可能留下同 field 的重复条目：按 uid 或同名一并替换，只保留服务端这一条。
	kept := make([]collection.FieldMapping, 0, len(items)+1)
	for _, it := range items {
		if it.UID == uid || it.Field == fm.Field {
			if it.UID != uid {
				delete(e.Bind.FieldMappingHashes, it.UID)
			}
			continue
		}
		kept = append(kept, it)
	}
	if err := e.Coll.SaveFieldMappings(append(kept, fm)); err != nil {
		return err
	}
	if e.Bind.FieldMappingHashes == nil {
		e.Bind.FieldMappingHashes = map[string]string{}
	}
	e.Bind.FieldMappingHashes[uid] = collection.FieldMappingHash(fm)
	return nil
}

func (e *Engine) applyDelete(it Item, rep *Report) {
	// 删除失败必须让用户看到：本地没删掉但服务端已删，下次同步会重新「复活」它，
	// 表现为「删了又回来」。此前这里是 `_ =`，全都静默吞掉。
	fail := func(err error) {
		if err != nil {
			rep.Errors = append(rep.Errors, it.Type+" "+it.UID+": 本地删除失败："+err.Error())
		}
	}
	switch it.Type {
	case "api":
		fail(e.Coll.DeleteRequest(it.UID))
	case "group":
		// 组删除只移除 folder.yml 语义，保留子文件（§4.5）
		if dir := e.findDirByUID(it.UID); dir != "" {
			fail(os.Remove(filepath.Join(e.Coll.Dir, filepath.FromSlash(dir), "folder.yml")))
		}
	case "example":
		// 审核 M#4：仅本地删除成功才清基线——失败时保留记录，避免下一轮把残留文件
		// 当「首次推送」再 upsert 一遍，把服务端刚删的示例又建回来。
		if err := e.Coll.DeleteResponseExampleByUID(it.UID); err == nil {
			delete(e.Bind.ExampleHashes, it.UID)
		} else {
			fail(err)
		}
	// J11-2.4：文档删除（进 .trash，可恢复）；同样只在成功时清已推送记录
	case "doc":
		if err := e.Coll.DeleteDoc(it.UID); err == nil {
			delete(e.Bind.DocHashes, it.UID)
		} else {
			fail(err)
		}
	case "variable":
		e.removeVarByUID(it.UID)
		delete(e.Bind.VarHashes, it.UID)
	case "variable_env":
		e.removeEnvByUID(it.UID)
	case "field_mapping":
		// 与文档同理：仅在本地删除成功后清基线，避免残留文件被当新条目重推。
		if err := e.Coll.DeleteFieldMapping(it.UID); err == nil {
			delete(e.Bind.FieldMappingHashes, it.UID)
		} else {
			fail(err)
		}
	}
}

// removeVarByUID 从所属环境文件里删掉该 uid 的变量（保存走 SaveEnv，secret 旁路随之收口）。
func (e *Engine) removeVarByUID(uid string) {
	env, _, err := e.envByUIDVar(uid)
	if err != nil || env.Name == "" {
		return
	}
	kept := env.Vars[:0:0]
	for _, v := range env.Vars {
		if v.UID != uid {
			kept = append(kept, v)
		}
	}
	env.Vars = kept
	// 服务端已删该变量：本地写回失败只影响这一份环境文件（下次同步会重试），
	// 但它不该让整轮 apply 失败，否则后面的条目都被卡住。
	_ = e.Coll.SaveEnv(env)
}

// envByUIDVar 按变量 uid 反查其所在环境。
func (e *Engine) envByUIDVar(uid string) (collection.Env, int, error) {
	envs, err := e.Coll.ListEnvs()
	if err != nil {
		return collection.Env{}, -1, err
	}
	for i, env := range envs {
		for _, v := range env.Vars {
			if v.UID == uid {
				return env, i, nil
			}
		}
	}
	return collection.Env{}, -1, nil
}

// removeEnvByUID 删除环境（两文件移入 .trash），并清理其变量与自身的同步摘要记录。
func (e *Engine) removeEnvByUID(uid string) {
	env, _, err := e.envByUID(uid)
	if err != nil || env.Name == "" {
		delete(e.Bind.EnvHashes, uid)
		return
	}
	for _, v := range env.Vars {
		if v.UID != "" {
			delete(e.Bind.VarHashes, v.UID)
		}
	}
	// 环境文件移入 .trash 失败也要清摘要：留着旧摘要只会让下一轮重复推同一个删除。
	_ = e.Coll.DeleteEnv(env.Name)
	delete(e.Bind.EnvHashes, uid)
}

// applyExample 应用服务端下发的响应示例：落到所属请求的 examples/ 目录。
// 请求尚未同步到本地时报错（typeRank 已保证 api 先于 example，通常不会走到）。
func (e *Engine) applyExample(uid string, p Payload) error {
	if p.APIUID == "" {
		return fmt.Errorf("example %s 缺少 api_uid", uid)
	}
	r, err := e.Coll.ReadRequest(p.APIUID)
	if err != nil {
		return fmt.Errorf("example %s 找不到所属请求 %s", uid, p.APIUID)
	}
	status := p.Status
	if status == 0 {
		status = 200
	}
	name := p.Name
	if name == "" {
		name = "示例"
	}
	ex := &collection.ResponseExample{
		UID: uid, Name: name, RequestUID: p.APIUID,
		Path: path.Join(collection.ExampleDirForPath(r.Path), name+".yml"),
		Seq:  p.Seq,
		Response: collection.ExampleResponse{
			Status: status, ContentType: p.ContentType, Body: p.Sample,
		},
	}
	finalPath, err := e.Coll.UpsertResponseExampleFile(ex)
	if err != nil {
		return err
	}
	if h := e.Coll.FileHashOf(finalPath); h != "" {
		if e.Bind.ExampleHashes == nil {
			e.Bind.ExampleHashes = map[string]string{}
		}
		e.Bind.ExampleHashes[uid] = h
	}
	return nil
}

// findDirByUID 扫描找分组目录相对路径。
// findDirByUID 分组 uid → 目录相对路径（找不到返回空串）。
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

// folderUIDByDir 目录相对路径 → 分组 uid（J11：文档 push 的 GroupUID 用）。
// 目录不是分组（如 docs/ 或未建 folder.yml 的裸目录）时返回空串 = 未分组。
func (e *Engine) folderUIDByDir(dir string) string {
	info, err := e.Coll.Info()
	if err != nil {
		return ""
	}
	var walk func(nodes []*collection.Node) string
	walk = func(nodes []*collection.Node) string {
		for _, n := range nodes {
			if n.Type != "folder" {
				continue
			}
			if n.Path == dir {
				return n.UID
			}
			if n.Children != nil {
				if u := walk(n.Children); u != "" {
					return u
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
	// 不能因为 dirty/pendingDeletes 都为空就早退：文档条目、响应示例、环境变量都不进索引，
	// 走 Bind 的 hash 基线做脏检测（collectDocOps/collectExampleOps/collectVarOps），
	// 早退会让「只改文档/示例/环境」这一轮完全不推送。真正的「没东西可推」由下面的
	// len(b.ops) == 0 兜底。
	// 拓扑序：folder 先，request 后
	sort.SliceStable(dirty, func(i, j int) bool {
		return typeRank(dirty[i].Type) < typeRank(dirty[j].Type)
	})

	b := newPushBatch(len(dirty) + len(pendingDel))
	for _, n := range dirty {
		if n.UID == "" {
			continue
		}
		switch n.Type {
		case "folder":
			e.addFolderOp(b, n)
		case "request":
			e.addRequestOp(b, n)
		}
	}
	b.collectTombstoneOps(pendingDel, e.Bind.DocHashes)
	e.collectDocOps(b)
	e.collectExampleOps(b)
	e.collectVarOps(b)
	e.collectFieldMappingOps(b)
	if len(b.ops) == 0 {
		return nil
	}
	resp, err := e.CLI.Push(&PushReq{
		ProjectID:  e.Bind.ProjectID,
		DeviceID:   e.CLI.Device,
		DeviceName: e.CLI.Device,
		Ops:        b.ops,
	})
	if err != nil {
		return err
	}
	// 回执处理（计数、冲突副本、hash 基线）照旧；返回值 allOk 表示「删除类操作是否全部成功」，
	// 但它不再决定能否清墓碑——见下面的逐条清除。
	e.applyPushResults(rep, resp, b.meta)
	// 墓碑按回执逐条清（P2-1）。别用「allOk 才 ClearPendingDeletes」一把清：
	// 同批只要有一条墓碑被服务端永久拒绝（rejected），整个列表就永远清不掉，
	// 下一轮会把同批**已成功**的删除也再发一遍——而且用的是新的 op_id，
	// 服务端按 op_id 做的幂等去重完全失效，已成功的删除就被无限重推。
	// 被拒的那条留在列表里继续重试是**正确**的（服务端拒绝需要人工介入），
	// 但它不该拖累同批已经成功的删除。
	if len(pendingDel) > 0 {
		if err := e.Coll.RemovePendingDeletes(succeededTombstones(resp, b.meta)); err != nil {
			// 清不掉不算本轮失败：墓碑还在，下一轮自然重试推送（服务端按 op_id 幂等）。
			rep.Errors = append(rep.Errors, err.Error())
		}
	}
	if resp.Cursor > e.Bind.Cursor {
		e.Bind.Cursor = resp.Cursor
	}
	return nil
}

// succeededTombstones 从回执里挑出「本条墓碑推送成功」的条目，供 RemovePendingDeletes 逐条摘除。
//
// 只认 b.meta 里带 pendingTypes 的 op：那是 collectTombstoneOps 真正从 .sync-deletes.json
// 发出去的墓碑；文档/示例由差异检测（collectDocOps/collectExampleOps）发现的删除虽然
// isDelete 也为真，但它们从来不在待删列表里，交给 RemovePendingDeletes 也只会白写一次盘。
//
// 用 pendingTypes（墓碑在文件里记录的类型）而不是 typ：旧格式墓碑无类型时 typ 会被文档基线
// 纠正成 doc，而文件里存的仍是 api，按 typ 删会摘不掉那条旧条目（P1-10 迁移路径）。
// 一条 op 可能折叠了多条墓碑（见 collectTombstoneOps），所以是逐条摘、不是一个类型。
func succeededTombstones(resp *PushResp, meta map[string]dirtyMeta) []collection.PendingDelete {
	if resp == nil {
		return nil
	}
	done := make([]collection.PendingDelete, 0, len(resp.Results))
	for _, r := range resp.Results {
		if r.Status != "ok" {
			continue // 冲突/被拒：留在线表里下轮重试
		}
		m, ok := meta[r.OpID]
		if !ok || !m.isDelete || len(m.pendingTypes) == 0 {
			continue
		}
		for _, pt := range m.pendingTypes {
			done = append(done, collection.PendingDelete{UID: m.uid, Type: pt})
		}
	}
	return done
}

// pushBatch 一批待推送操作及其元信息（回执按 OpID 反查条目）。
// ops 与 meta 必须同步登记，故用一个结构承载，避免调用方漏写其中一边。
type pushBatch struct {
	ops  []Op
	meta map[string]dirtyMeta
	// pendingDel collectTombstoneOps 本轮已登记过删除的 uid。
	// collectDocOps 的墓碑分支据此跳过，避免「一次删除发两条 op」（P1-10）：
	// 走 DeleteDoc 的文档已经在 pendingDeletes 里按 doc 类型发出去了。
	pendingDel map[string]bool
}

func newPushBatch(capacity int) *pushBatch {
	return &pushBatch{
		ops:        make([]Op, 0, capacity),
		meta:       make(map[string]dirtyMeta, capacity),
		pendingDel: make(map[string]bool, capacity),
	}
}

// add 登记一条操作及其元信息。
func (b *pushBatch) add(op Op, m dirtyMeta) {
	b.ops = append(b.ops, op)
	b.meta[op.OpID] = m
}

// addFolderOp 分组：payload 只带标题；分组无文件 hash，回执用哨兵值标同步。
func (e *Engine) addFolderOp(b *pushBatch, n index.Node) {
	op := Op{OpID: newOpID(), Op: "upsert", Type: "group", UID: n.UID}
	op.Payload, _ = json.Marshal(Payload{Title: n.Title})
	b.add(op, dirtyMeta{uid: n.UID, hash: n.Hash, typ: n.Type})
}

// addRequestOp 请求：extra 聚合请求头 / 请求参数（query + JSON 体展平）/ 说明，
// 口径见 collection.SyncExtra（与服务端 Bruno 导入一致）。读取失败则跳过本条。
func (e *Engine) addRequestOp(b *pushBatch, n index.Node) {
	req, err := e.Coll.ReadRequest(n.UID)
	if err != nil {
		return
	}
	op := Op{OpID: newOpID(), Op: "upsert", Type: "api", UID: n.UID, BaseRev: n.BaseRev}
	op.Payload, _ = json.Marshal(Payload{
		Name:   req.Name,
		Method: req.Method,
		URL:    req.URL,
		Extra:  collection.SyncExtra(req),
	})
	b.add(op, dirtyMeta{uid: n.UID, hash: n.Hash, typ: n.Type})
}

// collectTombstoneOps 本地已删除条目的墓碑（delete）：Op.Type 与 dirtyMeta.typ 都用条目自带的
// Type（api / doc），不再一律按 api 出网——否则删一篇文档会多发一条 delete/api/<docUid>，
// 服务端对未知 api uid 回 rejected 后待删列表永远清不掉（P1-10）。
// docHashes 是文档推送基线（e.Bind.DocHashes），只为「旧格式墓碑无类型」的迁移兜底而传入；
// pushBatch 拿不到 Engine，故把需要的这唯一一份数据显式传进来。
// 清墓碑不在这里做（也不看「是否全部成功」）：pushDirty 按回执逐条 RemovePendingDeletes（P2-1）。
//
// 出网前还要按「纠正后的 (uid,type)」去重：类型纠正会把不同类型折叠成同一类型——待删列表里的
// 异常数据 [{uid,api},{uid,doc}] 若命中 DocHashes，两条会被一起纠正成 doc，同一次 push 就发出
// 两条一模一样的 delete/doc/<uid>。去重键必须带 type 而不能只用 uid：api 与 doc 在服务端是两类
// 不同操作（P1-10），只按 uid 合并会把另一类删除整条丢掉（本地墓碑清了、服务端没删）。
// 被折叠掉的那条墓碑类型并入保留那条的 pendingTypes，这样该 op 成功后两条会一起被摘掉，
// 否则留下的那条永远推不成功，又变成「无限重推」。
func (b *pushBatch) collectTombstoneOps(pendingDel []collection.PendingDelete, docHashes map[string]string) {
	// 出网 op 身份（type + uid）→ OpID：纠正后同一个 (uid,type) 只发一条 op。
	opByKey := make(map[string]string, len(pendingDel))
	for _, d := range pendingDel {
		if d.UID == "" {
			continue
		}
		typ := d.Type
		if typ == "" {
			typ = collection.PendingDeleteAPI
		}
		// 旧格式（无类型）无从得知实体类型，用文档基线兜底：命中 DocHashes 的就是文档，
		// 否则会以 api 出网、被服务端按未知 uid 拒绝，pendingDeletes 永远清不掉。
		if typ == collection.PendingDeleteAPI && docHashes[d.UID] != "" {
			typ = collection.PendingDeleteDoc
		}
		// 登记进 b.pendingDel：collectDocOps 的墓碑分支据此跳过同一个 uid（P1-10）。
		b.pendingDel[d.UID] = true
		// pendingType 记墓碑在文件里**原始**记录的类型：清墓碑必须按它删，
		// 因为上面可能把 typ 从 api 纠正成 doc（旧格式迁移），而文件里存的还是 api。
		origType := d.Type
		if origType == "" {
			origType = collection.PendingDeleteAPI
		}
		key := typ + "\x00" + d.UID
		if opID, dup := opByKey[key]; dup {
			// 纠正后撞成同一个出网 op（见函数注释）：不再发第二条，只把原始类型并进去。
			m := b.meta[opID]
			m.pendingTypes = append(m.pendingTypes, origType)
			b.meta[opID] = m
			continue
		}
		op := Op{OpID: newOpID(), Op: "delete", Type: typ, UID: d.UID}
		opByKey[key] = op.OpID
		b.add(op, dirtyMeta{uid: d.UID, typ: typ, isDelete: true, pendingTypes: []string{origType}})
	}
}

// collectDocOps 文档条目（B13/J11）：按内容 hash 做脏检测只推变化项；本地已删→ tombstone。
// 安全红线（审核 C#1）：清单读不出或本地意外扫空时，绝不产生删除 op——
// 「不确定就当已删」会把远端整仓文档删掉，与「删除必进 .trash、覆盖必先留底」原则相悖。
func (e *Engine) collectDocOps(b *pushBatch) {
	docs, err := e.Coll.ListDocs()
	if err != nil {
		return // 扫描任何一步失败 ≠ 本地全删了：本轮不推文档，交下一轮重试
	}
	live := make(map[string]bool, len(docs))
	for _, d := range docs {
		if d.UID == "" {
			continue
		}
		live[d.UID] = true
		h := e.Coll.FileHashOf(d.Path)
		if prev, seen := e.Bind.DocHashes[d.UID]; seen && prev == h && h != "" {
			continue // 内容未变化，跳过
		}
		p := Payload{Name: d.Name, Content: d.Content, Icon: d.Icon}
		// J11-2.1：目录归属 = 服务端分组。根级与 docs/ 默认位都算未分组（空串），
		// 与「旧集合 docs/*.md 挂根」的既有行为一致。
		if dir := parentDir(d.Path); dir != "" && dir != "docs" {
			p.GroupUID = e.folderUIDByDir(dir)
		}
		op := Op{OpID: newOpID(), Op: "upsert", Type: "doc", UID: d.UID}
		op.Payload, _ = json.Marshal(p)
		b.add(op, dirtyMeta{uid: d.UID, hash: h, typ: "doc"})
	}
	// J11-2.2：本地已删除的文档 → tombstone（照抄 collectExampleOps 模式）
	// 保留这段基于 Bind.DocHashes 的差异检测：文档被用户在资源管理器里直接删掉（不走 DeleteDoc）
	// 时只有它能发现删除。但走了 DeleteDoc 的 uid 已经在 pendingDeletes 里，由
	// collectTombstoneOps 按 doc 类型发过一条，这里必须跳过，否则同一次删除会出两条 op（P1-10）。
	// 防呆（审核 C#1，与 P1-10 防线折中）：本地一篇不剩、基线有货、但磁盘上仍存在 .md——
	// 视为扫描异常（如 frontmatter 全体解析失败被静默丢弃），拒绝批量删除；
	// 磁盘确实无 .md 时放行：这正是「用户在资源管理器里删光了文档」的合法墓碑场景（P1-10 第④防线）。
	if len(docs) == 0 && len(e.Bind.DocHashes) > 0 && hasAnyMarkdown(e.Coll.Dir) {
		return
	}
	for uid := range e.Bind.DocHashes {
		if live[uid] || b.pendingDel[uid] {
			continue
		}
		b.add(Op{OpID: newOpID(), Op: "delete", Type: "doc", UID: uid},
			dirtyMeta{uid: uid, typ: "doc", isDelete: true})
	}
}

/** parentDir 取相对路径的父目录：根级文件返回空串。 */
func parentDir(rel string) string {
	dir := path.Dir(filepath.ToSlash(rel))
	if dir == "." || dir == "/" {
		return ""
	}
	return dir
}

// hasAnyMarkdown 磁盘上是否仍存在 .md 文件（跳过 .trash/.git 等隐藏目录）。
// 供 collectDocOps 扫空防呆：区分「真删光」与「解析失败被静默丢弃」。
func hasAnyMarkdown(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(filepath.Base(p), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(d.Name()), ".md") {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

// dirHasAnyFile 目录是否存在且含任何条目（examples 侧扫空防呆用，顶层判定即可）。
func dirHasAnyFile(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

// collectExampleOps 响应示例：按 hash 脏检测推 upsert；
// 「有推送记录但文件已不在」视为本地删除 → tombstone。
// 同 collectDocOps 的安全红线（审核 C#1 同形）：读取失败或意外扫空时不产生删除 op。
func (e *Engine) collectExampleOps(b *pushBatch) {
	examples, err := e.Coll.ListAllResponseExamples()
	if err != nil {
		return // 清单读不出≠全删：宁可不推，不能在不确定时批量删远端示例
	}
	if len(examples) == 0 && len(e.Bind.ExampleHashes) > 0 && dirHasAnyFile(filepath.Join(e.Coll.Dir, "examples")) {
		return // 磁盘仍有示例文件而清单为空 → 扫描异常，拒绝批量删除（同 doc 侧防呆）
	}
	live := make(map[string]string, len(examples)) // uid → 文件 hash
	for _, ex := range examples {
		if ex.UID == "" {
			continue
		}
		h := e.Coll.FileHashOf(ex.Path)
		live[ex.UID] = h
		if prev, seen := e.Bind.ExampleHashes[ex.UID]; seen && prev == h && h != "" {
			continue // 未变化
		}
		op := Op{OpID: newOpID(), Op: "upsert", Type: "example", UID: ex.UID}
		op.Payload, _ = json.Marshal(Payload{
			Name: ex.Name, APIUID: ex.RequestUID, Sample: ex.Response.Body,
			Status: ex.Response.Status, ContentType: ex.Response.ContentType, Seq: ex.Seq,
		})
		b.add(op, dirtyMeta{uid: ex.UID, hash: h, typ: "example"})
	}
	for uid := range e.Bind.ExampleHashes {
		if _, ok := live[uid]; ok {
			continue
		}
		b.add(Op{OpID: newOpID(), Op: "delete", Type: "example", UID: uid},
			dirtyMeta{uid: uid, typ: "example", isDelete: true})
	}
}

// collectFieldMappingOps 字段字典：按条目摘要做脏检测推 upsert；
// 「有推送记录但条目已不在」视为本地删除 → tombstone。
// ListFieldMappings 读/解析失败时本轮不推（与文档/示例同口径：不确定时绝不批量删远端）。
func (e *Engine) collectFieldMappingOps(b *pushBatch) {
	items, err := e.Coll.ListFieldMappings()
	if err != nil {
		return
	}
	// 自愈式身份分配：手工编辑新增的条目缺 uid，补一个才能参与同步。
	changed := false
	for i := range items {
		if items[i].UID == "" && strings.TrimSpace(items[i].Field) != "" {
			items[i].UID = collection.NewUID()
			changed = true
		}
	}
	if changed {
		if err := e.Coll.SaveFieldMappings(items); err != nil {
			return
		}
	}
	live := make(map[string]bool, len(items))
	for _, fm := range items {
		if fm.UID == "" {
			continue
		}
		live[fm.UID] = true
		h := collection.FieldMappingHash(fm)
		if prev, seen := e.Bind.FieldMappingHashes[fm.UID]; seen && prev == h {
			continue // 内容未变化
		}
		op := Op{OpID: newOpID(), Op: "upsert", Type: "field_mapping", UID: fm.UID}
		op.Payload, _ = json.Marshal(Payload{Field: fm.Field, FieldType: fm.Type, Description: fm.Description})
		b.add(op, dirtyMeta{uid: fm.UID, hash: h, typ: "field_mapping"})
	}
	for uid := range e.Bind.FieldMappingHashes {
		if !live[uid] {
			b.add(Op{OpID: newOpID(), Op: "delete", Type: "field_mapping", UID: uid},
				dirtyMeta{uid: uid, typ: "field_mapping", isDelete: true})
		}
	}
}

// applyPushResults 处理推送回执；返回 allOk（删除类操作是否全部成功）。
// 注意：allOk 只是诊断口径（P2-1 之后不再决定能否清 pendingDeletes，
// 墓碑改由 pushDirty 按回执逐条 RemovePendingDeletes）。
func (e *Engine) applyPushResults(rep *Report, resp *PushResp, meta map[string]dirtyMeta) bool {
	allOk := true
	for _, r := range resp.Results {
		m := meta[r.OpID]
		switch r.Status {
		case "ok":
			rep.Pushed++
			e.applyPushed(m, r)
		case "conflict":
			rep.Conflicts++
			if err := e.writeConflictCopy(m.uid, r); err != nil {
				rep.Errors = append(rep.Errors, err.Error())
			}
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
	return allOk
}

// collectVarOps 环境与变量（S16）：先补齐并落盘缺失的 uid（自愈式身份分配），再按摘要做脏检测；
// 顺序固定为「全部环境 → 全部变量」，服务端按序应用即可保证变量能找到环境。
// secret 值：设置了口令则以 enc:v1: 密文出网（已是密文的原样发、防二次加密）；
// 未设口令则跳过该条——明文绝不出本机，也不报错刷屏（下轮设好口令自然补推）。
func (e *Engine) collectVarOps(b *pushBatch) {
	envs, err := e.Coll.ListEnvs()
	if err != nil {
		return
	}
	ciph := e.cipher()
	type pendingVar struct {
		envUID string
		v      collection.Var
	}
	var pending []pendingVar
	liveEnv := map[string]bool{}
	for _, env := range envs {
		changed := false
		if env.UID == "" {
			env.UID = collection.NewUID()
			changed = true
		}
		for i := range env.Vars {
			if env.Vars[i].Name == "" {
				continue
			}
			if env.Vars[i].UID == "" {
				env.Vars[i].UID = collection.NewUID()
				changed = true
			}
		}
		if changed {
			if err := e.Coll.SaveEnv(env); err != nil {
				continue
			}
		}
		if env.UID == "" {
			continue
		}
		liveEnv[env.UID] = true
		eh := envHashOf(env.Name)
		if prev, seen := e.Bind.EnvHashes[env.UID]; !seen || prev != eh {
			op := Op{OpID: newOpID(), Op: "upsert", Type: "variable_env", UID: env.UID}
			op.Payload, _ = json.Marshal(Payload{Name: env.Name})
			b.add(op, dirtyMeta{uid: env.UID, hash: eh, typ: "variable_env"})
		}
		for _, v := range env.Vars {
			if v.UID != "" && v.Name != "" {
				pending = append(pending, pendingVar{envUID: env.UID, v: v})
			}
		}
	}
	liveVar := map[string]bool{}
	for _, pv := range pending {
		v := pv.v
		liveVar[v.UID] = true
		h := varHashOf(v)
		if prev, seen := e.Bind.VarHashes[v.UID]; seen && prev == h && h != "" {
			continue
		}
		val := v.Value
		if v.Secret {
			if !ciph.Enabled() {
				continue // 未设口令：secret 不出本机
			}
			if !secrets.IsEncrypted(val) {
				blob, _, serr := ciph.Seal(val)
				if serr != nil {
					continue
				}
				val = blob
			}
		}
		enabled := v.Enabled
		op := Op{OpID: newOpID(), Op: "upsert", Type: "variable", UID: v.UID}
		op.Payload, _ = json.Marshal(Payload{
			Name: v.Name, Value: val, Secret: v.Secret, Enabled: &enabled, EnvUID: pv.envUID,
		})
		b.add(op, dirtyMeta{uid: v.UID, hash: h, typ: "variable"})
	}
	// 「有记录但已不在本地」视为删除：变量 tombstone 先于环境。
	for uid := range e.Bind.VarHashes {
		if !liveVar[uid] {
			b.add(Op{OpID: newOpID(), Op: "delete", Type: "variable", UID: uid},
				dirtyMeta{uid: uid, typ: "variable", isDelete: true})
		}
	}
	for uid := range e.Bind.EnvHashes {
		if !liveEnv[uid] {
			b.add(Op{OpID: newOpID(), Op: "delete", Type: "variable_env", UID: uid},
				dirtyMeta{uid: uid, typ: "variable_env", isDelete: true})
		}
	}
}

// applyPushed 单条成功回执：示例 / 文档不进索引，改用 Bind.ExampleHashes / Bind.DocHashes 记录推送状态
// （upsert 记 hash、删除清记录）；其余条目 MarkSynced，无文件 hash 的（分组）用哨兵值。
func (e *Engine) applyPushed(m dirtyMeta, r OpResult) {
	if m.typ == "example" {
		if m.isDelete {
			delete(e.Bind.ExampleHashes, m.uid)
		} else if m.hash != "" {
			if e.Bind.ExampleHashes == nil {
				e.Bind.ExampleHashes = map[string]string{}
			}
			e.Bind.ExampleHashes[m.uid] = m.hash
		}
		return
	}
	if m.typ == "doc" {
		if m.isDelete {
			delete(e.Bind.DocHashes, m.uid)
		} else if m.hash != "" {
			if e.Bind.DocHashes == nil {
				e.Bind.DocHashes = map[string]string{}
			}
			e.Bind.DocHashes[m.uid] = m.hash
		}
		return
	}
	if m.typ == "field_mapping" {
		if m.isDelete {
			delete(e.Bind.FieldMappingHashes, m.uid)
		} else if m.hash != "" {
			if e.Bind.FieldMappingHashes == nil {
				e.Bind.FieldMappingHashes = map[string]string{}
			}
			e.Bind.FieldMappingHashes[m.uid] = m.hash
		}
		return
	}
	if m.typ == "variable" || m.typ == "variable_env" {
		store := e.Bind.VarHashes
		if m.typ == "variable_env" {
			store = e.Bind.EnvHashes
		}
		if m.isDelete {
			delete(store, m.uid)
			return
		}
		if m.hash != "" {
			if store == nil {
				store = map[string]string{}
				if m.typ == "variable_env" {
					e.Bind.EnvHashes = store
				} else {
					e.Bind.VarHashes = store
				}
			}
			store[m.uid] = m.hash
		}
		return
	}
	if m.uid == "" || m.isDelete {
		return
	}
	h := m.hash
	if h == "" {
		h = "synced" // 分组等无文件 hash 的条目用哨兵值
	}
	// 同上：标记失败 → 下轮重推该条（服务端按 op_id 幂等去重），不影响正确性。
	_ = e.Coll.MarkSynced(m.uid, h, r.ItemRev)
}

// dirtyMeta 一条 dirty 节点的推送元信息。
type dirtyMeta struct {
	uid      string
	hash     string
	typ      string
	isDelete bool
	// pendingTypes 该 delete op 对应的墓碑在 .sync-deletes.json 里记录的类型（可能多条：
	// 类型纠正后撞成同一条出网 op 的墓碑会被折叠进来，见 collectTombstoneOps）；
	// 空表示这条 op 不是墓碑推送（例如 collectDocOps 的差异检测删除，它不在待删列表里）。
	// 与 typ 可能不同：旧格式墓碑无类型时 typ 会被文档基线纠正成 doc，而文件里存的仍是 api，
	// 逐条清墓碑必须按文件里的原始类型删，否则那条旧条目永远摘不掉（P1-10 迁移路径）。
	pendingTypes []string
}

// writeConflictCopy 冲突：本地版本进 .conflicts/（R8，不带可同步 uid）。
// 返回错误而不是吞掉：副本是用户改动的唯一留底，写失败必须让调用方记进 Report。
func (e *Engine) writeConflictCopy(uid string, r OpResult) error {
	if uid == "" {
		return nil
	}
	req, err := e.Coll.ReadRequest(uid)
	if err != nil {
		// 读不出来通常意味着本地已经没有这条（例如刚被删）：不算副本写失败
		return nil
	}
	if _, cerr := e.Coll.SaveConflictCopy(req, r.ItemRev, r.Server); cerr != nil {
		return fmt.Errorf("%s: 保存冲突副本失败（本地改动可能未留存）：%w", uid, cerr)
	}
	return nil
}

func newOpID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// LoadBind 从 .sync.json 读绑定，并把 PAT 密文解到内存 Token。
//
// 兼容旧版明文 token：检测到后立即迁成 tokenEnc 并写回（迁移失败不阻断本次使用）。
// 密钥丢失/换机导致密文解不开时置 TokenLocked，Token 留空——调用方据此提示重新填 PAT。
func LoadBind(dir string) (BindInfo, error) {
	var b BindInfo
	data, err := os.ReadFile(filepath.Join(dir, ".sync.json"))
	if err != nil {
		return b, err
	}
	if err := json.Unmarshal(data, &b); err != nil {
		return b, err
	}
	if b.TokenEnc != "" {
		tok, derr := patvault.Open(b.TokenEnc)
		if derr != nil {
			b.TokenLocked = true
		} else {
			b.Token = tok
		}
	} else if b.legacyPlain {
		b.legacyPlain = false
		_ = SaveBind(dir, b) // 旧明文迁移：写回 tokenEnc，失败则下次读取再试
	}
	return b, nil
}

// SaveBind 写绑定：PAT 只以密文（tokenEnc）落盘，内存 Token 绝不写入文件。
//
// 空 Token 的处理：
//   - 未关联（解绑）→ 清空密文；
//   - 已关联且 TokenLocked → 保留既有密文（否则「解不开」会被一次保存误删）。
func SaveBind(dir string, b BindInfo) error {
	out := b
	out.Token = ""
	out.legacyPlain = false
	switch {
	case strings.TrimSpace(b.Token) != "":
		enc, err := patvault.Seal(b.Token)
		if err != nil {
			return fmt.Errorf("加密保存访问令牌: %w", err)
		}
		out.TokenEnc = enc
	case !b.Linked:
		out.TokenEnc = ""
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化同步绑定: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, ".sync.json"), data, 0o600)
}

// IsLinked 集合是否已关联服务端。
func IsLinked(dir string) bool {
	b, err := LoadBind(dir)
	return err == nil && b.Linked && b.ServerURL != "" && b.ProjectID > 0
}
