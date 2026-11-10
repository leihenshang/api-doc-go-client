package collection

import (
	"fmt"
	"path/filepath"
)

// CloneRequest 在同一分组内复制一个请求，显示名自动加「复制N」后缀（N 从 1 起取第一个不重复的）。
//
// 命名为什么是「原名复制1」而不是「原名 (1)」：中文界面里更短、更直观，也和「复制 ID」
// 这类既有措辞一致。文件名不在这里管 —— saveNewRequest 会 sanitize 并按磁盘冲突补 -2，
// 这里只保证**显示名**在同一分组里不重复，用户看到的两行才有区别。
func (c *Collection) CloneRequest(uid string) (*Request, error) {
	src, err := c.ReadRequest(uid)
	if err != nil {
		return nil, err
	}
	folder := folderOfPath(src.Path)
	name, err := c.nextCloneName(folder, src.Name)
	if err != nil {
		return nil, err
	}
	cp := cloneRequest(src)
	// 身份字段全部交给 saveNewRequest 重新分配；
	// ExpectHash 是「源文件」的基线，带过去会让副本一落盘就报冲突。
	cp.UID, cp.Path, cp.Seq, cp.ExpectHash = "", "", 0, ""
	return c.saveNewRequest(folder, name, cp)
}

// folderOfPath 请求所在分组（相对路径；根 = 空串）。
func folderOfPath(p string) string {
	dir := filepath.ToSlash(filepath.Dir(p))
	if dir == "." || dir == "/" {
		return ""
	}
	return dir
}

// nextCloneName 「原名复制1」「原名复制2」…里取第一个没被占用的。
func (c *Collection) nextCloneName(folder, base string) (string, error) {
	taken, err := c.requestNamesIn(folder)
	if err != nil {
		return "", err
	}
	for i := 1; i <= 999; i++ {
		name := fmt.Sprintf("%s复制%d", base, i)
		if !taken[name] {
			return name, nil
		}
	}
	return "", fmt.Errorf("「%s」的副本过多（已到 999），请先清理", base)
}

// requestNamesIn 某个分组下现有的请求显示名集合（只比同一层，跨分组的同名互不影响）。
func (c *Collection) requestNamesIn(folder string) (map[string]bool, error) {
	res, err := c.scan()
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(res.reqs))
	for _, r := range res.reqs {
		if folderOfPath(r.Path) == folder {
			out[r.Name] = true
		}
	}
	return out, nil
}

// cloneRequest 值拷贝请求；切片与指针各复制一份，避免副本和源共享底层数据。
func cloneRequest(src *Request) *Request {
	cp := *src
	cp.Params = append([]KV(nil), src.Params...)
	cp.Headers = append([]KV(nil), src.Headers...)
	cp.VarsPreRequest = append([]ScriptVar(nil), src.VarsPreRequest...)
	cp.Asserts = append([]ScriptAssert(nil), src.Asserts...)
	if src.Auth != nil {
		v := *src.Auth
		cp.Auth = &v
	}
	if src.Settings != nil {
		v := *src.Settings
		cp.Settings = &v
	}
	if src.GRPC != nil {
		v := *src.GRPC
		cp.GRPC = &v
	}
	if src.Script != nil {
		v := *src.Script
		cp.Script = &v
	}
	if src.RespDoc != nil {
		v := *src.RespDoc
		cp.RespDoc = &v
	}
	return &cp
}
