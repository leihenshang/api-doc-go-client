// 端到端同步实测脚本（临时，验证后删除）。
// 流程：建集合 → 绑定 → push → 另一"设备" pull → 双向改动 → 冲突。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/syncengine"
)

func main() {
	serverURL := "http://127.0.0.1:2027"
	token := os.Getenv("PAT")
	pidStr := os.Getenv("PID")
	if pidStr == "" {
		pidStr = "20"
	}
	projectID := uint64(1)
	fmt.Sscanf(pidStr, "%d", &projectID)

	if token == "" {
		fmt.Println("PAT 环境变量缺失")
		os.Exit(1)
	}

	base := filepath.Join(os.TempDir(), fmt.Sprintf("e2e-sync-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(base, 0o755); err != nil {
		fail(err)
	}
	defer os.RemoveAll(base)

	// === 1. 打开集合并建内容 ===
	coll, err := collection.Open(base)
	if err != nil {
		fail(err)
	}
	fmt.Println("== 1. 建集合与请求 ==")
	if err := coll.CreateFolder("", "用户管理"); err != nil {
		fail(err)
	}
	r1, err := coll.CreateRequest("用户管理", "用户-列表", "GET")
	if err != nil {
		fail(err)
	}
	r1.URL = "{{host}}/api/user/list"
	if err := coll.SaveRequest(r1); err != nil {
		fail(err)
	}
	r2, err := coll.CreateRequest("", "根-健康检查", "GET")
	if err != nil {
		fail(err)
	}
	r2.URL = "{{host}}/api/health"
	if err := coll.SaveRequest(r2); err != nil {
		fail(err)
	}
	fmt.Printf("  建了 2 请求: %s / %s\n", r1.UID, r2.UID)

	// === 2. 绑定并 push ===
	fmt.Println("== 2. 绑定 + RunSync (push) ==")
	bind := syncengine.BindInfo{
		Linked: true, ServerURL: serverURL, ProjectID: projectID, Mode: syncengine.ModeAuto,
	}
	if err := syncengine.SaveBind(base, bind); err != nil {
		fail(err)
	}
	cli := syncengine.New(serverURL, token, "e2e-device-A")
	eng := &syncengine.Engine{Coll: coll, Bind: bind, CLI: cli}
	rep, err := eng.RunOne()
	if err != nil {
		fail(err)
	}
	fmt.Printf("  推送结果: pushed=%d conflicts=%d rejected=%d cursor=%d errors=%v\n",
		rep.Pushed, rep.Conflicts, rep.Rejected, rep.Cursor, rep.Errors)
	if rep.Pushed == 0 {
		fmt.Println("FAIL: 没有推送任何条目")
		os.Exit(1)
	}

	// === 3. 第二个"设备"空目录 pull ===
	fmt.Println("== 3. 设备 B 空目录 pull ==")
	baseB := filepath.Join(os.TempDir(), fmt.Sprintf("e2e-sync-b-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(baseB, 0o755); err != nil {
		fail(err)
	}
	defer os.RemoveAll(baseB)
	collB, err := collection.Open(baseB)
	if err != nil {
		fail(err)
	}
	if err := syncengine.SaveBind(baseB, bind); err != nil {
		fail(err)
	}
	cliB := syncengine.New(serverURL, token, "e2e-device-B")
	engB := &syncengine.Engine{Coll: collB, Bind: bind, CLI: cliB}
	repB, err := engB.RunOne()
	if err != nil {
		fail(err)
	}
	fmt.Printf("  设备 B: pulled=%d pushed=%d cursor=%d\n", repB.Pulled, repB.Pushed, repB.Cursor)
	infoB, err := collB.Info()
	if err != nil {
		fail(err)
	}
	var names []string
	var walk func(nodes []*collection.Node)
	walk = func(nodes []*collection.Node) {
		for _, n := range nodes {
			names = append(names, fmt.Sprintf("%s(%s)", n.Name, n.Type))
			if n.Children != nil {
				walk(n.Children)
			}
		}
	}
	walk(infoB.Tree)
	fmt.Printf("  设备 B 树: %v\n", names)
	if repB.Pulled == 0 {
		fmt.Println("FAIL: 设备 B 没拉到任何条目")
		os.Exit(1)
	}

	// === 4. 设备 B 改一个请求，push ===
	fmt.Println("== 4. 设备 B 编辑并 push ==")
	reqB, err := collB.ReadRequest(r1.UID)
	if err != nil {
		fmt.Println("  WARN: 设备 B 找不到 r1:", err)
	} else {
		reqB.URL = "{{host}}/api/user/list?page=1"
		if err := collB.SaveRequest(reqB); err != nil {
			fail(err)
		}
		_ = collB.RebuildIndex()
		repB2, err := engB.RunOne()
		if err != nil {
			fail(err)
		}
		fmt.Printf("  设备 B push: pushed=%d conflicts=%d\n", repB2.Pushed, repB2.Conflicts)
	}

	// === 5. 设备 A 拉取 B 的改动 ===
	fmt.Println("== 5. 设备 A 拉取 B 的改动 ==")
	repA2, err := eng.RunOne()
	if err != nil {
		fail(err)
	}
	fmt.Printf("  设备 A: pulled=%d pushed=%d conflicts=%d\n", repA2.Pulled, repA2.Pushed, repA2.Conflicts)
	got, err := coll.ReadRequest(r1.UID)
	if err != nil {
		fmt.Println("  WARN: 设备 A 读 r1 失败:", err)
	} else {
		fmt.Printf("  设备 A 的 r1.URL = %s\n", got.URL)
	}

	// === 6. 冲突场景：两边都改 ===
	fmt.Println("== 6. 冲突场景 ==")
	if got, err := coll.ReadRequest(r1.UID); err == nil {
		got.URL = "{{host}}/A-change"
		_ = coll.SaveRequest(got)
		_ = coll.RebuildIndex()
	}
	if reqB, err := collB.ReadRequest(r1.UID); err == nil {
		reqB.URL = "{{host}}/B-change"
		_ = collB.SaveRequest(reqB)
		_ = collB.RebuildIndex()
	}
	repB3, _ := engB.RunOne()
	fmt.Printf("  B 先 push: pushed=%d conflicts=%d\n", repB3.Pushed, repB3.Conflicts)
	repA3, _ := eng.RunOne()
	fmt.Printf("  A 后 push: pushed=%d conflicts=%d\n", repA3.Pushed, repA3.Conflicts)

	confs, _ := coll.ListConflicts()
	fmt.Printf("  A 的冲突副本数: %d\n", len(confs))
	for _, c := range confs {
		fmt.Printf("    - %s (of %s, serverRev=%d)\n", c.File, c.OfUID, c.ServerRev)
	}

	// === 7. 总结 ===
	fmt.Println("== 7. 总结 ==")
	fmt.Printf("  设备 A dirty: %d\n", countDirty(coll))
	fmt.Printf("  设备 B dirty: %d\n", countDirty(collB))
	fmt.Println("E2E SYNC OK")
}

func countDirty(c *collection.Collection) int {
	n, _ := c.DirtyNodes()
	return len(n)
}

func fail(err error) {
	fmt.Println("FAIL:", err)
	os.Exit(1)
}
