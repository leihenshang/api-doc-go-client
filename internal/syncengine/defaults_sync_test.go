package syncengine

// U6：集合级公共参数（服务端 project.defaults）拉到本地清单；服务端清空时本地也跟着清。

import "testing"

func TestProjectDefaultsAppliedFromServer(t *testing.T) {
	f, cli := newFakeServer(t)
	f.state = State{Cursor: 10}
	f.snapshot = Snapshot{Cursor: 10, Items: []Item{
		{Type: "defaults", UID: "project-1", Op: "upsert", ItemRev: 1, Payload: payloadOf(t, Payload{
			Defaults: `{"headers":[{"name":"X-Common","value":"1","enabled":true}],"auth":{"type":"bearer","token":"t"}}`,
		})},
	}}
	c, e := newBoundEngine(t, cli)

	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	if len(rep.Errors) != 0 {
		t.Fatalf("应用公共参数不应报错: %+v", rep.Errors)
	}
	d := c.Defaults()
	if d == nil || len(d.Headers) != 1 || d.Headers[0].Name != "X-Common" {
		t.Fatalf("公共请求头未落地: %+v", d)
	}
	if d.Auth == nil || d.Auth.Type != "bearer" || d.Auth.Token != "t" {
		t.Fatalf("公共认证未落地: %+v", d.Auth)
	}

	// 第二轮：服务端清空 defaults → 本地镜像也要清空（否则队友删掉的公共头会一直生效）。
	f.state = State{Cursor: 11}
	e.Bind.Cursor = 10
	f.changes = Changes{Cursor: 11, Items: []Item{
		{Type: "defaults", UID: "project-1", Op: "upsert", ItemRev: 2, Payload: payloadOf(t, Payload{})},
	}}
	if _, err := e.RunOne(); err != nil {
		t.Fatalf("RunOne 第二轮: %v", err)
	}
	if d := c.Defaults(); d != nil && (len(d.Headers) > 0 || d.Auth != nil) {
		t.Fatalf("服务端清空后本地应同步清空: %+v", d)
	}
}
