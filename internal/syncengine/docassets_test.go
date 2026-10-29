package syncengine

// 文档图片随文档同步：拉取时把 /uploads/*（或 /api/file/<hash>）下载到 assets/ 并
// 改写成本地 /doc-assets/<hash>；推送时反向换回服务端 URL（首次上传，之后走映射复用）。
//
// 这一组用例用假的同步服务端（httptest）跑真实 Engine，覆盖：
// 文档条目、接口说明（extra.docs）、<img src> 写法、外链不动、上传只发生一次。

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"api-doc-go-client/internal/collection"
)

// 1×1 PNG（与其它测试同一张）。
const assetTestPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVR4nGNgAAIAAAUAAXpeqz8AAAAASUVORK5CYII="

func assetPNG(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(assetTestPNG)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assetPNGHash(t *testing.T) string {
	t.Helper()
	sum := sha256.Sum256(assetPNG(t))
	return hex.EncodeToString(sum[:])
}

// assetServer 描述假服务端要下发的图片与上传落点。
type assetServer struct {
	mu          sync.Mutex
	uploads     int               // 上传次数
	uploaded    [][]byte          // 每次上传的内容
	uploadURL   string            // 上传成功返回的 URL（默认 /uploads/up.png）
	files       map[string][]byte // 可直接 GET 的路径 → 内容（/uploads/… 或 /api/file/…）
	pushFn      func(*PushReq) *PushResp
	snapshot    Snapshot
	pushes      []*PushReq
	stateCursor int64
}

func newAssetServer(t *testing.T) (*assetServer, *Client) {
	t.Helper()
	a := &assetServer{uploadURL: "/uploads/up.png", files: map[string][]byte{}, stateCursor: 0}
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, data any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "success", "data": data})
	}
	mux.HandleFunc("/api/meta", func(w http.ResponseWriter, r *http.Request) {
		write(w, Meta{ServerVersion: "test", SyncProto: 1, Features: []string{"sync", "change_log"}})
	})
	mux.HandleFunc("/api/sync/state", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		cur := a.stateCursor
		a.mu.Unlock()
		write(w, State{Cursor: cur})
	})
	mux.HandleFunc("/api/sync/snapshot", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		sn := a.snapshot
		a.mu.Unlock()
		write(w, sn)
	})
	mux.HandleFunc("/api/sync/push", func(w http.ResponseWriter, r *http.Request) {
		var req PushReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		a.mu.Lock()
		a.pushes = append(a.pushes, &req)
		fn := a.pushFn
		a.mu.Unlock()
		if fn != nil {
			write(w, fn(&req))
			return
		}
		res := make([]OpResult, 0, len(req.Ops))
		for _, op := range req.Ops {
			res = append(res, OpResult{OpID: op.OpID, Status: "ok", ItemRev: op.BaseRev + 1})
		}
		write(w, PushResp{Results: res, Cursor: req.Ops[0].BaseRev + 1})
	})
	mux.HandleFunc("/api/file/upload", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer func() { _ = f.Close() }()
		data := make([]byte, 0)
		buf := make([]byte, 32<<10)
		for {
			n, rerr := f.Read(buf)
			data = append(data, buf[:n]...)
			if rerr != nil {
				break
			}
		}
		sum := sha256.Sum256(data)
		a.mu.Lock()
		a.uploads++
		a.uploaded = append(a.uploaded, data)
		url := a.uploadURL
		a.mu.Unlock()
		write(w, map[string]string{"url": url, "hash": hex.EncodeToString(sum[:])})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		data, ok := a.files[r.URL.Path]
		a.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(data)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return a, New(srv.URL, "test-token", "dev-1")
}

func (a *assetServer) pushCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.pushes)
}

func (a *assetServer) uploadCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.uploads
}

func (a *assetServer) ops(i int) []Op {
	a.mu.Lock()
	defer a.mu.Unlock()
	if i >= len(a.pushes) {
		return nil
	}
	return a.pushes[i].Ops
}

// TestPullLocalizesDocAndRequestImages 拉取：doc 与 api.docs 里的服务端图片都下载并改写。
func TestPullLocalizesDocAndRequestImages(t *testing.T) {
	srv, cli := newAssetServer(t)
	png := assetPNG(t)
	hash := assetPNGHash(t)
	srv.files["/uploads/a.png"] = png
	srv.files["/api/file/"+hash] = png

	// 构造一条 api 的 extra（带 docs，含两种图片写法）：直接用真实请求生成契约 JSON
	seedColl, err := collection.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := seedColl.CreateFolder("", "模块"); err != nil {
		t.Fatal(err)
	}
	r, err := seedColl.CreateRequest("模块", "带图接口", "GET")
	if err != nil {
		t.Fatal(err)
	}
	r.Docs = "![md](/uploads/a.png)\n<img src=\"/api/file/" + hash + "\">\n![ext](https://cdn.example.com/x.png)"
	extra := collection.SyncExtra(r)

	srv.snapshot = Snapshot{
		Cursor: 1,
		Items: []Item{
			{Type: "group", UID: "g-1", Payload: payloadOf(t, Payload{Title: "模块"})},
			{Type: "api", UID: "req-1", Payload: payloadOf(t, Payload{
				Name: "带图接口", Method: "GET", URL: "/x", GroupUID: "g-1", Extra: extra,
			})},
			{Type: "doc", UID: "doc-1", Payload: payloadOf(t, Payload{
				Name: "带图文档", Content: "![a](/uploads/a.png)\n<img src=\"/uploads/a.png\">",
			})},
		},
	}

	c, e := newBoundEngine(t, cli)
	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	if len(rep.Errors) > 0 {
		t.Fatalf("不应有错误: %v", rep.Errors)
	}
	if !c.HasAsset(hash) {
		t.Fatalf("图片应落到本地 assets/")
	}
	doc, err := c.ReadDoc("doc-1")
	if err != nil {
		t.Fatalf("文档未落盘: %v", err)
	}
	local := collection.DocAssetURL(hash)
	if strings.Count(doc.Content, local) != 2 {
		t.Errorf("文档里的两种写法都应本地化：\n%s", doc.Content)
	}
	if strings.Contains(doc.Content, "/uploads/") {
		t.Errorf("文档不应保留服务端 URL：\n%s", doc.Content)
	}
	req, err := c.ReadRequest("req-1")
	if err != nil {
		t.Fatalf("接口未落盘: %v", err)
	}
	if !strings.Contains(req.Docs, local) || strings.Contains(req.Docs, "/uploads/a.png") {
		t.Errorf("接口说明应本地化：\n%s", req.Docs)
	}
	if !strings.Contains(req.Docs, "https://cdn.example.com/x.png") {
		t.Errorf("外链必须原样保留：\n%s", req.Docs)
	}
	if e.Bind.AssetURLs[hash] == "" {
		t.Errorf("应记录 hash → 服务端 URL 映射（推送复用）")
	}
	if rep.Assets == 0 {
		t.Errorf("新镜像的附件应计入 Report.Assets")
	}
}

// TestPushServerizesImagesAndUploadsOnce 推送：本地图片换回服务端 URL，只上传一次。
func TestPushServerizesImagesAndUploadsOnce(t *testing.T) {
	srv, cli := newAssetServer(t)
	srv.stateCursor = 1
	srv.snapshot = Snapshot{Cursor: 1}

	c, e := newBoundEngine(t, cli)
	e.Bind.Cursor = 1 // 已在服务端游标：本轮不拉取

	hash := assetPNGHash(t)
	if _, err := c.SaveAsset(hash, assetPNG(t)); err != nil {
		t.Fatal(err)
	}
	local := collection.DocAssetURL(hash)

	// ① 文档条目
	if _, err := c.CreateDoc("带图文档", "doc-1", ""); err != nil {
		t.Fatal(err)
	}
	doc, err := c.ReadDoc("doc-1")
	if err != nil {
		t.Fatal(err)
	}
	doc.Content = "![a](" + local + ")"
	if err := c.SaveDoc(doc); err != nil {
		t.Fatal(err)
	}
	// ② 接口说明（docs 走 extra）
	req, err := c.CreateRequest("", "带图接口", "GET")
	if err != nil {
		t.Fatal(err)
	}
	req.Docs = "![a](" + local + ")"
	if err := c.SaveRequest(req); err != nil {
		t.Fatal(err)
	}

	rep, err := e.RunOne()
	if err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	if len(rep.Errors) > 0 {
		t.Fatalf("不应有错误: %v", rep.Errors)
	}
	if got := srv.uploadCount(); got != 1 {
		t.Fatalf("同一张图只应上传一次，实际 %d", got)
	}
	if got := e.Bind.AssetURLs[hash]; got != "/uploads/up.png" {
		t.Errorf("映射未记录: %q", got)
	}
	// 映射要能持久化：下一次启动（新 Engine）仍复用，不重复上传
	if err := SaveBind(c.Dir, e.Bind); err != nil {
		t.Fatalf("SaveBind: %v", err)
	}
	reloaded, err := LoadBind(c.Dir)
	if err != nil {
		t.Fatalf("LoadBind: %v", err)
	}
	if got := reloaded.AssetURLs[hash]; got != "/uploads/up.png" {
		t.Errorf("assetUrls 未随 .sync.json 持久化: %q", got)
	}

	var docOp, apiOp *Op
	for i := range srv.ops(0) {
		op := srv.ops(0)[i]
		switch op.Type {
		case "doc":
			docOp = &op
		case "api":
			apiOp = &op
		}
	}
	if docOp == nil || apiOp == nil {
		t.Fatalf("应同时推送文档与接口：%+v", srv.ops(0))
	}
	var docPayload, apiPayload Payload
	if err := json.Unmarshal(docOp.Payload, &docPayload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(docPayload.Content, "/uploads/up.png") || strings.Contains(docPayload.Content, "/doc-assets/") {
		t.Errorf("文档正文应换成服务端 URL：%q", docPayload.Content)
	}
	if err := json.Unmarshal(apiOp.Payload, &apiPayload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(apiPayload.Extra, "/uploads/up.png") || strings.Contains(apiPayload.Extra, "/doc-assets/") {
		t.Errorf("接口说明应换成服务端 URL：%q", apiPayload.Extra)
	}
	// 本地文件不能被改写（本地始终是 /doc-assets）
	after, err := c.ReadDoc("doc-1")
	if err != nil || !strings.Contains(after.Content, local) {
		t.Errorf("本地文档应保持本地 URL：%q err=%v", after.Content, err)
	}

	// ③ 再跑一轮：内容没变 → 不应再推送/上传
	if _, err := e.RunOne(); err != nil {
		t.Fatalf("第二轮 RunOne: %v", err)
	}
	if got := srv.uploadCount(); got != 1 {
		t.Errorf("第二轮不应再上传，累计 %d", got)
	}
	if got := srv.pushCount(); got != 1 {
		t.Errorf("第二轮不应再推送，push 次数 %d", got)
	}
}

// TestContentRewritersSkipForeignURLs 外链/非法 hash 原样保留，不触发网络。
func TestContentRewritersSkipForeignURLs(t *testing.T) {
	_, cli := newAssetServer(t)
	c, e := newBoundEngine(t, cli)
	_ = c

	content := "![a](https://cdn.example.com/a.png)\n![b](/doc-assets/not-a-hash)\n![c](/doc-assets/" + strings.Repeat("z", 64) + ")"
	if got := e.localizeContent(content, nil); got != content {
		t.Errorf("外链/非法 hash 不应被改写：\n%s", got)
	}
	if got := e.serverizeContent(content, nil); got != content {
		t.Errorf("推送改写也应跳过：\n%s", got)
	}

	// 合法 hash 但本地文件不存在：保留本地 URL，并记录错误（不阻断）
	hash := strings.Repeat("a", 64)
	missing := "![x](/doc-assets/" + hash + ")"
	rep := &Report{}
	if got := e.serverizeContent(missing, rep); got != missing {
		t.Errorf("缺图时应保留原 URL：%s", got)
	}
	if len(rep.Errors) == 0 {
		t.Errorf("缺图应记入 Report.Errors")
	}
	if _, err := os.Stat(c.AssetPath(hash)); !os.IsNotExist(err) {
		t.Errorf("不应凭空创建文件")
	}
}
