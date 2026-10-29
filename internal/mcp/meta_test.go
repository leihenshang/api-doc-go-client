package mcp

import (
	"reflect"
	"testing"
)

// TestValidateMetas 元数据表完整性：name 唯一、分组合法、Brief/描述齐备、风险自洽。
func TestValidateMetas(t *testing.T) {
	if err := ValidateMetas(); err != nil {
		t.Fatalf("元数据自检失败: %v", err)
	}
}

// TestToolNamesStable 锁定对外工具名基线（改名/增删 = 破坏 MCP 契约，必须显式更新这里）。
func TestToolNamesStable(t *testing.T) {
	want := []string{
		"list_tools", "list_workspaces", "use_workspace", "list_projects",
		"get_project_modules", "get_request_detail", "search_requests",
		"search_docs", "list_docs", "get_doc", "list_envs",
		"create_request", "import_collection", "create_module", "create_project",
		"update_request", "delete_request", "delete_requests",
		"rename_module", "delete_module", "rename_project", "delete_project",
		"create_doc", "update_doc", "delete_doc", "save_doc_image",
		"save_response_example", "update_response_example", "delete_response_example",
		"create_env", "rename_env", "delete_env", "set_env_var", "delete_env_var",
		"send_request",
	}
	got := ToolNames()
	if len(got) != 35 {
		t.Fatalf("正式工具数 = %d，期望 35", len(got))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("工具名基线漂移：\n got=%v\nwant=%v", got, want)
	}
}

// TestRiskClassification 风险三档与正交标记：
// 11 读 = low；send_request = medium+NetEgress；23 个落盘写 = high+Write（集合 18 + 环境 5）。
func TestRiskClassification(t *testing.T) {
	readers := []string{
		"list_tools", "list_workspaces", "use_workspace", "list_projects",
		"get_project_modules", "get_request_detail", "search_requests",
		"search_docs", "list_docs", "get_doc", "list_envs",
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
		"create_request", "import_collection", "create_module", "create_project",
		"update_request", "delete_request", "delete_requests",
		"rename_module", "delete_module", "rename_project", "delete_project",
		"create_doc", "update_doc", "delete_doc", "save_doc_image",
		"save_response_example", "update_response_example", "delete_response_example",
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

// TestNamesForMode 只读模式 = 11 读 + send_request 共 12 个；全模式 35 个。
func TestNamesForMode(t *testing.T) {
	if got := NamesForMode(false, nil); len(got) != 35 {
		t.Errorf("全模式工具数 = %d，期望 35", len(got))
	}
	ro := NamesForMode(true, nil)
	if len(ro) != 12 {
		t.Fatalf("只读模式工具数 = %d，期望 12：%v", len(ro), ro)
	}
	roSet := map[string]bool{}
	for _, n := range ro {
		roSet[n] = true
	}
	for _, name := range append([]string{"send_request"},
		"list_tools", "list_workspaces", "use_workspace", "list_projects",
		"get_project_modules", "get_request_detail", "search_requests",
		"search_docs", "list_docs", "get_doc", "list_envs") {
		if !roSet[name] {
			t.Errorf("只读模式应注册 %s", name)
		}
	}
	if roSet["create_request"] || roSet["create_env"] {
		t.Errorf("只读模式不应注册写工具：%v", ro)
	}
}

// TestGroupFiltering 工具组过滤：session 恒开、空组=全开、未知组被忽略、顺序稳定。
func TestGroupFiltering(t *testing.T) {
	docsOnly := NamesForMode(false, []string{"docs"})
	// 4 个 session + 7 个 docs
	if len(docsOnly) != 11 {
		t.Fatalf("docs 组工具数 = %d，期望 11：%v", len(docsOnly), docsOnly)
	}
	set := map[string]bool{}
	for _, n := range docsOnly {
		set[n] = true
	}
	for _, n := range []string{
		"list_tools", "list_workspaces", "use_workspace", "list_projects",
		"search_docs", "list_docs", "get_doc", "create_doc", "update_doc", "delete_doc", "save_doc_image",
	} {
		if !set[n] {
			t.Errorf("docs 组应包含 %s", n)
		}
	}
	if set["send_request"] || set["create_env"] || set["create_request"] {
		t.Errorf("docs 组不应包含其它组工具：%v", docsOnly)
	}

	got := NormalizeGroups([]string{" DOCS ", "unknown", "docs", "session"})
	if !reflect.DeepEqual(got, []string{"docs"}) {
		t.Errorf("NormalizeGroups 结果 = %v，期望 [docs]", got)
	}
	if all := NamesForMode(false, nil); len(all) != 35 {
		t.Errorf("空组应等价全开：%d", len(all))
	}
}

// TestMetaOfUnknown 查不存在的工具返回 false。
func TestMetaOfUnknown(t *testing.T) {
	if m, ok := MetaOf("not_a_tool"); ok {
		t.Errorf("不存在的工具应返回 false，得到 %+v", m)
	}
}

// TestToolDescriptionBudget 描述体积回归：单条 ≤ 320 字、总量有上限，
// 防止后续继续把「一屏说明书」塞回工具描述（会话一开始的固定上下文成本）。
func TestToolDescriptionBudget(t *testing.T) {
	total := 0
	for _, m := range toolMetas {
		n := len([]rune(m.Description))
		total += n
		if n > 320 {
			t.Errorf("%s 描述 %d 字，超过单条上限 320", m.Name, n)
		}
	}
	t.Logf("工具描述总字符 = %d", total)
	if total > 3200 {
		t.Errorf("工具描述总字符 %d 超过预算 3200", total)
	}
}
