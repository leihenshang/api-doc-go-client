package mcp

import (
	"strings"
	"testing"
)

// TestRenderEnvsShowsMaskAndSorted 直接测渲染函数：它在内部包（不导出的实现细节）。
// 输出会进 AI 的上下文，排序与掩码标注错了会直接误导调用方。
func TestRenderEnvsShowsMaskAndSorted(t *testing.T) {
	envs := []EnvEntry{
		{Name: "dev", Default: true, Vars: map[string]EnvVarEntry{
			"zeta":  {Name: "zeta", Value: "z"},
			"alpha": {Name: "alpha", Value: maskedValue, Secret: true},
			"off":   {Name: "off", Value: "x", Enabled: false},
		}},
		{Name: "prod", Vars: map[string]EnvVarEntry{}},
	}
	out := renderEnvs("demo", envs)
	for _, want := range []string{"dev", "prod", "默认", maskedValue, "敏感值", "已停用", "空环境"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺少 %q:\n%s", want, out)
		}
	}
	// 变量按名字排序（alpha < off < zeta），否则 AI 每次看到的顺序都不同
	ia, io, iz := strings.Index(out, "alpha"), strings.Index(out, "off"), strings.Index(out, "zeta")
	if !(ia < io && io < iz) {
		t.Errorf("变量未按名字排序:\n%s", out)
	}
	// 空集合也要给得出可读结论
	if s := renderEnvs("demo", nil); !strings.Contains(s, "create_env") {
		t.Errorf("空列表应提示怎么建环境:\n%s", s)
	}
}
