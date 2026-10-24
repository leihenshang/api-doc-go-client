package collection

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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
	// exampleNameMaxBytes 示例**基名**（不含 .yml）的字节上限。
	//
	// 口径是字节而不是 rune：文件系统对单个文件名限 255 字节，中文一个字 3 字节。
	// 留足余量：基名 240 → 文件名 244 → 去重后缀「 (999)」6 字节仍是 250，均未越界。
	// 回收站文件名（时间戳前缀 + 16 字节）不靠这里兜，由 trashSafeBase 截断。
	exampleNameMaxBytes = 240
)

// checkExampleName 校验示例基名（**不含扩展名**）不超过字节上限。
//
// 为什么需要单独兜一道：share 的 SanitizeFileName 按 **rune** 截断到 100，
// 对中文来说是 300 字节 —— 依然超过 255 字节的文件名上限，直接落到 writeFileAtomic
// 只会拿到 ENAMETOOLONG（AI / 用户读不懂）。这里提前给出可读报错。
func checkExampleName(base string) error {
	if len(base) > exampleNameMaxBytes {
		return fmt.Errorf("示例名过长：名字 %d 字节，超过 %d 字节上限（按字节算，中文约 %d 字），请缩短",
			len(base), exampleNameMaxBytes, exampleNameMaxBytes/3)
	}
	return nil
}

// exampleBaseOf 取示例相对路径的文件名（不含扩展名）。
func exampleBaseOf(rel string) string {
	b := path.Base(filepath.ToSlash(rel))
	return strings.TrimSuffix(b, path.Ext(b))
}

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

// ExampleDirForPath exampleDir 的导出版本（同步引擎按请求路径定位示例目录）。
func ExampleDirForPath(reqPath string) string { return exampleDir(reqPath) }

// exampleSaveMu 串行化「选名 → 计算序号 → 写文件」：这三步之间没有原子性，
// 并发保存（两个 MCP 会话 / 界面与 MCP 同时存）会取到同一个名字并互相覆盖。
var exampleSaveMu sync.Mutex

// SaveResponseExample 保存一次响应（req 为发送时的请求快照）；
// 同名示例自动去重为「name (1)」「name (2)」，与 Bruno 的命名一致。
func (c *Collection) SaveResponseExample(reqUID, name string, req ExampleRequest, res ExampleResponse) (*ResponseExample, error) {
	exampleSaveMu.Lock()
	defer exampleSaveMu.Unlock()
	if !validEntryName(name) {
		return nil, fmt.Errorf("名称含非法字符或为空")
	}
	base := sanitizeFileName(name)
	if err := checkExampleName(base); err != nil {
		return nil, err
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
	unique, err := uniqueExampleName(fullDir, base)
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
	if err := writeFileAtomic(filepath.Join(fullDir, unique+".yml"), data, 0o644); err != nil {
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
//
// 循环从 base 开始逐个试 base (1)…base (maxExampleSeq)：不能用「先改名再检查」的写法
// （那样最后一个候选名会被跳过，等于少一个可用名）。
func uniqueExampleName(dir, base string) (string, error) {
	name := base
	for i := 1; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, name+".yml")); os.IsNotExist(err) {
			return name, nil
		}
		if i > maxExampleSeq {
			return "", fmt.Errorf("同名示例过多: %s", base)
		}
		name = fmt.Sprintf("%s (%d)", base, i)
	}
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

// ListAllResponseExamples 遍历 examples/ 下全部示例（同步推送用）；目录不存在视为空。
func (c *Collection) ListAllResponseExamples() ([]*ResponseExample, error) {
	root := filepath.Join(c.Dir, examplesDirName)
	out := []*ResponseExample{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 单个文件读失败不拖垮全量
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext != ".yml" && ext != ".yaml" {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		var f exampleFile
		if yaml.Unmarshal(data, &f) != nil || f.Meta.UID == "" {
			return nil
		}
		rel, rerr := filepath.Rel(c.Dir, p)
		if rerr != nil {
			return nil
		}
		out = append(out, fromExampleFile(filepath.ToSlash(rel), &f))
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return out, nil
}

// UpsertResponseExampleFile 按 UID 写入/替换示例文件（同步拉取应用用）。
// 示例的落点由其所属请求决定：调用方负责把 Path 设为 examples/<请求目录>/<名>.yml。
// 目标文件名被其它 UID 占用时自动改成「名 (N)」，返回最终落盘的相对路径。
//
// 顺序很关键：**先写新文件成功，再删旧落点**。反过来（先删后写）一旦写入失败
// （名字过长 / 目录不可写 / 磁盘满），示例就整条消失 —— 那是一次静默的数据丢失。
func (c *Collection) UpsertResponseExampleFile(ex *ResponseExample) (string, error) {
	exampleSaveMu.Lock()
	defer exampleSaveMu.Unlock()
	existing, _ := c.ListAllResponseExamples()
	// 同 UID 的现有落点：stale = 换了路径的旧文件（写成功后删）；
	// inPlace = 「原地重写同一路径」—— 这种调用不该受长度上限约束，
	// 否则历史遗留 / 外部同步来的超长名示例连「只改状态码」都做不到。
	stale, inPlace := "", false
	for _, old := range existing {
		if old.UID != ex.UID {
			continue
		}
		if old.Path != ex.Path {
			stale = old.Path
		} else {
			inPlace = true
		}
		break
	}
	// 文件名冲突：换名，并同步改显示名（文件名与示例名同源）
	for _, old := range existing {
		if old.UID != ex.UID && old.Path == ex.Path {
			base := sanitizeFileName(ex.Name)
			// 长度上限：先去重再写，超限时直接报错（不动旧文件，见下面的先写后删）
			if err := checkExampleName(base); err != nil {
				return "", err
			}
			unique, uerr := uniqueExampleName(filepath.Dir(filepath.Join(c.Dir, filepath.FromSlash(ex.Path))), base)
			if uerr != nil {
				return "", uerr
			}
			ex.Name = unique
			ex.Path = path.Join(path.Dir(ex.Path), unique+".yml")
			break
		}
	}
	// 落点变了（新建 / 改名 / 换目录）才检查长度：超限时不动旧文件、直接报错。
	// 原地重写不检查 —— 已存在的名字是什么就写什么，长度问题不该让用户改不动它。
	if !inPlace {
		if err := checkExampleName(exampleBaseOf(ex.Path)); err != nil {
			return "", err
		}
	}
	full := filepath.Join(c.Dir, filepath.FromSlash(ex.Path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	data, err := yaml.Marshal(toExampleFile(ex))
	if err != nil {
		return "", fmt.Errorf("序列化响应示例: %w", err)
	}
	if err := writeFileAtomic(full, data, 0o644); err != nil {
		return "", err
	}
	if stale != "" {
		_ = os.Remove(filepath.Join(c.Dir, filepath.FromSlash(stale)))
	}
	return ex.Path, nil
}

// DeleteResponseExampleByUID 按 UID 删除示例文件（同步删除应用用）；找不到视为已删除。
func (c *Collection) DeleteResponseExampleByUID(uid string) error {
	existing, err := c.ListAllResponseExamples()
	if err != nil {
		return err
	}
	for _, ex := range existing {
		if ex.UID == uid {
			return os.Remove(filepath.Join(c.Dir, filepath.FromSlash(ex.Path)))
		}
	}
	return nil
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
