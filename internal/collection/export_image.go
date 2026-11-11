package collection

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// maxInlineImageBytes 单张图内联上限：超过就保留原地址（宁可破图，也不产出一个上百 MB 的 HTML）。
const maxInlineImageBytes = 8 << 20

// inlineImageTimeout 网络图片的下载超时：导出是交互操作，不能让一张图卡住整次导出。
const inlineImageTimeout = 5 * time.Second

// ImageInlineStats 图片内联统计（导出结果提示用）。
type ImageInlineStats struct {
	Inlined int `json:"inlined"` // 成功内联（本地 + 网络）
	Remote  int `json:"remote"`  // 其中来自网络
	Skipped int `json:"skipped"` // 本可内联但失败：读不到 / 非图片 / 超限
}

// imgSrcRE 只匹配 <img> 的 src 属性。
// 不用「替换所有 /doc-assets/」的原因：那会把正文里作为示例的路径文本一起改掉，
// 也会误伤 <a href>；按 <img> 标签取属性，范围最小、最不容易误伤。
var imgSrcRE = regexp.MustCompile(`(?i)(<img\b[^>]*?\bsrc=")([^"]*)(")`)

// InlineImages 把 HTML 里的图片地址换成 data URL，产出真正的单文件 HTML。
//
//   - /doc-assets/<hash>：读集合 assets/ 下的字节（ReadDocAsset 已做 hash 校验与路径防护）
//   - http(s)://：remote=true 时才下载（5s 超时、8MB 上限、只接受 image/*）
//
// 内联失败的保留原地址：导出文件里是破图，但整次导出不会失败 —— 用户至少能看到
// 哪张图没带上，而不是收到一份悄悄少了几张图的文档。
func (c *Collection) InlineImages(htmlText string, remote bool) (string, ImageInlineStats) {
	var st ImageInlineStats
	matches := imgSrcRE.FindAllStringSubmatchIndex(htmlText, -1)
	if len(matches) == 0 {
		return htmlText, st
	}
	cache := make(map[string]string, len(matches))
	client := &http.Client{Timeout: inlineImageTimeout}
	var b strings.Builder
	b.Grow(len(htmlText) + 64*1024)
	last := 0
	for _, m := range matches {
		b.WriteString(htmlText[last:m[4]])
		src := htmlText[m[4]:m[5]]
		url, ok, isRemote := c.inlineOne(src, remote, cache, client)
		switch {
		case ok:
			b.WriteString(url)
			st.Inlined++
			if isRemote {
				st.Remote++
			}
		default:
			b.WriteString(src)
			if isRemote || strings.HasPrefix(src, DocAssetURLPrefix) {
				st.Skipped++
			}
		}
		last = m[5]
	}
	b.WriteString(htmlText[last:])
	return b.String(), st
}

// inlineOne 处理单个 src：返回替换值、是否替换成功、是否属于网络图片。
func (c *Collection) inlineOne(src string, remote bool, cache map[string]string, client *http.Client) (string, bool, bool) {
	if v, ok := cache[src]; ok {
		return v, v != "", strings.HasPrefix(strings.ToLower(src), "http")
	}
	if strings.HasPrefix(src, DocAssetURLPrefix) {
		hash := strings.TrimPrefix(src, DocAssetURLPrefix)
		if len(hash) < 64 || !ValidAssetHash(hash[:64]) {
			cache[src] = ""
			return "", false, false
		}
		data, mime, err := c.ReadDocAsset(hash[:64])
		if err != nil || len(data) > maxInlineImageBytes {
			cache[src] = ""
			return "", false, false
		}
		url := dataURLOf(mime, data)
		cache[src] = url
		return url, true, false
	}
	lower := strings.ToLower(src)
	isHTTP := strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
	if !isHTTP {
		return "", false, false
	}
	if !remote {
		cache[src] = ""
		return "", false, true
	}
	data, mime, err := fetchImage(client, src)
	if err != nil || len(data) > maxInlineImageBytes {
		cache[src] = ""
		return "", false, true
	}
	url := dataURLOf(mime, data)
	cache[src] = url
	return url, true, true
}

// dataURLOf 拼 data URL；MIME 用嗅探结果而不是响应头（响应头可能缺失或写错）。
func dataURLOf(mime string, data []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// fetchImage 下载网络图片并做类型/大小校验。
func fetchImage(client *http.Client, src string) ([]byte, string, error) {
	resp, err := client.Get(src)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if ct := strings.ToLower(resp.Header.Get("Content-Type")); ct != "" && !strings.HasPrefix(ct, "image/") {
		return nil, "", fmt.Errorf("不是图片：%s", ct)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxInlineImageBytes)+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxInlineImageBytes {
		return nil, "", fmt.Errorf("图片超过 %d MB", maxInlineImageBytes>>20)
	}
	mime := SniffImageMime(data)
	if mime == "" {
		return nil, "", fmt.Errorf("无法识别的图片格式")
	}
	return data, mime, nil
}
