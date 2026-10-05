// Package index 本地 SQLite 索引（C1）：节点(uid/type/path/title/method/url/mtime/hash)。
// 文件仍是真相源，索引只是加速树/搜索；删库后可从目录扫描完整重建。
package index

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Node 索引里的一条（请求或分组）。
type Node struct {
	UID    string `json:"uid"`
	Type   string `json:"type"` // folder | request
	Path   string `json:"path"` // 相对集合根，斜杠分隔
	Title  string `json:"title"`
	Method string `json:"method,omitempty"`
	URL    string `json:"url,omitempty"`
	MTime  int64  `json:"mtime"` // Unix 毫秒
	Hash   string `json:"hash,omitempty"`
	// SyncedHash 上次成功同步时的内容 hash（hash != syncedHash ⇒ dirty）
	SyncedHash string `json:"syncedHash,omitempty"`
	// BaseRev 服务端条目版本（冲突基线）
	BaseRev int64 `json:"baseRev,omitempty"`
}

// DB 集合索引；每个集合一个 sqlite 文件。
type DB struct {
	db  *sql.DB
	dir string // 集合根（用于算 hash/mtime 时的路径）
}

// Open 打开（不存在则创建）集合索引；path 为 sqlite 文件完整路径。
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开索引: %w", err)
	}
	// 单连接即可：客户端单写者
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS node (
			uid    TEXT PRIMARY KEY,
			type   TEXT NOT NULL,
			path   TEXT NOT NULL,
			title  TEXT NOT NULL DEFAULT '',
			method TEXT NOT NULL DEFAULT '',
			url    TEXT NOT NULL DEFAULT '',
			mtime  INTEGER NOT NULL DEFAULT 0,
			hash   TEXT NOT NULL DEFAULT '',
			synced_hash TEXT NOT NULL DEFAULT '',
			base_rev INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_node_path ON node(path);
		CREATE INDEX IF NOT EXISTS idx_node_title ON node(title);
	`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("初始化索引: %w", err)
	}
	// 旧库补列（幂等）
	for _, alter := range []string{
		`ALTER TABLE node ADD COLUMN synced_hash TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE node ADD COLUMN base_rev INTEGER NOT NULL DEFAULT 0`,
	} {
		_, _ = db.Exec(alter)
	}
	// 收权：索引里有请求 URL（可能带 user:pass@）与标题，不应被同机其他用户读取。
	// sqlite 驱动不会按 0600 建文件，这里显式 chmod 一次。
	_ = os.Chmod(path, 0o600)
	return &DB{db: db}, nil
}

// Close 关闭索引。
func (d *DB) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	return d.db.Close()
}

// Rebuild 用给定节点列表整表重建。
// 会保留各 uid 原有的 synced_hash / base_rev（同步状态不能因重建丢失）。
func (d *DB) Rebuild(nodes []Node) error {
	// 先读出旧同步状态
	prevHash := map[string]string{}
	prevRev := map[string]int64{}
	if rows, err := d.db.Query(`SELECT uid, synced_hash, base_rev FROM node`); err == nil {
		for rows.Next() {
			var uid, sh string
			var rev int64
			if rows.Scan(&uid, &sh, &rev) == nil {
				prevHash[uid] = sh
				prevRev[uid] = rev
			}
		}
		rows.Close()
	}

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM node`); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO node(uid,type,path,title,method,url,mtime,hash,synced_hash,base_rev) VALUES(?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, n := range nodes {
		if n.UID == "" {
			continue
		}
		sh := n.SyncedHash
		if sh == "" {
			sh = prevHash[n.UID]
		}
		rev := n.BaseRev
		if rev == 0 {
			rev = prevRev[n.UID]
		}
		if _, err := stmt.Exec(n.UID, n.Type, n.Path, n.Title, n.Method, n.URL, n.MTime, n.Hash, sh, rev); err != nil {
			return fmt.Errorf("写入索引 %s: %w", n.UID, err)
		}
	}
	return tx.Commit()
}

// Upsert 插入或更新一条。
func (d *DB) Upsert(n Node) error {
	if n.UID == "" {
		return fmt.Errorf("缺少 uid")
	}
	_, err := d.db.Exec(`
		INSERT INTO node(uid,type,path,title,method,url,mtime,hash,synced_hash,base_rev) VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(uid) DO UPDATE SET
			type=excluded.type, path=excluded.path, title=excluded.title,
			method=excluded.method, url=excluded.url, mtime=excluded.mtime, hash=excluded.hash,
			synced_hash=CASE WHEN excluded.synced_hash != '' THEN excluded.synced_hash ELSE node.synced_hash END,
			base_rev=CASE WHEN excluded.base_rev != 0 THEN excluded.base_rev ELSE node.base_rev END
	`, n.UID, n.Type, n.Path, n.Title, n.Method, n.URL, n.MTime, n.Hash, n.SyncedHash, n.BaseRev)
	return err
}

// Delete 按 uid 删除。
func (d *DB) Delete(uid string) error {
	_, err := d.db.Exec(`DELETE FROM node WHERE uid=?`, uid)
	return err
}

// Get 按 uid 取一条。
func (d *DB) Get(uid string) (Node, bool, error) {
	var n Node
	err := d.db.QueryRow(`SELECT uid,type,path,title,method,url,mtime,hash,synced_hash,base_rev FROM node WHERE uid=?`, uid).
		Scan(&n.UID, &n.Type, &n.Path, &n.Title, &n.Method, &n.URL, &n.MTime, &n.Hash, &n.SyncedHash, &n.BaseRev)
	if err == sql.ErrNoRows {
		return Node{}, false, nil
	}
	if err != nil {
		return Node{}, false, err
	}
	return n, true, nil
}

// List 全部节点（按 path 排序，稳定）。
func (d *DB) List() ([]Node, error) {
	rows, err := d.db.Query(`SELECT uid,type,path,title,method,url,mtime,hash,synced_hash,base_rev FROM node ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// Dirty 返回内容 hash 与上次同步不一致的节点（精确 dirty 集）。
// 分组无文件 hash，用 base_rev>0 且 synced_hash 为空视为待推；有 hash 时按 hash 比。
func (d *DB) Dirty() ([]Node, error) {
	rows, err := d.db.Query(`
		SELECT uid,type,path,title,method,url,mtime,hash,synced_hash,base_rev FROM node
		WHERE (hash != '' AND hash != synced_hash)
		   OR (hash = '' AND synced_hash = '' AND title != '')
		ORDER BY type, path
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// MarkSynced 一条同步成功：记录当前 hash 为已同步，并更新 base_rev。
func (d *DB) MarkSynced(uid, hash string, baseRev int64) error {
	_, err := d.db.Exec(`UPDATE node SET synced_hash=?, base_rev=? WHERE uid=?`, hash, baseRev, uid)
	return err
}

// Search 标题/URL/方法的模糊搜索（本地索引级，H4 的索引实现）。
// q 为空时返回全部。limit <= 0 不限。
func (d *DB) Search(q string, limit int) ([]Node, error) {
	q = strings.TrimSpace(q)
	var (
		rows *sql.Rows
		err  error
	)
	if q == "" {
		rows, err = d.db.Query(`SELECT uid,type,path,title,method,url,mtime,hash,synced_hash,base_rev FROM node ORDER BY path`)
	} else {
		like := "%" + q + "%"
		rows, err = d.db.Query(`
			SELECT uid,type,path,title,method,url,mtime,hash,synced_hash,base_rev FROM node
			WHERE title LIKE ? OR url LIKE ? OR method LIKE ? OR path LIKE ?
			ORDER BY path
		`, like, like, like, like)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out, err := scanNodes(rows)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Count 节点总数。
func (d *DB) Count() (int, error) {
	var n int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM node`).Scan(&n)
	return n, err
}

// MTime 便捷：把文件 mtime 转 Unix 毫秒。
func MTime(info os.FileInfo) int64 {
	if info == nil {
		return time.Now().UnixMilli()
	}
	return info.ModTime().UnixMilli()
}

func scanNodes(rows *sql.Rows) ([]Node, error) {
	out := []Node{}
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.UID, &n.Type, &n.Path, &n.Title, &n.Method, &n.URL, &n.MTime, &n.Hash, &n.SyncedHash, &n.BaseRev); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
