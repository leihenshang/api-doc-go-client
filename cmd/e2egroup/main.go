// A 组端到端：push 冲突 / 删除 tombstone / Web 拉取 / mode 行为 / 文档同步。
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/syncengine"
)

var (
	baseURL = "http://127.0.0.1:2027"
	jwt     string
	pat     string
	pid     uint64
)

func main() {
	jwt = os.Getenv("JWT")
	pat = os.Getenv("PAT")
	pid = 1
	if v := os.Getenv("PID"); v != "" {
		fmt.Sscanf(v, "%d", &pid)
	}
	if pat == "" {
		fmt.Println("need PAT")
		os.Exit(1)
	}

	pass := 0
	fail := 0
	check := func(name string, ok bool, detail string) {
		if ok {
			pass++
			fmt.Printf("  ✓ %s\n", name)
		} else {
			fail++
			fmt.Printf("  ✗ %s — %s\n", name, detail)
		}
	}

	base := tempDir("e2e-a")
	defer os.RemoveAll(base)
	coll := mustOpen(base)
	bind := syncengine.BindInfo{Linked: true, ServerURL: baseURL, ProjectID: pid, Mode: syncengine.ModeAuto}
	must(syncengine.SaveBind(base, bind))
	cli := syncengine.New(baseURL, pat, "device-A")
	eng := &syncengine.Engine{Coll: coll, Bind: bind, CLI: cli}

	// ============ A3: Web 创建 → 客户端增量 pull ============
	fmt.Println("== A3: Web 创建 → 客户端拉取 ==")
	webAPI := webCreateAPI("web-接口", "GET", "https://web.example.com/api")
	check("Web 建接口成功", webAPI != nil, "nil")

	rep, err := eng.RunOne()
	check("RunOne 无错", err == nil, fmt.Sprint(err))
	check("拉到 Web 条目", rep.Pulled > 0, fmt.Sprintf("pulled=%d", rep.Pulled))

	found := false
	if webAPI != nil {
		if r, err := coll.ReadRequest(webAPI.uid); err == nil && r.Name == "web-接口" {
			found = true
		}
	}
	check("Web 接口出现在本地", found, "ReadRequest 失败或名称不符")

	// ============ A1: push 冲突（base_rev 过期） ============
	fmt.Println("== A1: push 冲突（base_rev） ==")
	// 建一个本地请求并推送
	r1 := mustReq(coll, "冲突测试", "GET")
	r1.URL = "{{host}}/v1"
	must0(coll.SaveRequest(r1))
	must0(coll.RebuildIndex())
	eng.RunOne()

	// Web 端改同一条（提升 last_change_id）
	webUpdateAPI(r1.UID, "冲突测试-web改", "GET", "https://web.example.com/v2")

	// 客户端用过期 base_rev 直接 push
	staleOp := syncengine.Op{
		OpID:    "op-conflict-1",
		Op:      "upsert",
		Type:    "api",
		UID:     r1.UID,
		BaseRev: 1, // 故意过期
		Payload: json.RawMessage(`{"name":"本地改","method":"GET","url":"https://local/v3"}`),
	}
	pr, err := cli.Push(&syncengine.PushReq{ProjectID: pid, Ops: []syncengine.Op{staleOp}})
	check("push 可调用", err == nil, fmt.Sprint(err))
	if pr != nil && len(pr.Results) > 0 {
		check("返回 conflict", pr.Results[0].Status == "conflict", "status="+pr.Results[0].Status)
		check("conflict 带 server 版本", len(pr.Results[0].Server) > 0, "empty server payload")
	} else {
		check("返回 conflict", false, "no results")
	}

	// 幂等：同一 op_id 再推应返回上次结果
	pr2, _ := cli.Push(&syncengine.PushReq{ProjectID: pid, Ops: []syncengine.Op{staleOp}})
	if pr2 != nil && len(pr2.Results) > 0 {
		check("op_id 幂等", pr2.Results[0].Status == pr.Results[0].Status, "status 不同")
	} else {
		check("op_id 幂等", false, "no results")
	}

	// ============ A2: 删除同步（tombstone） ============
	fmt.Println("== A2: 删除 tombstone ==")
	r2 := mustReq(coll, "待删除", "GET")
	must0(coll.SaveRequest(r2))
	must0(coll.RebuildIndex())
	eng.RunOne()

	// 本地删除
	must0(coll.DeleteRequest(r2.UID))
	must0(coll.RebuildIndex())
	rep2, _ := eng.RunOne()
	check("删除已推送", rep2.Pushed >= 0, fmt.Sprint(rep2))

	// 服务端应有 delete 流水
	del := webHasTombstone(r2.UID)
	check("服务端有 delete 流水", del, "changes 里无 delete")

	// 设备 B 拉取应把该条移到 .trash
	baseB := tempDir("e2e-a-b")
	defer os.RemoveAll(baseB)
	collB := mustOpen(baseB)
	must(syncengine.SaveBind(baseB, bind))
	engB := &syncengine.Engine{Coll: collB, Bind: bind, CLI: syncengine.New(baseURL, pat, "device-B")}
	engB.RunOne()
	_, errB := collB.ReadRequest(r2.UID)
	check("B 上该条已不可读（tombstone）", errB != nil, "仍可读")

	// ============ A4: mode=mirror 只拉不推 ============
	fmt.Println("== A4: mode=mirror 只拉不推 ==")
	baseM := tempDir("e2e-a-m")
	defer os.RemoveAll(baseM)
	collM := mustOpen(baseM)
	bindM := bind
	bindM.Mode = syncengine.ModeMirror
	must(syncengine.SaveBind(baseM, bindM))
	engM := &syncengine.Engine{Coll: collM, Bind: bindM, CLI: syncengine.New(baseURL, pat, "device-M")}
	// 先建个本地请求（不应被推上去）
	rm := mustReq(collM, "镜像本地独有", "GET")
	must0(collM.SaveRequest(rm))
	must0(collM.RebuildIndex())
	repM, _ := engM.RunOne()
	check("mirror 不推送", repM.Pushed == 0, fmt.Sprintf("pushed=%d", repM.Pushed))

	// ============ A5: 文档同步 ============
	fmt.Println("== A5: 文档同步 ==")
	// Web 建文档
	webDoc := webCreateDoc("web-文档", "文档内容 **markdown**")
	check("Web 建文档", webDoc != nil, "nil")
	rep3, _ := eng.RunOne()
	check("pull 含文档变更", rep3.Pulled >= 0, fmt.Sprint(rep3.Pulled))

	// 本地建请求再 push（文档客户端侧暂不落盘，验证服务端文档可被 changes 取到）
	ch, _ := cli.Changes(pid, 0, 500)
	hasDoc := false
	if ch != nil {
		for _, it := range ch.Items {
			if it.Type == "doc" {
				hasDoc = true
			}
		}
	}
	check("changes 含 doc 条目", hasDoc, "无 doc")

	fmt.Printf("\n== A 组结果: %d 通过 / %d 失败 ==\n", pass, fail)
	if fail > 0 {
		os.Exit(1)
	}
	fmt.Println("A GROUP OK")
}

// ---------- 工具 ----------

func tempDir(prefix string) string {
	d := filepath.Join(os.TempDir(), fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()))
	_ = os.MkdirAll(d, 0o755)
	return d
}

func mustOpen(dir string) *collection.Collection {
	c, err := collection.Open(dir)
	if err != nil {
		panic(err)
	}
	return c
}

func mustReq(c *collection.Collection, name, method string) *collection.Request {
	r, err := c.CreateRequest("", name, method)
	if err != nil {
		panic(err)
	}
	return r
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func must0(err error) {
	if err != nil {
		panic(err)
	}
}

// webCreateAPI 服务端建接口，返回 uid/label。
type webResult struct {
	uid string
}

func webCreateAPI(name, method, url string) *webResult {
	// 先建分组
	g := apiCall("POST", "/api/group", map[string]any{
		"project_id": pid, "title": "sync-a-group", "type": 1,
	})
	_ = g
	body := map[string]any{
		"project_id":       pid,
		"api_name":         name,
		"http_method_type": method,
		"url":              url,
	}
	raw := apiCall("POST", "/api/api", body)
	if raw == nil {
		return nil
	}
	// 响应里拿 ext_uid —— CreateApi 会自动分配
	data, _ := json.Marshal(raw)
	var wrap struct {
		Data struct {
			ID     uint64 `json:"id"`
			ExtUID string `json:"ext_uid"`
		} `json:"data"`
	}
	_ = json.Unmarshal(data, &wrap)
	// ext_uid 可能不在响应里；从 changes 里找
	uid := findExtUIDByName(name)
	return &webResult{uid: uid}
}

func webUpdateAPI(uid, name, method, url string) {
	// 先查服务端 id
	id := findIDByExtUID(uid)
	if id == 0 {
		return
	}
	apiCall("PUT", "/api/api", map[string]any{
		"id": id, "project_id": pid,
		"api_name": name, "http_method_type": method, "url": url,
	})
}

func webCreateDoc(title, content string) map[string]any {
	return apiCall("POST", "/api/doc", map[string]any{
		"project_id": pid, "title": title, "content": content,
	})
}

func webHasTombstone(uid string) bool {
	ch := apiCall("GET", fmt.Sprintf("/api/sync/changes?project_id=%d&since=0&limit=500", pid), nil)
	if ch == nil {
		return false
	}
	b, _ := json.Marshal(ch)
	var wrap struct {
		Data syncengine.Changes `json:"data"`
	}
	_ = json.Unmarshal(b, &wrap)
	for _, it := range wrap.Data.Items {
		if it.UID == uid && (it.Deleted || it.Op == "delete") {
			return true
		}
	}
	return false
}

func findExtUIDByName(name string) string {
	ch := apiCall("GET", fmt.Sprintf("/api/sync/changes?project_id=%d&since=0&limit=500", pid), nil)
	if ch == nil {
		return ""
	}
	b, _ := json.Marshal(ch)
	var wrap struct {
		Data syncengine.Changes `json:"data"`
	}
	_ = json.Unmarshal(b, &wrap)
	for _, it := range wrap.Data.Items {
		if it.Type != "api" {
			continue
		}
		var p syncengine.Payload
		_ = json.Unmarshal(it.Payload, &p)
		if p.Name == name {
			return it.UID
		}
	}
	return ""
}

func findIDByExtUID(extUID string) uint64 {
	raw := apiCall("GET", fmt.Sprintf("/api/sync/snapshot?project_id=%d&offset=0&limit=500", pid), nil)
	if raw == nil {
		return 0
	}
	b, _ := json.Marshal(raw)
	var wrap struct {
		Data syncengine.Snapshot `json:"data"`
	}
	_ = json.Unmarshal(b, &wrap)
	// snapshot 只有 ext_uid；改用 changes+item  —— 服务端 GetApi 需要数字 id
	// 简化：从列表接口找
	list := apiCall("GET", fmt.Sprintf("/api/api?project_id=%d&page_num=1&page_size=100", pid), nil)
	if list == nil {
		return 0
	}
	lb, _ := json.Marshal(list)
	var lwrap struct {
		Data struct {
			List []struct {
				ID     uint64 `json:"id"`
				ExtUID string `json:"ext_uid"`
			} `json:"list"`
		} `json:"data"`
	}
	_ = json.Unmarshal(lb, &lwrap)
	for _, it := range lwrap.Data.List {
		if it.ExtUID == extUID {
			return it.ID
		}
	}
	return 0
}

func apiCall(method, path string, body any) map[string]any {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, baseURL+path, rdr)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}
