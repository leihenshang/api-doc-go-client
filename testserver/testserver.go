// Command testserver 是一个仅依赖标准库的增删改查测试服务，供客户端在线调试/同步联调使用。
//
// 提供 5 个用户接口（内存存储，重启即清空）：
//
//	POST /api/user/create  新增用户
//	POST /api/user/delete  删除用户（body.id）
//	POST /api/user/update  修改用户（body.id + 待改字段）
//	GET  /api/user/list    用户列表（?page=1&size=10）
//	GET  /api/user/detail  用户详情（?id=1）
//
// 统一响应格式 {code, msg, data}，HTTP 恒 200（与 api-doc-go 服务端契约一致）。
// 用法：
//
//	go run ./testserver              # 默认 :9091
//	go run ./testserver -port 9999   # 指定端口
//	详细接口说明见 testserver/README.md
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

// user 用户实体：接口入参与出参共用同一结构（指针字段区分"未传"与"零值"仅 update 用）。
type user struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Age       int    `json:"age"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// store 内存存储：自增 id + RWMutex 保护，list 按 id 升序返回。
type store struct {
	mu     sync.RWMutex
	nextID int64
	users  map[int64]user
}

func newStore() *store {
	return &store{nextID: 1, users: map[int64]user{}}
}

// resp 统一响应体，与服务端 {code, msg, data} 契约一致。
type resp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data"`
}

func writeJSON(w http.ResponseWriter, v resp) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK) // 业务错误也走 HTTP 200，由 code 表达
	_ = json.NewEncoder(w).Encode(v)
}

func ok(w http.ResponseWriter, data any)    { writeJSON(w, resp{Code: 0, Msg: "ok", Data: data}) }
func bad(w http.ResponseWriter, msg string) { writeJSON(w, resp{Code: 1, Msg: msg, Data: nil}) }

// createUser 新增：POST /api/user/create，body {"name","email","age"}。
func (s *store) createUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		bad(w, "仅支持 POST")
		return
	}
	var in struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Age   int    `json:"age"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		bad(w, "请求体不是合法 JSON")
		return
	}
	if in.Name == "" {
		bad(w, "name 不能为空")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Format(time.RFC3339)
	u := user{ID: s.nextID, Name: in.Name, Email: in.Email, Age: in.Age, CreatedAt: now, UpdatedAt: now}
	s.nextID++
	s.users[u.ID] = u
	ok(w, u)
}

// deleteUser 删除：POST /api/user/delete，body {"id":1}。id 不存在返回 code 1。
func (s *store) deleteUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		bad(w, "仅支持 POST")
		return
	}
	var in struct {
		ID *int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.ID == nil {
		bad(w, "缺少参数 id")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exist := s.users[*in.ID]; !exist {
		bad(w, fmt.Sprintf("用户 %d 不存在", *in.ID))
		return
	}
	delete(s.users, *in.ID)
	ok(w, map[string]any{"deleted": *in.ID})
}

// updateUser 修改：POST /api/user/update，body {"id":1,...}；只覆盖传入的 name/email/age。
func (s *store) updateUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		bad(w, "仅支持 POST")
		return
	}
	var in struct {
		ID    *int64  `json:"id"`
		Name  *string `json:"name"`
		Email *string `json:"email"`
		Age   *int    `json:"age"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.ID == nil {
		bad(w, "缺少参数 id")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, exist := s.users[*in.ID]
	if !exist {
		bad(w, fmt.Sprintf("用户 %d 不存在", *in.ID))
		return
	}
	if in.Name != nil {
		u.Name = *in.Name
	}
	if in.Email != nil {
		u.Email = *in.Email
	}
	if in.Age != nil {
		u.Age = *in.Age
	}
	u.UpdatedAt = time.Now().Format(time.RFC3339)
	s.users[u.ID] = u
	ok(w, u)
}

// listUsers 列表：GET /api/user/list?page=1&size=10，按 id 升序分页。
func (s *store) listUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		bad(w, "仅支持 GET")
		return
	}
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("size"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 10
	}
	s.mu.RLock()
	all := make([]user, 0, len(s.users))
	for _, u := range s.users {
		all = append(all, u)
	}
	s.mu.RUnlock()
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	total := len(all)
	start := (page - 1) * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}
	ok(w, map[string]any{"total": total, "page": page, "size": size, "items": all[start:end]})
}

// detailUser 详情：GET /api/user/detail?id=1。id 缺失或不存在返回 code 1。
func (s *store) detailUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		bad(w, "仅支持 GET")
		return
	}
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil {
		bad(w, "缺少或非法的参数 id")
		return
	}
	s.mu.RLock()
	u, exist := s.users[id]
	s.mu.RUnlock()
	if !exist {
		bad(w, fmt.Sprintf("用户 %d 不存在", id))
		return
	}
	ok(w, u)
}

func main() {
	port := flag.Int("port", 9091, "监听端口")
	flag.Parse()

	s := newStore()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/user/create", s.createUser)
	mux.HandleFunc("/api/user/delete", s.deleteUser)
	mux.HandleFunc("/api/user/update", s.updateUser)
	mux.HandleFunc("/api/user/list", s.listUsers)
	mux.HandleFunc("/api/user/detail", s.detailUser)

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("testserver 监听 %s（接口清单见 README.md）", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
