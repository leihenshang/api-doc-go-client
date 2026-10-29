package syncengine

// 文档图片的同步边界转换（方案 A 的补齐：图片随文档一起同步）。
//
// 本地正文引用 `/doc-assets/<sha256>`（内容寻址，离线可看）；服务端/Web 只认
// `/uploads/<name>`（公开静态）或 `/api/file/<hash>`。
// 因此：
//   - 推送前 serverize：本地 hash → 服务端 URL（首次上传，之后走 Bind.AssetURLs 复用）
//   - 拉取后 localize：服务端 URL → 下载落 assets/<hash> → 本地 URL
//
// 两端都只处理「同步服务端同源」的图片地址：外链（https://cdn…）原样保留，
// 既不把 PAT 发给第三方，也不做 SSRF。映射持久化在 .sync.json 的 assetUrls，
// 所以「只改文字不改图」的推送不会重复上传。

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"api-doc-go-client/internal/collection"
)

const (
	// serverUploadPrefix 服务端公开静态资源（Web 文档里的图片就是这种形态）。
	serverUploadPrefix = "/uploads/"
	// serverFilePrefix 服务端内容寻址取文件（S13；需要登录，客户端带 PAT 可下）。
	serverFilePrefix = "/api/file/"
)

// docImageMimeExt 服务端上传需要的文件名后缀（按文件头嗅探结果选，不信本地扩展名）。
var docImageMimeExt = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// localizeContent 拉取落盘前调用：把服务端图片换成本地 /doc-assets/<hash>。
// 下载失败只记 Report.Errors、保留原 URL（文档仍可用，图片稍后由 R9 兜底）。
func (e *Engine) localizeContent(content string, rep *Report) string {
	urls := collection.DocImageURLs(content)
	if len(urls) == 0 {
		return content
	}
	repl := make(map[string]string, len(urls))
	for _, u := range urls {
		if _, done := repl[u]; done {
			continue
		}
		if strings.HasPrefix(u, collection.DocAssetURLPrefix) {
			continue // 已经是本地形态（例如旧版本落盘的内容）
		}
		if !e.isServerImageURL(u) {
			continue // 外链不动
		}
		hash, err := e.storeRemoteAsset(u, rep)
		if err != nil {
			e.reportErr(rep, fmt.Sprintf("文档图片 %s 未能本地化：%v", u, err))
			continue
		}
		repl[u] = collection.DocAssetURL(hash)
	}
	for src, dst := range repl {
		content = strings.ReplaceAll(content, src, dst)
	}
	return content
}

// serverizeContent 推送前调用：把本地 /doc-assets/<hash> 换成服务端可渲染的 URL。
// 上传失败只记 Report.Errors、保留本地 URL（不阻断正文同步，失败项下一轮会重试）。
func (e *Engine) serverizeContent(content string, rep *Report) string {
	if !strings.Contains(content, collection.DocAssetURLPrefix) {
		return content
	}
	var b strings.Builder
	rest := content
	for {
		i := strings.Index(rest, collection.DocAssetURLPrefix)
		if i < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:i])
		rest = rest[i:]
		hash, ok := leadingAssetHash(rest[len(collection.DocAssetURLPrefix):])
		if !ok {
			// 不是合法 hash（可能是说明文字里的示例）：原样输出前缀，继续找下一个
			b.WriteString(rest[:len(collection.DocAssetURLPrefix)])
			rest = rest[len(collection.DocAssetURLPrefix):]
			continue
		}
		url := e.Bind.AssetURLs[hash]
		if url == "" {
			uploaded, err := e.uploadLocalAsset(hash)
			if err != nil {
				e.reportErr(rep, fmt.Sprintf("文档图片 %s 未能上传：%v", hash, err))
				b.WriteString(collection.DocAssetURLPrefix + hash)
				rest = rest[len(collection.DocAssetURLPrefix)+len(hash):]
				continue
			}
			url = uploaded
		}
		b.WriteString(url)
		rest = rest[len(collection.DocAssetURLPrefix)+len(hash):]
	}
	return b.String()
}

// leadingAssetHash 从 URL 前缀之后取出 64 位 hex（后面不能紧跟 hex，避免截断更长的串）。
func leadingAssetHash(s string) (string, bool) {
	if len(s) < 64 {
		return "", false
	}
	hash := s[:64]
	if !collection.ValidAssetHash(hash) {
		return "", false
	}
	if len(s) > 64 && isHexByte(s[64]) {
		return "", false
	}
	return hash, true
}

func isHexByte(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
}

// isServerImageURL 是否指向同步服务端的图片（相对 /uploads、/api/file，或同源绝对地址）。
func (e *Engine) isServerImageURL(raw string) bool {
	if strings.HasPrefix(raw, serverUploadPrefix) || strings.HasPrefix(raw, serverFilePrefix) {
		return true
	}
	full, err := e.CLI.sameOriginAssetURL(raw)
	return err == nil && full != ""
}

// storeRemoteAsset 下载一张服务端图片并落到本地 assets/，返回内容 hash。
func (e *Engine) storeRemoteAsset(raw string, rep *Report) (string, error) {
	// /api/file/<hash> 且本地已有同一份：不必再下载
	if h, ok := strings.CutPrefix(raw, serverFilePrefix); ok {
		if collection.ValidAssetHash(h) && e.Coll.HasAsset(h) {
			e.rememberAssetURL(h, raw)
			return h, nil
		}
	}
	data, err := e.CLI.FetchAsset(raw)
	if err != nil {
		return "", err
	}
	if collection.SniffImageMime(data) == "" {
		return "", fmt.Errorf("不是支持的图片类型（PNG/JPEG/GIF/WebP）")
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	existed := e.Coll.HasAsset(hash)
	if !existed {
		if _, err := e.Coll.SaveAsset(hash, data); err != nil {
			return "", err
		}
	}
	countNewAsset(rep, existed)
	e.rememberAssetURL(hash, raw)
	return hash, nil
}

// uploadLocalAsset 把本地 assets/<hash> 上传到服务端，返回服务端 URL。
func (e *Engine) uploadLocalAsset(hash string) (string, error) {
	if !collection.ValidAssetHash(hash) {
		return "", fmt.Errorf("图片文件名非法")
	}
	full := e.Coll.AssetPath(hash)
	// 双保险：内容里的 hash 已校验过，这里仍要求最终路径落在 assets/ 内（防路径穿越）。
	if filepath.Dir(full) != filepath.Join(e.Coll.Dir, "assets") {
		return "", fmt.Errorf("图片路径非法")
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("本地图片不存在: %w", err)
	}
	mime := collection.SniffImageMime(data)
	ext, ok := docImageMimeExt[mime]
	if !ok {
		return "", fmt.Errorf("不是支持的图片类型")
	}
	// 文件名只用于服务端校验扩展名（服务端会随机改名）；带上 hash 前缀便于排障
	url, remoteHash, err := e.CLI.UploadAsset(data, "doc-"+hash[:12]+ext)
	if err != nil {
		return "", err
	}
	if remoteHash != "" && remoteHash != hash {
		// 内容被中间层篡改或服务端算法不一致：不能把错图映射给这份内容
		return "", fmt.Errorf("服务端返回的 hash 与本地不一致（%s != %s）", remoteHash, hash)
	}
	e.rememberAssetURL(hash, url)
	return url, nil
}

// rememberAssetURL 记录「本地 hash ↔ 服务端 URL」，下一次推送直接复用。
func (e *Engine) rememberAssetURL(hash, url string) {
	if hash == "" || url == "" {
		return
	}
	if e.Bind.AssetURLs == nil {
		e.Bind.AssetURLs = make(map[string]string, 4)
	}
	if _, seen := e.Bind.AssetURLs[hash]; !seen {
		e.Bind.AssetURLs[hash] = url
	}
}

// reportErr 收集不阻断同步的错误（图片失效不该让整轮同步失败）。
func (e *Engine) reportErr(rep *Report, msg string) {
	if rep == nil {
		return
	}
	rep.Errors = append(rep.Errors, msg)
}

// countNewAsset 本地从无到有时把 Report.Assets +1（状态栏「镜像附件 N 个」）。
func countNewAsset(rep *Report, existed bool) {
	if rep != nil && !existed {
		rep.Assets++
	}
}
