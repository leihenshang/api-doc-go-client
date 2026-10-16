// Package keychain 封装操作系统密钥环（G6）：当前仅用于存放「同步口令」——
// 解开所有 enc:v1: 密文的唯一钥匙，比散落的 0600 文件更适得其所在。
//
// 无密钥环的环境（Linux 无 DBus / 钥匙串锁定 / CI）会返回 ErrNotAvailable，
// 调用方（syncengine）据此自动回退配置目录 0600 文件的旧方案，行为不劣化。
// 单测可通过 SetHooks 注入内存实现，绝不触碰真实密钥环。
package keychain

import (
	"errors"

	keyring "github.com/zalando/go-keyring"
)

// service 是本应用在此机器上的唯一密钥环命名空间。
const service = "api-doc-go-client"

// ErrNotFound 未命中条目（再导出，避免调用方直接依赖 go-keyring 类型）。
// 「无可用密钥环」不设独立哨兵：go-keyring 各平台错误形态不一，
// 调用方一律以「返回任何 error 即回退文件」处理（见 syncengine.SetSyncPassphrase）。
var ErrNotFound = keyring.ErrNotFound

// ErrNotAvailable 预留给需要显式判别的调用方（当前以普通错误返回）。
var ErrNotAvailable = errors.New("no OS keychain available")

var (
	setHook = func(user, value string) error { return keyring.Set(service, user, value) }
	getHook = func(user string) (string, error) { return keyring.Get(service, user) }
	delHook = func(user string) error { return keyring.Delete(service, user) }
)

// SetHooks 注入实现（仅供测试）；传 nil 保持原实现不变。
func SetHooks(set func(string, string) error, get func(string) (string, error), del func(string) error) {
	if set != nil {
		setHook = set
	}
	if get != nil {
		getHook = get
	}
	if del != nil {
		delHook = del
	}
}

// ResetHooks 恢复真实密钥环实现（配合 SetHooks 在测试 Cleanup 中调用）。
func ResetHooks() {
	setHook = func(user, value string) error { return keyring.Set(service, user, value) }
	getHook = func(user string) (string, error) { return keyring.Get(service, user) }
	delHook = func(user string) error { return keyring.Delete(service, user) }
}

// Set 写入条目（同 user 覆盖）。
func Set(user, value string) error { return setHook(user, value) }

// Get 读取条目：命中返回 (值, nil)；未命中返回 ErrNotFound；无密钥环返回 ErrNotAvailable。
func Get(user string) (string, error) { return getHook(user) }

// Delete 删除条目（不存在返回 ErrNotFound，调用方可忽略）。
func Delete(user string) error { return delHook(user) }
