package collection

// 文档图片（docimage.go）的单测：类型嗅探、hash 校验（含路径穿越）、落盘去重与读取。

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 1×1 透明 PNG（68 字节，合法 CRC）：测试与前端 e2e 共用同一张图。
const testPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVR4nGNgAAIAAAUAAXpeqz8AAAAASUVORK5CYII="

func testPNG(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(testPNGBase64)
	if err != nil {
		t.Fatalf("测试 PNG 解码失败: %v", err)
	}
	return data
}

func TestSniffImageMime(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"PNG", testPNG(t), "image/png"},
		{"JPEG", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00}, "image/jpeg"},
		{"GIF87a", []byte("GIF87a...."), "image/gif"},
		{"GIF89a", []byte("GIF89a...."), "image/gif"},
		{"WebP", []byte("RIFF\x24\x00\x00\x00WEBPVP8 "), "image/webp"},
		{"SVG 不收", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), ""},
		{"HTML 改名", []byte("<html></html>"), ""},
		{"空", nil, ""},
	}
	for _, c := range cases {
		if got := SniffImageMime(c.data); got != c.want {
			t.Errorf("%s: SniffImageMime=%q，期望 %q", c.name, got, c.want)
		}
	}
}

func TestValidAssetHash(t *testing.T) {
	ok := strings.Repeat("a1", 32)
	if !ValidAssetHash(ok) {
		t.Errorf("64 位 hex 应合法: %q", ok)
	}
	for _, bad := range []string{
		"", "abc", strings.Repeat("A", 64), strings.Repeat("g", 64),
		"../../etc/passwd", ok + "/../x", ok + "\x00",
	} {
		if ValidAssetHash(bad) {
			t.Errorf("应判非法: %q", bad)
		}
	}
}

func TestSaveDocImageAndRead(t *testing.T) {
	c := newTestCollection(t)
	data := testPNG(t)
	hash, err := c.SaveDocImage(data)
	if err != nil {
		t.Fatalf("保存图片失败: %v", err)
	}
	if !ValidAssetHash(hash) {
		t.Fatalf("返回的 hash 非法: %q", hash)
	}
	// 落盘位置：assets/<sha256>（与 R9 离线附件同目录）
	if _, err := os.Stat(filepath.Join(c.Dir, "assets", hash)); err != nil {
		t.Fatalf("未落盘到 assets/: %v", err)
	}
	// 同内容再次保存应去重（同一 hash、文件仍只有一份）
	again, err := c.SaveDocImage(data)
	if err != nil || again != hash {
		t.Fatalf("同图应去重: hash=%q err=%v", again, err)
	}
	// 读回：内容与类型一致
	got, mime, err := c.ReadDocAsset(hash)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if mime != "image/png" || string(got) != string(data) {
		t.Errorf("读回内容/类型不符: mime=%q len=%d", mime, len(got))
	}
	if url := DocAssetURL(hash); url != DocAssetURLPrefix+hash {
		t.Errorf("URL 生成不符: %q", url)
	}
}

func TestSaveDocImageRejects(t *testing.T) {
	c := newTestCollection(t)
	if _, err := c.SaveDocImage(nil); err == nil || !strings.Contains(err.Error(), "为空") {
		t.Errorf("空内容应报错: %v", err)
	}
	if _, err := c.SaveDocImage([]byte("<svg/>")); err == nil || !strings.Contains(err.Error(), "不支持") {
		t.Errorf("SVG 应被拒绝: %v", err)
	}
	big := make([]byte, MaxDocImageBytes+1)
	copy(big, testPNG(t))
	if _, err := c.SaveDocImage(big); err == nil || !strings.Contains(err.Error(), "过大") {
		t.Errorf("超限应报错: %v", err)
	}
}

func TestReadDocAssetGuards(t *testing.T) {
	c := newTestCollection(t)
	// 非法 hash（穿越）一律按不存在处理
	if _, _, err := c.ReadDocAsset("../../etc/passwd"); !os.IsNotExist(err) {
		t.Errorf("穿越路径应报不存在: %v", err)
	}
	// 合法 hash 但文件不存在
	if _, _, err := c.ReadDocAsset(strings.Repeat("0", 64)); !os.IsNotExist(err) {
		t.Errorf("缺文件应报不存在: %v", err)
	}
	// assets/ 下存在同名文件但不是图片：按不存在处理（不把任意文件当图片发出去）
	hash := strings.Repeat("b", 64)
	if err := os.MkdirAll(filepath.Join(c.Dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Dir, "assets", hash), []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.ReadDocAsset(hash); !os.IsNotExist(err) {
		t.Errorf("非图片文件应拒绝: %v", err)
	}
}

// DocImageURLs 要覆盖 Markdown 的各种写法（含 <url> 与 "title"）和 <img src>，
// 否则同步/离线镜像会漏掉这些图片。
func TestDocImageURLsForms(t *testing.T) {
	content := strings.Join([]string{
		"![a](/uploads/a.png)",
		"![b](<https://cdn.example.com/b.png>)",
		"![c](/uploads/c.png \"标题\")",
		`<img src="/api/file/abc.png">`,
		`<img alt="x" src='https://cdn.example.com/d.png'>`,
		"![plain](relative/e.png)",
	}, "\n")
	got := DocImageURLs(content)
	want := []string{
		"/uploads/a.png",
		"https://cdn.example.com/b.png",
		"/uploads/c.png",
		"/api/file/abc.png",
		"https://cdn.example.com/d.png",
		"relative/e.png",
	}
	if len(got) != len(want) {
		t.Fatalf("URL 数量 = %d，期望 %d：%#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 个 URL = %q，期望 %q", i+1, got[i], want[i])
		}
	}
}
