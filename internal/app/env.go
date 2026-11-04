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
// 未选择环境时只给内置动态变量与全局变量：不再把全部环境混在一起，避免意外命中其它环境的值。
//
// 全局变量（服务端 scope=3、跨项目共享）由同步下发到集合根 globals.yml，
// 优先级最低：先放，再被当前环境的同名变量覆盖。
func (a *App) envVars(envName string) (map[string]string, error) {
	vars := map[string]string{}
	// 全局变量：读不到集合或文件时静默跳过（没有集合时也要能用内置变量）。
	if c, cerr := a.requireCollection(); cerr == nil {
		if globals, gerr := c.ListGlobals(); gerr == nil {
			for _, v := range globals {
				if v.Enabled && v.Name != "" {
					vars[v.Name] = v.Value
				}
			}
		}
	}
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

// ListGlobals 集合内缓存的全局变量（服务端 scope=3 的只读镜像）。
//
// 谁写它：同步引擎（syncengine/globalvar.go）；谁读它：变量面板只读展示。
// 客户端不提供编辑入口 —— 全局变量由服务端管理员维护，本地改了也会被下一轮同步覆盖。
func (a *App) ListGlobals() ([]collection.Var, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	return c.ListGlobals()
}
