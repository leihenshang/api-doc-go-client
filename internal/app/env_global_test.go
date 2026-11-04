package app

// R2：全局变量（服务端下发，落在集合根 globals.yml）参与变量解析，且优先级最低 ——
// 当前环境的同名变量必须覆盖它；不选环境时全局变量依然生效。

import (
	"testing"

	"api-doc-go-client/internal/collection"
)

func TestGlobalVarResolvesAndEnvOverridesIt(t *testing.T) {
	core, _ := newTestApp(t)
	c, err := core.requireCollection()
	if err != nil {
		t.Fatalf("取集合失败: %v", err)
	}
	if err := c.SaveGlobals([]collection.Var{
		{Name: "global_host", Value: "https://global.example.com", Enabled: true},
	}); err != nil {
		t.Fatalf("保存全局变量失败: %v", err)
	}

	// ① 不选环境：全局变量生效。
	vars, err := core.envVars("")
	if err != nil {
		t.Fatalf("envVars: %v", err)
	}
	if vars["global_host"] != "https://global.example.com" {
		t.Fatalf("全局变量未参与解析: %+v", vars)
	}

	// ② 环境里的同名变量覆盖全局变量（优先级：环境 > 全局）。
	if err := core.SaveEnv(&collection.Env{
		Name: "dev",
		Vars: []collection.Var{
			{Name: "global_host", Value: "https://env.example.com", Enabled: true},
		},
	}); err != nil {
		t.Fatalf("保存环境失败: %v", err)
	}
	vars, err = core.envVars("dev")
	if err != nil {
		t.Fatalf("envVars(dev): %v", err)
	}
	if vars["global_host"] != "https://env.example.com" {
		t.Fatalf("环境变量应覆盖全局变量，实际 %q", vars["global_host"])
	}

	// ③ 禁用的全局变量不生效。
	if err := c.SaveGlobals([]collection.Var{
		{Name: "global_host", Value: "https://global.example.com", Enabled: false},
	}); err != nil {
		t.Fatalf("保存全局变量失败: %v", err)
	}
	vars, err = core.envVars("")
	if err != nil {
		t.Fatalf("envVars: %v", err)
	}
	if _, ok := vars["global_host"]; ok {
		t.Fatalf("禁用的全局变量不应参与解析: %+v", vars)
	}
}

// U1：变量面板要能读到全局变量（只读镜像）——门面必须把接口暴露出来。
func TestListGlobalsExposesMirror(t *testing.T) {
	core, _ := newTestApp(t)
	c, err := core.requireCollection()
	if err != nil {
		t.Fatalf("取集合失败: %v", err)
	}
	if err := c.SaveGlobals([]collection.Var{
		{UID: "g1", Name: "global_host", Value: "https://g.example.com", Enabled: true},
		{UID: "g2", Name: "global_token", Value: "secret-value", Enabled: false, Secret: true},
	}); err != nil {
		t.Fatalf("写全局变量失败: %v", err)
	}
	got, err := core.ListGlobals()
	if err != nil {
		t.Fatalf("ListGlobals: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("应返回 2 条，实际 %+v", got)
	}
	// 落盘按名字排序：global_host 在前
	if got[0].Name != "global_host" || got[0].Value != "https://g.example.com" {
		t.Fatalf("条目内容不符: %+v", got[0])
	}
	if !got[1].Secret || got[1].Enabled {
		t.Fatalf("secret/disabled 标记丢失: %+v", got[1])
	}
}
