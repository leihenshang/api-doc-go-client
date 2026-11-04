package app

// 客户端自更新：检查 GitHub Release → 下载 deb → 交给系统安装器。
//
// 为什么不让客户端自己静默安装：deb 落到 /usr 需要 root，静默提权在任何发行版都不被接受。
// 因此流程做成「检测 → 下载到 ~/Downloads → xdg-open 交给软件中心/apt 确认」，
// 用户点一下确认即可，既安全又不打断工作。

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// releaseRepo 发布仓库（与 scripts/release.sh、CI 一致）。
const releaseRepo = "leihenshang/api-doc-go-client"

// updateAPIBase 可在测试里替换为 httptest 地址（生产固定 GitHub）。
var updateAPIBase = "https://api.github.com"

// UpdateInfo 一次检查更新的结果。
type UpdateInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	URL       string `json:"url"`      // Release 页面（给「查看详情」用）
	Notes     string `json:"notes"`    // 更新说明（Release body 前若干行）
	AssetURL  string `json:"assetUrl"` // 正式版 deb 直链（可能为空）
	AssetName string `json:"assetName"`
}

// CheckUpdate 查询最新发布版并与 current 比较（current 由前端传入，保证与界面显示一致）。
func (a *App) CheckUpdate(current string) (*UpdateInfo, error) {
	cur := strings.TrimPrefix(strings.TrimSpace(current), "v")
	req, err := http.NewRequest(http.MethodGet, updateAPIBase+"/repos/"+releaseRepo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "api-doc-go-client")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("检查更新失败（网络不可达）：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return &UpdateInfo{Current: cur}, nil // 还没有任何 Release：当作已是最新
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("检查更新失败（HTTP %d）", resp.StatusCode)
	}
	var rel struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Body    string `json:"body"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return nil, fmt.Errorf("更新信息解析失败：%w", err)
	}
	latest := strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
	info := &UpdateInfo{
		Current: cur, Latest: latest, URL: rel.HTMLURL,
		Available: compareVer(latest, cur) > 0,
		Notes:     clipLines(rel.Body, 12),
	}
	// 优先正式版 amd64 deb（排除 dev 包），拿不到就留空，前端只显示「查看详情」。
	for _, as := range rel.Assets {
		if strings.HasSuffix(as.Name, ".deb") && strings.Contains(as.Name, "amd64") &&
			!strings.Contains(as.Name, "-dev") && !strings.Contains(as.Name, "dev_") {
			info.AssetURL, info.AssetName = as.URL, as.Name
			break
		}
	}
	return info, nil
}

// DownloadUpdate 下载安装包到 ~/Downloads，返回本地路径。
func (a *App) DownloadUpdate(assetURL, name string) (string, error) {
	if strings.TrimSpace(assetURL) == "" {
		return "", fmt.Errorf("这条发布没有可用的 deb 包")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if name == "" || strings.ContainsAny(name, `/\`) {
		name = "api-doc-client-update.deb"
	}
	dst := filepath.Join(dir, name)
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(assetURL)
	if err != nil {
		return "", fmt.Errorf("下载失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载失败（HTTP %d）", resp.StatusCode)
	}
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return dst, os.Rename(tmp, dst)
}

// InstallUpdate 用系统安装器打开下载好的 deb（deb 需要 root，由系统弹确认）。
func (a *App) InstallUpdate(path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("安装包不存在：%s", path)
	}
	// 优先 xdg-open（走软件中心/包管理器 GUI），失败再退回终端提示。
	if err := exec.Command("xdg-open", path).Start(); err == nil {
		return nil
	}
	return fmt.Errorf("请手动执行：sudo apt install %s", path)
}

// compareVer 比较 x.y.z（缺位按 0 处理）：返回 -1/0/1。
func compareVer(a, b string) int {
	pa, pb := verParts(a), verParts(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// verParts 取前三段数字（忽略 -beta 之类后缀）。
func verParts(v string) [3]int {
	var out [3]int
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	for i, s := range strings.Split(v, ".") {
		if i >= 3 {
			break
		}
		n, _ := strconv.Atoi(strings.TrimSpace(s))
		out[i] = n
	}
	return out
}

// clipLines 截取更新说明的前 n 行，避免弹窗被超长 Release body 撑爆。
func clipLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = append(lines[:n], "…")
	}
	return strings.Join(lines, "\n")
}
