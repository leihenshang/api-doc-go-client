package collection

// 文档图片（Markdown 编辑器的本地上传）：
// 内容寻址保存在 <集合>/assets/<sha256>（与 R9 离线附件同目录同命名，天然去重），
// 正文引用固定为 /doc-assets/<sha256>（桌面壳与 devserver 提供只读路由）。
//
// 只接受位图（PNG / JPEG / GIF / WebP）：
//   - SVG 能在同源页面里执行脚本，绝不按图片放行；
//   - BMP / TIFF 体积大且各端支持参差，不做白名单。

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// MaxDocImageBytes 单张图片上限（前端提示、MCP 校验与这里保持同一数值）。
	MaxDocImageBytes = 5 << 20
	// DocAssetURLPrefix 文档图片的只读 URL 前缀。
	// 刻意不用 /assets：那是 Vite 构建产物的目录（dist/assets/*.js），会互相覆盖。
	DocAssetURLPrefix = "/doc-assets/"
)

// SniffImageMime 按文件头识别允许的图片类型；不认识返回空串。
func SniffImageMime(data []byte) string {
	switch {
	case len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a"):
		return "image/gif"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}

// ValidAssetHash 是否为本项目生成的 sha256 文件名。
// 同时用于挡路径穿越：只允许 64 位小写 hex，`../` 之类直接判非法。
func ValidAssetHash(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	for i := 0; i < len(hash); i++ {
		c := hash[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// DocAssetURL 由内容 hash 生成 Markdown 里引用的 URL。
func DocAssetURL(hash string) string { return DocAssetURLPrefix + hash }

// SaveDocImage 校验类型与大小后按内容寻址落盘，返回 sha256。
// DocImageInfo 保存结果：内容寻址 hash、字节数、是否命中已有附件（同图去重）。
type DocImageInfo struct {
	Hash    string
	Size    int
	Deduped bool
}

// SaveDocImageInfo 保存（或命中）内容寻址图片，返回 hash/size/deduped。
func (c *Collection) SaveDocImageInfo(data []byte) (DocImageInfo, error) {
	if len(data) == 0 {
		return DocImageInfo{}, fmt.Errorf("图片内容为空")
	}
	if len(data) > MaxDocImageBytes {
		return DocImageInfo{}, fmt.Errorf("图片过大：%d 字节（上限 %d MB）", len(data), MaxDocImageBytes>>20)
	}
	if SniffImageMime(data) == "" {
		return DocImageInfo{}, fmt.Errorf("不支持的图片类型：只接受 PNG / JPEG / GIF / WebP")
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	_, statErr := os.Stat(c.AssetPath(hash))
	deduped := statErr == nil
	if _, err := c.SaveAsset(hash, data); err != nil {
		return DocImageInfo{}, err
	}
	return DocImageInfo{Hash: hash, Size: len(data), Deduped: deduped}, nil
}

func (c *Collection) SaveDocImage(data []byte) (string, error) {
	info, err := c.SaveDocImageInfo(data)
	if err != nil {
		return "", err
	}
	return info.Hash, nil
}

// ReadDocAsset 读取本地附件并返回嗅探出的图片类型；不是白名单位图按「不存在」处理。
func (c *Collection) ReadDocAsset(hash string) ([]byte, string, error) {
	if !ValidAssetHash(hash) {
		return nil, "", os.ErrNotExist
	}
	full := c.AssetPath(hash)
	// 双保险：即使上面的 hex 校验被改动，也要求最终路径仍落在 assets/ 内（防路径穿越）。
	if filepath.Dir(full) != filepath.Join(c.Dir, assetsDir) {
		return nil, "", os.ErrNotExist
	}
	st, err := os.Stat(full)
	if err != nil {
		return nil, "", err
	}
	// 本地文件也可能来自 R9 下载（上限 32MB），读前先挡一次，避免异常大文件把内存吃满。
	if st.Size() > maxDocAssetRead {
		return nil, "", os.ErrNotExist
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, "", err
	}
	mime := SniffImageMime(data)
	if mime == "" {
		return nil, "", os.ErrNotExist
	}
	return data, mime, nil
}

// maxDocAssetRead 允许对外提供的附件上限（对齐 R9 的单个附件下载上限）。
const maxDocAssetRead = 32 << 20
