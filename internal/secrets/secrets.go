// Package secrets 提供客户端「口令派生 → AES-256-GCM」的值加密，用于同步 secret 变量。
//
// 产品约定：secret 值在客户端加密后才离开本机，服务端**只存储密文、不解析/不解密**，
// 并据密文前缀标注为「加密数据、暂不读取」；客户端拉回后用同一口令本地解密还原。
// 因此密文自带可辨识前缀（enc:v1:），非该前缀的值按明文透传（便于普通变量/旧数据共存）。
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"strings"

	"golang.org/x/crypto/scrypt"
)

// prefix 标记密文格式与版本；服务端据此识别「加密数据」而不解析。
const prefix = "enc:v1:"

const (
	saltLen  = 16 // scrypt 盐长度
	nonceLen = 12 // GCM 标准 nonce 长度
	keyLen   = 32 // AES-256
	scryptN  = 1 << 15
	scryptR  = 8
	scryptP  = 1
)

// ErrPassphrase 表示未设置口令却尝试解密密文。
var ErrPassphrase = errors.New("未设置解密口令")

// IsEncrypted 判断值是否为本包生成的密文（带 enc:v1: 前缀）。
func IsEncrypted(v string) bool { return strings.HasPrefix(v, prefix) }

// deriveKey 由口令 + 盐派生 AES-256 密钥（scrypt）。
func deriveKey(passphrase string, salt []byte) ([]byte, error) {
	return scrypt.Key([]byte(passphrase), salt, scryptN, scryptR, scryptP, keyLen)
}

// encryptWith 用给定口令加密明文，返回 enc:v1:<base64(salt‖nonce‖ciphertext)>。
func encryptWith(passphrase, plaintext string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", err
	}
	key, err := deriveKey(passphrase, salt)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := aead.Seal(nil, nonce, []byte(plaintext), nil)
	buf := make([]byte, 0, saltLen+nonceLen+len(ct))
	buf = append(buf, salt...)
	buf = append(buf, nonce...)
	buf = append(buf, ct...)
	return prefix + base64.StdEncoding.EncodeToString(buf), nil
}

// decryptWith 解密 encryptWith 产出的密文；口令错误或格式非法返回错误。
func decryptWith(passphrase, blob string) (string, error) {
	if passphrase == "" {
		return "", ErrPassphrase
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(blob, prefix))
	if err != nil {
		return "", err
	}
	if len(raw) < saltLen+nonceLen {
		return "", errors.New("密文长度不足")
	}
	salt, nonce, ct := raw[:saltLen], raw[saltLen:saltLen+nonceLen], raw[saltLen+nonceLen:]
	key, err := deriveKey(passphrase, salt)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plain, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// Cipher 绑定一个口令，供同步引擎读写 secret 时复用（口令为空时按明文透传/拒绝解密）。
type Cipher struct {
	passphrase string
}

// NewCipher 以口令构造加解密器。
func NewCipher(passphrase string) *Cipher { return &Cipher{passphrase: passphrase} }

// Enabled 报告是否设置了口令。
func (c *Cipher) Enabled() bool { return c != nil && c.passphrase != "" }

// Seal 加密 secret 值；未设口令时返回原值并标记未加密（调用方据此拒绝同步 secret）。
func (c *Cipher) Seal(plaintext string) (string, bool, error) {
	if !c.Enabled() {
		return plaintext, false, nil
	}
	blob, err := encryptWith(c.passphrase, plaintext)
	if err != nil {
		return "", false, err
	}
	return blob, true, nil
}

// Open 还原值：非密文（无 enc:v1: 前缀）原样返回；密文则用口令解密。
func (c *Cipher) Open(value string) (string, error) {
	if !IsEncrypted(value) {
		return value, nil
	}
	return decryptWith(c.passphrase, value)
}
