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

// CreateRequestFromDraftEx 按 uid/onConflict 落盘草稿：
//   - error（默认）：同 uid 或同路径已存在 → ErrRequestConflict（带已有 uid/path）
//   - suffix：同路径已存在 → 追加 -2、-3…（同 uid 视为冲突，不能复制身份）
//   - overwrite：同 uid 或同路径已存在 → 复用其 uid/path/seq 覆盖内容
func (c *Collection) CreateRequestFromDraftEx(folder, name string, src *Request, uid, onConflict string) (*Request, error) {
	mode, err := NormalizeRequestConflict(onConflict)
	if err != nil {
		return nil, err
	}
	next, err := requestFromDraft(name, src)
	if err != nil {
		return nil, err
	}
	uid = strings.TrimSpace(uid)
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
		if err := c.SaveRequest(next); err != nil {
			return nil, err
		}
		return next, nil
	}

	if err := c.ensureDir(folder); err != nil {
		return nil, err
	}
	if uid == "" {
		uid = uuid.NewString()
	}
	next.UID, next.Name, next.Path, next.Seq = uid, name, rel, c.nextSeq(folder)
	if err := c.SaveRequest(next); err != nil {
		return nil, err
	}
	return next, nil
}
