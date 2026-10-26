// Package patvault 对集合内 .sync.json 的 PAT 做本地静态加密。
//
// 密钥保存在用户配置目录（pat_vault.key，0600）而不是集合目录：
// 集合被拷贝、打包或误提交时，.sync.json 里只有 AES-256-GCM 密文，
// 不会把长期访问令牌带出去；换机/删掉配置目录后旧密文不可解，重新填写 PAT 即可。
//
// 取舍：不依赖系统密钥环（Linux 无 DBus / CI 不可用、且测试易污染真实钥匙串），
// 属于「防误泄露」而非「防本机同一用户读取」——同用户下的恶意程序仍可读密钥文件。
package patvault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"api-doc-go-client/internal/config"
)

// prefix 标识密文格式与版本；只有本包生成的密文才带该前缀。
const prefix = "pat:v1:"

const (
	keyLen   = 32 // AES-256
	nonceLen = 12 // GCM 标准 nonce
	keyFile  = "pat_vault.key"
)

var (
	mu         sync.Mutex
	cachedKey  []byte
	cachedPath string
)

// IsEncrypted 判断值是否为本包生成的密文。
func IsEncrypted(v string) bool { return strings.HasPrefix(v, prefix) }

// ResetCache 清空内存中的密钥缓存（仅供测试：切换配置目录后重新加载）。
func ResetCache() {
	mu.Lock()
	cachedKey = nil
	cachedPath = ""
	mu.Unlock()
}

// keyPath 本机 PAT 加密密钥文件（与 config.json 同目录）。
func keyPath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, keyFile), nil
}

// loadKey 读取或生成 32 字节随机密钥。
// 密钥文件损坏时视同丢失：重新生成（旧密文本就不可解，用户重新填 PAT 即可）。
func loadKey() ([]byte, error) {
	path, err := keyPath()
	if err != nil {
		return nil, err
	}
	mu.Lock()
	defer mu.Unlock()
	if cachedKey != nil && cachedPath == path {
		return cachedKey, nil
	}

	if data, err := os.ReadFile(path); err == nil {
		raw, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if derr == nil && len(raw) == keyLen {
			cachedKey, cachedPath = raw, path
			return raw, nil
		}
		// 损坏：落回下面重新生成，避免用户填了新 PAT 却始终保存失败
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取 PAT 密钥: %w", err)
	}

	key := make([]byte, keyLen)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("生成 PAT 密钥: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("创建配置目录: %w", err)
	}
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(key)), 0o600); err != nil {
		return nil, fmt.Errorf("写入 PAT 密钥: %w", err)
	}
	cachedKey, cachedPath = key, path
	return key, nil
}

// Seal 加密 PAT，返回 pat:v1:<base64(nonce‖ciphertext)>。
func Seal(plaintext string) (string, error) {
	key, err := loadKey()
	if err != nil {
		return "", err
	}
	aead, err := newAEAD(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := aead.Seal(nil, nonce, []byte(plaintext), nil)
	buf := make([]byte, 0, nonceLen+len(ct))
	buf = append(buf, nonce...)
	buf = append(buf, ct...)
	return prefix + base64.StdEncoding.EncodeToString(buf), nil
}

// Open 解密密文；非本包格式、密钥不匹配或内容被篡改均返回错误。
func Open(blob string) (string, error) {
	if !IsEncrypted(blob) {
		return "", fmt.Errorf("不是 patvault 密文")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(blob, prefix))
	if err != nil {
		return "", fmt.Errorf("解密 PAT: %w", err)
	}
	if len(raw) < nonceLen {
		return "", fmt.Errorf("PAT 密文长度不足")
	}
	key, err := loadKey()
	if err != nil {
		return "", err
	}
	aead, err := newAEAD(key)
	if err != nil {
		return "", err
	}
	plain, err := aead.Open(nil, raw[:nonceLen], raw[nonceLen:], nil)
	if err != nil {
		return "", fmt.Errorf("解密 PAT: %w", err)
	}
	return string(plain), nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
