package collection

// .gitignore 补齐逻辑（P0-5 回归）：已有 .gitignore 的用户目录，客户端以前完全不管，
// 于是明文存 PAT 的 .sync.json 会被 commit 进版本库。这里锁住「只追加、不重写、幂等、
// 保持换行风格」四条性质。

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// readGitignoreOrFail 读集合目录下的 .gitignore 文本。
func readGitignoreOrFail(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("读取 .gitignore: %v", err)
	}
	return string(data)
}

// hasGitignoreLine 判断文本里是否有某一整行（trim 后比较）。
func hasGitignoreLine(text, want string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// TestEnsureGitignoreCreatesFile 空目录：创建文件，内容与 gitignoreContent 完全一致。
func TestEnsureGitignoreCreatesFile(t *testing.T) {
	dir := t.TempDir()
	if err := ensureGitignore(dir); err != nil {
		t.Fatalf("ensureGitignore: %v", err)
	}
	if got := readGitignoreOrFail(t, dir); got != gitignoreContent {
		t.Fatalf(".gitignore 内容应为 gitignoreContent，got %q", got)
	}
}

// TestEnsureGitignoreAppendsMissingEntries 已有用户内容：用户行原样保留，缺失条目被追加，
// 已存在的条目不重复追加。
func TestEnsureGitignoreAppendsMissingEntries(t *testing.T) {
	dir := t.TempDir()
	// 用户自己的 .gitignore：既有自定义行，也有 .sync.json（不该再被追加一次）
	user := "dist/\n*.log\n.sync.json\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(user), 0o644); err != nil {
		t.Fatalf("写初始 .gitignore: %v", err)
	}
	if err := ensureGitignore(dir); err != nil {
		t.Fatalf("ensureGitignore: %v", err)
	}
	got := readGitignoreOrFail(t, dir)
	// 用户内容必须原样保留在开头（顺序不变、不改写）
	if !strings.HasPrefix(got, user) {
		t.Fatalf("用户已有内容必须原样保留，got %q", got)
	}
	// 全部条目都在（含 .sync.json），且没有重复行
	for _, entry := range gitignoreEntries {
		if !hasGitignoreLine(got, entry) {
			t.Errorf("缺少条目 %q，got %q", entry, got)
		}
	}
	counts := map[string]int{}
	for _, line := range strings.Split(got, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			counts[s]++
		}
	}
	for line, n := range counts {
		if n > 1 {
			t.Errorf("行 %q 出现了 %d 次，不应有重复", line, n)
		}
	}
	if n := strings.Count(got, gitignoreBanner); n != 1 {
		t.Errorf("自动维护标识行应只出现 1 次，got %d 次（%q）", n, got)
	}
}

// TestEnsureGitignoreIdempotent 连续两次调用：第二次内容一个字节都不变。
func TestEnsureGitignoreIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("dist/\n"), 0o644); err != nil {
		t.Fatalf("写初始 .gitignore: %v", err)
	}
	if err := ensureGitignore(dir); err != nil {
		t.Fatalf("第一次 ensureGitignore: %v", err)
	}
	first := readGitignoreOrFail(t, dir)
	if err := ensureGitignore(dir); err != nil {
		t.Fatalf("第二次 ensureGitignore: %v", err)
	}
	if second := readGitignoreOrFail(t, dir); second != first {
		t.Fatalf("重复调用应幂等：\n第一次 %q\n第二次 %q", first, second)
	}
}

// TestEnsureGitignoreKeepsCRLF CRLF 文件：追加的行也用 CRLF，全文不混入裸 LF。
func TestEnsureGitignoreKeepsCRLF(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("dist/\r\n*.log\r\n"), 0o644); err != nil {
		t.Fatalf("写初始 .gitignore: %v", err)
	}
	if err := ensureGitignore(dir); err != nil {
		t.Fatalf("ensureGitignore: %v", err)
	}
	got := readGitignoreOrFail(t, dir)
	// 去掉所有 CRLF 后若还残留 '\n'，说明混入了裸 LF
	if stripped := strings.ReplaceAll(got, "\r\n", ""); strings.Contains(stripped, "\n") {
		t.Fatalf("CRLF 文件里混入了裸 LF: %q", got)
	}
	for _, entry := range gitignoreEntries {
		if !strings.Contains(got, entry+"\r\n") {
			t.Errorf("条目 %q 应以 CRLF 结尾，got %q", entry, got)
		}
	}
}

// TestEnsureGitignoreUntouchedWhenComplete 已包含全部条目：内容一个字节都不变。
func TestEnsureGitignoreUntouchedWhenComplete(t *testing.T) {
	dir := t.TempDir()
	// 顺序与默认清单不同、外加用户自定义行与结尾无换行：都不该被「规整」
	orig := "*.tmp\nnode_modules/\n.sync.json\n.sync-deletes.json\n.conflicts/\n.trash/\n*.local.yml\nenvironments/*.secrets.yml\n" + gitignoreBanner
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(orig), 0o644); err != nil {
		t.Fatalf("写初始 .gitignore: %v", err)
	}
	if err := ensureGitignore(dir); err != nil {
		t.Fatalf("ensureGitignore: %v", err)
	}
	if got := readGitignoreOrFail(t, dir); got != orig {
		t.Fatalf("条目齐备时不应改动文件：\n期望 %q\n实际 %q", orig, got)
	}
}

// TestEnsureGitignorePreservesMode 已存在的文件只能原地追加：权限不能被放宽。
// （整份重写 = 新建 inode + rename，mode 会掉回 0644。）
func TestEnsureGitignorePreservesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不支持 POSIX 权限位")
	}
	dir := t.TempDir()
	gi := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gi, []byte("dist/\n"), 0o600); err != nil {
		t.Fatalf("写初始 .gitignore: %v", err)
	}
	if err := ensureGitignore(dir); err != nil {
		t.Fatalf("ensureGitignore: %v", err)
	}
	st, err := os.Stat(gi)
	if err != nil {
		t.Fatalf("stat .gitignore: %v", err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Fatalf("追加后权限应保持 0600，got %04o", perm)
	}
	if !hasGitignoreLine(readGitignoreOrFail(t, dir), ".sync.json") {
		t.Fatal("追加未生效：缺 .sync.json")
	}
}

// TestEnsureGitignoreKeepsSymlink 用户的 .gitignore 是软链时，写入必须穿过链接落到真实文件，
// 不能把链接换成普通文件（那样真实文件里依旧没有 .sync.json）。
func TestEnsureGitignoreKeepsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 建软链需要额外权限")
	}
	dir := t.TempDir()
	target := "a.gitignore"
	if err := os.WriteFile(filepath.Join(dir, target), []byte("dist/\n"), 0o644); err != nil {
		t.Fatalf("写真实文件: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(dir, ".gitignore")); err != nil {
		t.Fatalf("建软链: %v", err)
	}
	if err := ensureGitignore(dir); err != nil {
		t.Fatalf("ensureGitignore: %v", err)
	}
	st, err := os.Lstat(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("lstat .gitignore: %v", err)
	}
	if st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf(".gitignore 应仍是软链，实际 mode=%v", st.Mode())
	}
	data, err := os.ReadFile(filepath.Join(dir, target))
	if err != nil {
		t.Fatalf("读真实文件: %v", err)
	}
	if !hasGitignoreLine(string(data), ".sync.json") {
		t.Fatalf("真实文件里应出现 .sync.json，got %q", string(data))
	}
}

// TestOpenAppendsToExistingGitignore Open 是真实入口：已有 .gitignore（git init / clone 来的
// 仓库）时也必须补上 .sync.json，否则明文 PAT 会被提交。
func TestOpenAppendsToExistingGitignore(t *testing.T) {
	dir := t.TempDir()
	user := "node_modules/\ndist/\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(user), 0o644); err != nil {
		t.Fatalf("写初始 .gitignore: %v", err)
	}
	c, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	got := readGitignoreOrFail(t, dir)
	if !strings.HasPrefix(got, user) {
		t.Fatalf("用户已有内容必须原样保留，got %q", got)
	}
	if !hasGitignoreLine(got, ".sync.json") {
		t.Fatalf("Open 后 .sync.json 必须被忽略（明文 PAT 不能进版本库），got %q", got)
	}
	if !hasGitignoreLine(got, "environments/*.secrets.yml") {
		t.Fatalf("Open 后 secret 环境文件必须被忽略，got %q", got)
	}
}
