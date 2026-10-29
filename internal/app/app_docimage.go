package app

// 文档图片的本地上传与只读路由（Markdown 编辑器的 onUploadImg 落点）。
//
// 存储：<集合>/assets/<sha256>（内容寻址，见 collection.SaveDocImage），
// 正文引用：/doc-assets/<sha256>；桌面壳的 AssetServer.Handler 与 devserver 都挂
// App.DocAssetHandler()，所以预览里能直接显示，不依赖网络。

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"api-doc-go-client/internal/collection"
)

// SaveDocImage 保存一张 Markdown 图片：入参是 base64（也接受带 data:image/...;base64, 前缀），
// 返回可直接写进正文的 URL（/doc-assets/<sha256>）。
//
// 只允许位图且 ≤5MB（校验在 collection.SaveDocImage）；同一张图重复上传因内容寻址自动去重。
func (a *App) SaveDocImage(dataBase64 string) (string, error) {
	c, err := a.requireCollection()
	if err != nil {
		return "", err
	}
	data, err := decodeImageBase64(dataBase64)
	if err != nil {
		return "", err
	}
	hash, err := c.SaveDocImage(data)
	if err != nil {
		return "", err
	}
	return collection.DocAssetURL(hash), nil
}

// SaveDocImageInfo 保存或命中内容寻址图片，返回访问 URL 与 hash/size/deduped。
func (a *App) SaveDocImageInfo(dataBase64 string) (string, collection.DocImageInfo, error) {
	c, err := a.requireCollection()
	if err != nil {
		return "", collection.DocImageInfo{}, err
	}
	data, err := decodeImageBase64(dataBase64)
	if err != nil {
		return "", collection.DocImageInfo{}, err
	}
	info, err := c.SaveDocImageInfo(data)
	if err != nil {
		return "", collection.DocImageInfo{}, err
	}
	return collection.DocAssetURL(info.Hash), info, nil
}

// decodeImageBase64 兼容纯 base64 与 data URL；先按编码后长度挡一次，避免超大 payload 占内存。
func decodeImageBase64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "data:") {
		if i := strings.Index(s, ";base64,"); i > 0 {
			s = s[i+len(";base64,"):]
		}
	}
	// 允许换行/空格（有些 MCP 客户端会按行折叠长 base64）
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', ' ', '\t':
			return -1
		}
		return r
	}, s)
	if s == "" {
		return nil, fmt.Errorf("图片数据为空：data 传 base64（可带 data URL 前缀）")
	}
	// 4/3 膨胀：编码长度超上限就不必解码了
	if int64(len(s)) > int64(base64.StdEncoding.EncodedLen(collection.MaxDocImageBytes))+4 {
		return nil, fmt.Errorf("图片过大：上限 %d MB", collection.MaxDocImageBytes>>20)
	}
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		// 无填充的 base64 也接受
		raw, rerr := base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "="))
		if rerr != nil {
			return nil, fmt.Errorf("base64 解码失败：%w", err)
		}
		data = raw
	}
	if len(data) > collection.MaxDocImageBytes {
		return nil, fmt.Errorf("图片过大：%d 字节（上限 %d MB）", len(data), collection.MaxDocImageBytes>>20)
	}
	return data, nil
}

// DocAssetHandler 文档图片的只读路由：GET /doc-assets/<sha256>。
// 生产由 wails AssetServer.Handler 兜底（dist 里没有该路径），devserver 直接挂在 mux 上。
func (a *App) DocAssetHandler() http.Handler { return http.HandlerFunc(a.serveDocAsset) }

func (a *App) serveDocAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	hash := strings.TrimPrefix(r.URL.Path, collection.DocAssetURLPrefix)
	if hash == r.URL.Path || !collection.ValidAssetHash(hash) {
		http.NotFound(w, r)
		return
	}
	for _, c := range a.docAssetCollections() {
		data, mime, err := c.ReadDocAsset(hash)
		if err != nil {
			continue
		}
		h := w.Header()
		// 类型只来自文件头嗅探（不信请求与扩展名），并禁掉浏览器再猜
		h.Set("Content-Type", mime)
		h.Set("X-Content-Type-Options", "nosniff")
		// 内容寻址：同一 hash 的内容永不变化，可长期缓存
		h.Set("Cache-Control", "private, max-age=31536000, immutable")
		http.ServeContent(w, r, hash, time.Time{}, bytes.NewReader(data))
		return
	}
	http.NotFound(w, r)
}

// docAssetCollections 活动集合优先，其余根随后（内容寻址：任意一份命中即可）。
func (a *App) docAssetCollections() []*collection.Collection {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]*collection.Collection, 0, len(a.colls)+1)
	if e := a.activeEntryLocked(); e != nil {
		out = append(out, e.coll)
	}
	for k, e := range a.colls {
		if k != a.activeRoot {
			out = append(out, e.coll)
		}
	}
	return out
}
