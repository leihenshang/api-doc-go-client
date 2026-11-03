package syncengine

// R2：全局变量（服务端 scope=3，跨项目）下发到客户端后落到集合根 globals.yml，
// 且不会混进环境文件（它不属于任何环境）。

import "testing"

func TestGlobalVariableAppliedToGlobalsFile(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 10}
	f.snapshot = Snapshot{Cursor: 10, Items: []Item{
		{Type: "variable", UID: "gv1", Op: "upsert", ItemRev: 1, Payload: payloadOf(t, Payload{
			Name: "global_host", Value: "https://api.example.com", Scope: 3,
		})},
	}}
	c, e := newBoundEngine(t, cli)

	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	if len(rep.Errors) != 0 {
		t.Fatalf("应用全局变量不应报错: %+v", rep.Errors)
	}
	globals, err := c.ListGlobals()
	if err != nil {
		t.Fatalf("ListGlobals: %v", err)
	}
	if len(globals) != 1 {
		t.Fatalf("应落 1 条全局变量，got %+v", globals)
	}
	if globals[0].UID != "gv1" || globals[0].Name != "global_host" || globals[0].Value != "https://api.example.com" {
		t.Fatalf("全局变量内容不符: %+v", globals[0])
	}

	// 全局变量不属于任何环境：环境列表里不能出现它。
	envs, err := c.ListEnvs()
	if err != nil {
		t.Fatalf("ListEnvs: %v", err)
	}
	for _, env := range envs {
		for _, v := range env.Vars {
			if v.Name == "global_host" {
				t.Fatalf("全局变量不应混进环境 %q", env.Name)
			}
		}
	}
}
