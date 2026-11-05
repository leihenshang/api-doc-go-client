package syncengine

// 拉取时的分组层级：服务端下发的是 parent_uid（服务端 uid），
// 本地新目录必须用同一个 uid 落盘，否则子分组找不到父级、整棵树会被拍平到根。
//
// 这条是 dns 项目实测问题的回归防线：220 个请求 + 9 个子目录，拉下来全堆在根层。

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPullKeepsGroupHierarchy(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 10}
	f.snapshot = Snapshot{Cursor: 10, Items: []Item{
		{Type: "group", UID: "srv-root", Op: "upsert", ItemRev: 1, Payload: payloadOf(t, Payload{Title: "dns项目"})},
		{Type: "group", UID: "srv-child", Op: "upsert", ItemRev: 2, Payload: payloadOf(t, Payload{Title: "DNS 配置", PID: "srv-root"})},
		{Type: "api", UID: "srv-api", Op: "upsert", ItemRev: 3, Payload: payloadOf(t, Payload{
			Name: "域名统计重算-列表", Method: "GET", URL: "/apis/dns/recalc", GroupUID: "srv-child",
		})},
	}}
	c, e := newBoundEngine(t, cli)

	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	if len(rep.Errors) != 0 {
		t.Fatalf("应用出错: %+v", rep.Errors)
	}

	// 根目录必须落在集合根下
	if _, err := os.Stat(filepath.Join(c.Dir, "dns项目", "folder.yml")); err != nil {
		t.Fatalf("根分组未创建: %v", err)
	}
	// 子目录必须在父目录下，而不是根层
	if _, err := os.Stat(filepath.Join(c.Dir, "dns项目", "DNS 配置", "folder.yml")); err != nil {
		t.Fatalf("子分组未挂在父目录下: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "DNS 配置")); err == nil {
		t.Fatal("子分组被拍平到了集合根层")
	}
	// 请求必须落在子分组里
	if _, err := os.Stat(filepath.Join(c.Dir, "dns项目", "DNS 配置", "域名统计重算-列表.yml")); err != nil {
		t.Fatalf("请求未落在子分组目录: %v", err)
	}
}
