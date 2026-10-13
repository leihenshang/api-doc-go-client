package mcp

import (
	"reflect"
	"testing"
)

// TestValidateMetas 元数据表完整性：18 项、name 唯一、risk 合法、无别名正式项。
func TestValidateMetas(t *testing.T) {
	if err := ValidateMetas(); err != nil {
		t.Fatalf("元数据自检失败: %v", err)
	}
}

// TestToolNamesStable 锁定 18 个对外工具名基线（改名 = 破坏 MCP 契约，测试必须红）。
func TestToolNamesStable(t *testing.T) {
	want := []string{
		"list_workspaces", "use_workspace", "list_projects", "get_project_modules",
		"get_request_detail", "search_requests", "list_envs",
		"create_request", "create_module", "create_project", "update_request", "delete_request",
		"create_env", "rename_env", "delete_env", "set_env_var", "delete_env_var",
		"send_request",
	}
	got := ToolNames()
	if len(got) != 18 {
		t.Fatalf("正式工具数 = %d，期望 18", len(got))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("工具名基线漂移：\n got=%v\nwant=%v", got, want)
	}
}

// TestRiskClassification 风险三档与正交标记：
// 7 读=low；send_request=medium+NetEgress；10 个落盘写=high+Write；5 个环境工具 EnvWrite。
func TestRiskClassification(t *testing.T) {
	readers := []string{
		"list_workspaces", "use_workspace", "list_projects", "get_project_modules",
		"get_request_detail", "search_requests", "list_envs",
	}
	for _, name := range readers {
		m, ok := MetaOf(name)
		if !ok {
			t.Fatalf("缺少元数据 %s", name)
		}
		if m.Risk != RiskLow || m.Write || m.NetEgress || m.EnvWrite || !m.InReadOnly {
			t.Errorf("%s 应为 low/无副作用/只读模式注册，实际 %+v", name, m)
		}
		if m.NeedsConsent() {
			t.Errorf("%s 不应需要交互确认", name)
		}
	}

	send, ok := MetaOf("send_request")
	if !ok {
		t.Fatal("缺少 send_request 元数据")
	}
	if send.Risk != RiskMedium || !send.NetEgress || send.Write || send.EnvWrite || !send.InReadOnly {
		t.Errorf("send_request 应为 medium+NetEgress+InReadOnly：%+v", send)
	}
	if !send.NeedsConsent() || send.Mutable() {
		t.Errorf("send_request 应需确认但不落盘：NeedsConsent=%v Mutable=%v", send.NeedsConsent(), send.Mutable())
	}

	writers := []string{
		"create_request", "create_module", "create_project", "update_request", "delete_request",
		"create_env", "rename_env", "delete_env", "set_env_var", "delete_env_var",
	}
	envWriters := map[string]bool{
		"create_env": true, "rename_env": true, "delete_env": true,
		"set_env_var": true, "delete_env_var": true,
	}
	for _, name := range writers {
		m, ok := MetaOf(name)
		if !ok {
			t.Fatalf("缺少元数据 %s", name)
		}
		if m.Risk != RiskHigh || !m.Write || m.NetEgress || m.InReadOnly {
			t.Errorf("%s 应为 high+Write+只读模式不注册：%+v", name, m)
		}
		if m.EnvWrite != envWriters[name] {
			t.Errorf("%s EnvWrite 期望 %v，实际 %v", name, envWriters[name], m.EnvWrite)
		}
		if !m.NeedsConsent() || !m.Mutable() {
			t.Errorf("%s 应需确认且为落盘写", name)
		}
	}
}

// TestNamesForMode 只读模式 = 7 读 + send_request 共 8 个；全模式 18 个。
func TestNamesForMode(t *testing.T) {
	if got := NamesForMode(false); len(got) != 18 {
		t.Errorf("全模式工具数 = %d，期望 18", len(got))
	}
	ro := NamesForMode(true)
	if len(ro) != 8 {
		t.Fatalf("只读模式工具数 = %d，期望 8：%v", len(ro), ro)
	}
	roSet := map[string]bool{}
	for _, n := range ro {
		roSet[n] = true
	}
	for _, name := range append([]string{"send_request"},
		"list_workspaces", "use_workspace", "list_projects", "get_project_modules",
		"get_request_detail", "search_requests", "list_envs") {
		if !roSet[name] {
			t.Errorf("只读模式应注册 %s", name)
		}
	}
}

// TestMetaOfUnknown 查不存在的工具返回 false。
func TestMetaOfUnknown(t *testing.T) {
	if m, ok := MetaOf("not_a_tool"); ok {
		t.Errorf("不存在的工具应返回 false，得到 %+v", m)
	}
}
