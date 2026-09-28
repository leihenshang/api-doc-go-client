package collection

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// 保存的响应示例：Bruno 的 Save Response 语义 —— 把一次真实响应（含请求快照）落成集合内
// 的纯文本 YAML，可 git diff、可评审，且不参与目录树扫描（见 reserved）。
// 落点：examples/<请求相对目录>/<请求文件名>/<示例名>.yml（目录层级与集合树镜像）。
const (
	examplesDirName = "examples"
	exampleFileType = "response-example"
	maxExampleSeq   = 999 // 重名去重的上限，避免病态目录下死循环
)

// ExampleRequest 示例里的请求快照。
type ExampleRequest struct {
	Method  string `json:"method"`
	URL     string `json:"url"`
	Headers []KV   `json:"headers"`
	Body    Body   `json:"body"`
}

// ExampleResponse 示例里的响应快照；Binary 为真时 Body 是 base64。
type ExampleResponse struct {
	Status      int    `json:"status"`
	Proto       string `json:"proto"`
	TimeMS      int64  `json:"timeMs"`
	Size        int    `json:"size"`
	ContentType string `json:"contentType"`
	Binary      bool   `json:"binary"`
	Headers     []KV   `json:"headers"`
	Body        string `json:"body"`
}

// ResponseExample 一个保存的响应示例。
type ResponseExample struct {
	UID        string          `json:"uid"`
	Name       string          `json:"name"`
	RequestUID string          `json:"requestUid"`
	Path       string          `json:"path"` // 相对集合根（不含集合目录）
	Seq        int             `json:"seq"`  // 同一请求内的保存序号，决定「新 → 旧」排序
	CreatedAt  int64           `json:"createdAt"`
	Request    ExampleRequest  `json:"request"`
	Response   ExampleResponse `json:"response"`
}

// exampleFile 示例的磁盘格式；info/meta/http/response 四段与请求文件同风格。
type exampleFile struct {
	Info struct {
		Name string `yaml:"name"`
		Type string `yaml:"type"`
		Seq  int    `yaml:"seq"`
	} `yaml:"info"`
	Meta struct {
		UID        string `yaml:"uid"`
		RequestUID string `yaml:"request_uid"`
		CreatedAt  int64  `yaml:"created_at"`
	} `yaml:"meta"`
	HTTP struct {
		Method  string `yaml:"method"`
		URL     string `yaml:"url"`
		Headers []KV   `yaml:"headers,omitempty"`
		Body    *Body  `yaml:"body,omitempty"` // 无请求体时不写空段
	} `yaml:"http"`
	Response struct {
		Status      int    `yaml:"status"`
		Proto       string `yaml:"proto,omitempty"`
		TimeMS      int64  `yaml:"time_ms,omitempty"`
		Size        int    `yaml:"size,omitempty"`
		ContentType string `yaml:"content_type,omitempty"`
		Binary      bool   `yaml:"binary,omitempty"`
		Headers     []KV   `yaml:"headers,omitempty"`
		Body        string `yaml:"body,omitempty"`
	} `yaml:"response"`
}

// exampleDir 请求文件 → 示例目录（相对集合根）：examples/<目录>/<请求文件名>。
func exampleDir(reqPath string) string {
	p := filepath.ToSlash(strings.TrimSpace(reqPath))
	return path.Join(examplesDirName, path.Dir(p), strings.TrimSuffix(path.Base(p), path.Ext(p)))
}

// SaveResponseExample 保存一次响应（req 为发送时的请求快照）；
// 同名示例自动去重为「name (1)」「name (2)」，与 Bruno 的命名一致。
func (c *Collection) SaveResponseExample(reqUID, name string, req ExampleRequest, res ExampleResponse) (*ResponseExample, error) {
	if !validEntryName(name) {
		return nil, fmt.Errorf("名称含非法字符或为空")
	}
	r, err := c.ReadRequest(reqUID)
	if err != nil {
		return nil, fmt.Errorf("定位请求: %w", err)
	}
	relDir := exampleDir(r.Path)
	fullDir := filepath.Join(c.Dir, filepath.FromSlash(relDir))
	if err := os.MkdirAll(fullDir, 0o755); err != nil {
		return nil, err
	}
	unique, err := uniqueExampleName(fullDir, sanitizeFileName(name))
	if err != nil {
		return nil, err
	}
	ex := &ResponseExample{
		UID: uuid.NewString(), Name: unique, RequestUID: reqUID,
		Path: path.Join(relDir, unique+".yml"), Seq: c.nextExampleSeq(relDir),
		CreatedAt: time.Now().UnixMilli(), Request: req, Response: res,
	}
	data, err := yaml.Marshal(toExampleFile(ex))
	if err != nil {
		return nil, fmt.Errorf("序列化响应示例: %w", err)
	}
	if err := os.WriteFile(filepath.Join(fullDir, unique+".yml"), data, 0o644); err != nil {
		return nil, err
	}
	return ex, nil
}

// nextExampleSeq 同请求内的下一个保存序号（目录不存在或读取失败时从 1 开始）。
func (c *Collection) nextExampleSeq(relDir string) int {
	existing, err := c.listExamples(relDir)
	if err != nil {
		return 1
	}
	maxSeq := 0
	for _, ex := range existing {
		if ex.Seq > maxSeq {
			maxSeq = ex.Seq
		}
	}
	return maxSeq + 1
}

// uniqueExampleName 取示例目录内不冲突的显示名（同时用作文件名）。
func uniqueExampleName(dir, base string) (string, error) {
	name := base
	for i := 1; i <= maxExampleSeq; i++ {
		if _, err := os.Stat(filepath.Join(dir, name+".yml")); os.IsNotExist(err) {
			return name, nil
		}
		name = fmt.Sprintf("%s (%d)", base, i)
	}
	return "", fmt.Errorf("同名示例过多: %s", base)
}

// ListResponseExamples 某请求已保存的响应示例（新 → 旧）。
func (c *Collection) ListResponseExamples(reqUID string) ([]*ResponseExample, error) {
	r, err := c.ReadRequest(reqUID)
	if err != nil {
		return nil, fmt.Errorf("定位请求: %w", err)
	}
	return c.listExamples(exampleDir(r.Path))
}

// listExamples 读取示例目录；目录不存在视为空（未保存过示例的请求很常见）。
func (c *Collection) listExamples(relDir string) ([]*ResponseExample, error) {
	fullDir := filepath.Join(c.Dir, filepath.FromSlash(relDir))
	entries, err := os.ReadDir(fullDir)
	if os.IsNotExist(err) {
		return []*ResponseExample{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]*ResponseExample, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".yml" && ext != ".yaml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(fullDir, e.Name()))
		if err != nil {
			continue // 单个示例读失败不拖垮列表
		}
		var f exampleFile
		if yaml.Unmarshal(data, &f) != nil || f.Meta.UID == "" {
			continue
		}
		out = append(out, fromExampleFile(path.Join(relDir, e.Name()), &f))
	}
	// 新 → 旧：以保存序号为准（同一毫秒内多次保存也能稳定排序）
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Seq != out[j].Seq {
			return out[i].Seq > out[j].Seq
		}
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt > out[j].CreatedAt
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// DeleteResponseExample 删除示例：移入 .trash/（带时间戳，可人工找回）。
func (c *Collection) DeleteResponseExample(reqUID, exampleUID string) error {
	examples, err := c.ListResponseExamples(reqUID)
	if err != nil {
		return err
	}
	for _, ex := range examples {
		if ex.UID == exampleUID {
			return c.moveToTrash(filepath.Join(c.Dir, filepath.FromSlash(ex.Path)))
		}
	}
	return errNotFound
}

func toExampleFile(ex *ResponseExample) *exampleFile {
	f := &exampleFile{}
	f.Info.Name, f.Info.Type, f.Info.Seq = ex.Name, exampleFileType, ex.Seq
	f.Meta.UID, f.Meta.RequestUID, f.Meta.CreatedAt = ex.UID, ex.RequestUID, ex.CreatedAt
	f.HTTP.Method, f.HTTP.URL = ex.Request.Method, ex.Request.URL
	f.HTTP.Headers = ex.Request.Headers
	if b := ex.Request.Body; b.Type != "" || b.Raw != "" || len(b.Form) > 0 {
		f.HTTP.Body = &b
	}
	f.Response.Status, f.Response.Proto = ex.Response.Status, ex.Response.Proto
	f.Response.TimeMS, f.Response.Size = ex.Response.TimeMS, ex.Response.Size
	f.Response.ContentType, f.Response.Binary = ex.Response.ContentType, ex.Response.Binary
	f.Response.Headers, f.Response.Body = ex.Response.Headers, ex.Response.Body
	return f
}

func fromExampleFile(rel string, f *exampleFile) *ResponseExample {
	body := Body{}
	if f.HTTP.Body != nil {
		body = *f.HTTP.Body
	}
	return &ResponseExample{
		UID: f.Meta.UID, Name: f.Info.Name, RequestUID: f.Meta.RequestUID,
		Path: rel, Seq: f.Info.Seq, CreatedAt: f.Meta.CreatedAt,
		Request: ExampleRequest{Method: f.HTTP.Method, URL: f.HTTP.URL, Headers: f.HTTP.Headers, Body: body},
		Response: ExampleResponse{
			Status: f.Response.Status, Proto: f.Response.Proto,
			TimeMS: f.Response.TimeMS, Size: f.Response.Size,
			ContentType: f.Response.ContentType, Binary: f.Response.Binary,
			Headers: f.Response.Headers, Body: f.Response.Body,
		},
	}
}
