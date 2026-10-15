package app

import (
	"errors"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/varx"
)

// 环境变量（环境 = 一组变量 + 可选 secret 旁路文件）的读写与并发保护。

// ListEnvs 列出活动集合的全部环境。
func (a *App) ListEnvs() ([]collection.Env, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	return c.ListEnvs()
}

// SaveEnvChecked 保存环境；expectHash 非空时校验磁盘内容是否仍为该哈希（并发写保护）。
// 供 MCP 的 set_env_var / rename_env / delete_env_var 使用：这些操作是「读整份 → 改一处 → 整份写回」，
// 不校验就会静默抹掉客户端界面或另一个 AI 会话在此期间的改动。
func (a *App) SaveEnvChecked(env *collection.Env, expectHash string) error {
	if env == nil {
		return errors.New("环境为空")
	}
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.SaveEnvChecked(*env, expectHash)
}

// RenameEnv 给环境改名（两个文件一起搬，旧文件进 .trash）；expectHash 非空时先校验旧文件是否仍为该哈希。
// 供 MCP 的 rename_env 使用：并发校验必须落在旧名文件上，所以这层不能由调用方拼「写新+删旧」。
func (a *App) RenameEnv(oldName, newName, expectHash string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.RenameEnv(oldName, newName, expectHash)
}

// EnvFileHash 环境主文件的内容哈希（不存在返回空串；并发写保护用）。
func (a *App) EnvFileHash(name string) (string, error) {
	c, err := a.requireCollection()
	if err != nil {
		return "", err
	}
	return c.EnvFileHash(name), nil
}

// SaveEnv 整份写回环境（不做并发校验；界面自身的保存路径用）。
func (a *App) SaveEnv(env *collection.Env) error {
	if env == nil {
		return errors.New("环境为空")
	}
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.SaveEnv(*env)
}

// DeleteEnv 删除环境（进 .trash）。
func (a *App) DeleteEnv(name string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.DeleteEnv(name)
}

// envVars 取指定环境的变量表（含 secret 合并与内置动态变量）。
// 未选择环境时只给内置动态变量：不再把全部环境混在一起，避免意外命中其它环境的值。
func (a *App) envVars(envName string) (map[string]string, error) {
	vars := map[string]string{}
	if envName != "" {
		c, err := a.requireCollection()
		if err != nil {
			return nil, err
		}
		envs, err := c.ListEnvs()
		if err != nil {
			return nil, err
		}
		for _, e := range envs {
			if e.Name != envName {
				continue
			}
			for _, v := range e.Vars {
				if v.Enabled && v.Name != "" {
					vars[v.Name] = v.Value
				}
			}
		}
	}
	for k, v := range varx.Builtins() {
		vars[k] = v
	}
	return vars, nil
}
