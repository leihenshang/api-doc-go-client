package collection

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// create_request 的冲突策略（评审 P0-2）：与 create_doc 对齐，默认 error。
const (
	RequestConflictError     = "error"
	RequestConflictSuffix    = "suffix"
	RequestConflictOverwrite = "overwrite"
)

// ErrRequestConflict 同路径或同 uid 的请求已存在（默认策略下返回）。
var ErrRequestConflict = errors.New("接口请求已存在")

// NormalizeRequestConflict 校验并归一冲突策略；空值 = error（与文档创建一致）。
func NormalizeRequestConflict(v string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", RequestConflictError:
		return RequestConflictError, nil
	case RequestConflictSuffix:
		return RequestConflictSuffix, nil
	case RequestConflictOverwrite:
		return RequestConflictOverwrite, nil
	default:
		return "", fmt.Errorf("onConflict 只能是 error/suffix/overwrite（收到 %q）", v)
	}
}

// RequestCreateOpts Plan/CreateRequestWithOpts 的入参（FileName 可选，见该字段说明）。
type RequestCreateOpts struct {
	Folder     string   // 目标模块（相对集合根；空 = 项目根）
	Name       string   // 请求名
	Src        *Request // 草稿内容
	UID        string   // 可选：指定 uid 做幂等
	OnConflict string   // error（默认）/ suffix / overwrite
	// FileName 可选：落盘文件名（不含 .yml）。留空 = 由 Name slug 化推导（既有行为）。
	// 用途与 CreateDocOpts.FileName 一致：显示名是中文时文件也会是中文名，
	// 需要 ASCII 文件名（脚本/工具链友好）时由调用方显式给。
	FileName string
}

// PlanCreateRequest 只做冲突判定与身份/路径分配，不写盘（dryRun）。
func (c *Collection) PlanCreateRequest(folder, name string, src *Request, uid, onConflict string) (*Request, error) {
	return c.PlanCreateRequestWithOpts(RequestCreateOpts{
		Folder: folder, Name: name, Src: src, UID: uid, OnConflict: onConflict,
	})
}

// PlanCreateRequestWithOpts 同 PlanCreateRequest，但可显式指定落盘文件名（FileName）。
func (c *Collection) PlanCreateRequestWithOpts(o RequestCreateOpts) (*Request, error) {
	mode, err := NormalizeRequestConflict(o.OnConflict)
	if err != nil {
		return nil, err
	}
	name := o.Name
	next, err := requestFromDraft(name, o.Src)
	if err != nil {
		return nil, err
	}
	uid := strings.TrimSpace(o.UID)
	folder := o.Folder
	res, err := c.scan()
	if err != nil {
		return nil, err
	}
	var old *Request
	if uid != "" {
		old = res.reqs[uid]
	}
	if old != nil && mode != RequestConflictOverwrite {
		return nil, fmt.Errorf("%w：uid=%s（path=%s）。如确需并存请传 onConflict=suffix，覆盖已有内容请传 onConflict=overwrite",
			ErrRequestConflict, old.UID, old.Path)
	}
	base := sanitizeFileName(name)
	if fn := strings.TrimSpace(o.FileName); fn != "" {
		// 与 name 同口径：显式 fileName 不合法时报错，而不是清洗成另一个名字。
		if !validEntryName(fn) {
			return nil, fmt.Errorf("fileName 含非法字符或为空: %q（不能带路径分隔符，不能以点开头或结尾）", fn)
		}
		base = sanitizeFileName(fn)
	}
	rel := joinRel(folder, base+".yml")
	for i := 2; old == nil; i++ {
		if hit, ok := res.reqByPath[rel]; ok {
			switch mode {
			case RequestConflictError:
				return nil, fmt.Errorf("%w：path=%s（uid=%s）。如确需并存请传 onConflict=suffix，覆盖已有内容请传 onConflict=overwrite",
					ErrRequestConflict, hit.Path, hit.UID)
			case RequestConflictOverwrite:
				old = hit
			}
		}
		if old != nil {
			break
		}
		if _, statErr := os.Stat(filepath.Join(c.Dir, filepath.FromSlash(rel))); os.IsNotExist(statErr) {
			break
		}
		rel = joinRel(folder, fmt.Sprintf("%s-%d.yml", base, i))
	}
	if old != nil {
		next.UID, next.Path, next.Seq, next.Name = old.UID, old.Path, old.Seq, name
		return next, nil
	}
	if uid == "" {
		uid = uuid.NewString()
	}
	next.UID, next.Name, next.Path, next.Seq = uid, name, rel, c.nextSeq(folder)
	return next, nil
}

// CreateRequestFromDraftEx 按 uid/onConflict 落盘草稿：
//   - error（默认）：同 uid 或同路径已存在 → ErrRequestConflict（带已有 uid/path）
//   - suffix：同路径已存在 → 追加 -2、-3…（同 uid 视为冲突，不能复制身份）
//   - overwrite：同 uid 或同路径已存在 → 复用其 uid/path/seq 覆盖内容
func (c *Collection) CreateRequestFromDraftEx(folder, name string, src *Request, uid, onConflict string) (*Request, error) {
	return c.CreateRequestWithOpts(RequestCreateOpts{
		Folder: folder, Name: name, Src: src, UID: uid, OnConflict: onConflict,
	})
}

// CreateRequestWithOpts 同 CreateRequestFromDraftEx，但可显式指定落盘文件名（FileName）。
func (c *Collection) CreateRequestWithOpts(o RequestCreateOpts) (*Request, error) {
	next, err := c.PlanCreateRequestWithOpts(o)
	if err != nil {
		return nil, err
	}
	if err := c.ensureDir(o.Folder); err != nil {
		return nil, err
	}
	if err := c.SaveRequest(next); err != nil {
		return nil, err
	}
	return next, nil
}
