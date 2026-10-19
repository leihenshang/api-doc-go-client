package collection

// 通用冲突副本（P0）：非 api 条目（文档 / 响应示例 / 环境）在被远端覆盖前，
// 把本地文件**原样**另存到 .conflicts/，避免「本地有未同步改动却被 pull 静默盖掉」。
//
// 为什么不用请求那套副本格式：请求副本是「解析成 requestFile + 摘 uid + 存 server_payload」，
// 文档是 .md（带 frontmatter）、环境是 yml、示例是 yml，三者都不该被当成请求重新解析。
// 这里改成「整份文件字节 + 溯源元信息」的快照：还原 = 把字节写回原路径，语义最直白，
// 也不会因为格式演进（新增字段）而在还原时丢内容。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// 快照副本支持的条目类型。
const (
	ConflictKindDoc     = "doc"
	ConflictKindExample = "example"
	ConflictKindEnv     = "env"
)

// conflictSnapshot 一份非 api 条目的冲突快照（.conflicts/<kind>-<name>-<ts>.yml）。
type conflictSnapshot struct {
	Version int    `yaml:"version"`
	Kind    string `yaml:"kind"` // doc | example | env
	UID     string `yaml:"uid"`
	Name    string `yaml:"name"`
	// Path 原文件相对集合根的路径（还原时写回这里）
	Path      string `yaml:"path"`
	ServerRev int64  `yaml:"server_rev,omitempty"`
	// Remote 远端版本的可读摘要（只用于展示；敏感值不落此处）
	Remote string `yaml:"remote,omitempty"`
	// Local 本地文件全文（含 frontmatter / 缩进，原样还原）
	Local string `yaml:"local"`
}

// validSnapshotKind 是否受支持的快照类型。
func validSnapshotKind(kind string) bool {
	switch kind {
	case ConflictKindDoc, ConflictKindExample, ConflictKindEnv:
		return true
	}
	return false
}

// relPathInside 校验并规范化集合内相对路径，拒绝绝对路径与越界（副本内容可被手工编辑）。
func (c *Collection) relPathInside(rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(strings.TrimSpace(rel)))
	if clean == "." || clean == "" || filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("冲突副本记录的原文件路径非法：%q", rel)
	}
	return clean, nil
}

// SaveConflictSnapshot 把本地版本另存为通用冲突副本，返回副本文件名。
// local 为文件全文（原样），remote 为远端版本摘要（可空）。写失败必须由调用方上报，
// 否则用户既看不到副本、也不知道改动已被覆盖。
func (c *Collection) SaveConflictSnapshot(kind, uid, name, rel string, local []byte, remote string, serverRev int64) (string, error) {
	if !validSnapshotKind(kind) {
		return "", fmt.Errorf("未知的冲突副本类型：%s", kind)
	}
	clean, err := c.relPathInside(rel)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(c.Dir, ".conflicts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	snap := conflictSnapshot{
		Version: 1, Kind: kind, UID: uid, Name: name,
		Path: filepath.ToSlash(clean), ServerRev: serverRev,
		Remote: remote, Local: string(local),
	}
	data, err := yaml.Marshal(snap)
	if err != nil {
		return "", fmt.Errorf("序列化冲突副本失败：%w", err)
	}
	base := sanitizeFileName(name)
	if base == "" {
		base = kind
	}
	file := fmt.Sprintf("%s-%s-%d.yml", kind, base, time.Now().UnixMilli())
	for i := 2; ; i++ {
		if _, statErr := os.Stat(filepath.Join(dir, file)); os.IsNotExist(statErr) {
			break
		}
		file = fmt.Sprintf("%s-%s-%d-%d.yml", kind, base, time.Now().UnixMilli(), i)
	}
	if err := writeFileAtomic(filepath.Join(dir, file), data, 0o644); err != nil {
		return "", err
	}
	return file, nil
}

// readSnapshotFile 读取一份通用冲突副本；不是快照格式时返回错误（调用方回退到请求副本解析）。
func (c *Collection) readSnapshotFile(file string) (*conflictSnapshot, ConflictItem, error) {
	var item ConflictItem
	full := filepath.Join(c.Dir, ".conflicts", filepath.Base(file))
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, item, fmt.Errorf("冲突副本不存在")
	}
	var probe struct {
		Version int    `yaml:"version"`
		Kind    string `yaml:"kind"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return nil, item, fmt.Errorf("不是快照副本")
	}
	if probe.Kind == "" || !validSnapshotKind(probe.Kind) {
		return nil, item, fmt.Errorf("不是快照副本")
	}
	var snap conflictSnapshot
	if err := yaml.Unmarshal(data, &snap); err != nil {
		return nil, item, fmt.Errorf("冲突副本已损坏：%w", err)
	}
	if strings.TrimSpace(snap.Path) == "" {
		return nil, item, fmt.Errorf("冲突副本缺少原文件路径")
	}
	item.File = filepath.Base(file)
	item.Kind = snap.Kind
	item.OfUID = snap.UID
	item.Name = snap.Name
	item.ServerRev = snap.ServerRev
	if st, err := os.Stat(full); err == nil {
		item.CreatedAt = st.ModTime().UnixMilli()
	}
	return &snap, item, nil
}

// snapshotDetail 快照副本的详情：不做字段级 diff（快照是整份文件），
// 只给「本地版本预览」与远端摘要，前端按 mode=snapshot 渲染。
func (c *Collection) snapshotDetail(file string) (*ConflictDetail, error) {
	snap, item, err := c.readSnapshotFile(file)
	if err != nil {
		return nil, err
	}
	return &ConflictDetail{
		ConflictItem: item,
		Mode:         ConflictModeSnapshot,
		// 远端版本已在本轮 pull 落地，所以「以远端为准」永远可用（= 丢弃副本）
		HasPayload:    true,
		Preview:       previewText(snap.Local, 400),
		RemotePreview: snap.Remote,
		Diffs:         []FieldDiff{},
	}, nil
}

// resolveSnapshot 应用快照副本的取舍：
//   - local  把本地文件全文写回原路径（下轮因摘要不一致自动重新推送）
//   - remote 丢弃副本（远端版本已在 pull 时落地，无需再写）
//   - copy   保留副本
func (c *Collection) resolveSnapshot(snap *conflictSnapshot, file, choice string) error {
	if choice == "copy" {
		return nil
	}
	full := filepath.Join(c.Dir, ".conflicts", filepath.Base(file))
	switch choice {
	case "local":
		rel, err := c.relPathInside(snap.Path)
		if err != nil {
			return err
		}
		target := filepath.Join(c.Dir, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		// 自写回环：还原也是「自己写盘」，不该被当成外部改动再触发一次重载
		c.ignoreWriteContent(filepath.ToSlash(rel), []byte(snap.Local))
		if err := writeFileAtomic(target, []byte(snap.Local), 0o644); err != nil {
			return fmt.Errorf("还原本地版本失败：%w", err)
		}
	case "remote":
		// 远端版本已应用，无需写盘
	default:
		return fmt.Errorf("未知的取舍方式：%s", choice)
	}
	return c.moveToTrash(full)
}

// previewText 截取前 n 个字符做预览（按 rune 截断，避免切坏多字节字符）。
func previewText(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "\n…"
}
