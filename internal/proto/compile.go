// Package proto 把集合里的 .proto 定义编译成描述符，供 gRPC 动态调用使用。
//
// 约束（见 doc/客户端gRPC测试功能设计.md 的 D1/D3）：
//   - 纯 Go 编译（github.com/bufbuild/protocompile），使用者不需要安装 protoc；
//   - 不做文件变更监听：缓存只在显式 Invalidate（用户「导入 / 更新定义」）时失效，
//     既不读 mtime 也不做内容 hash 比对。
package proto

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

// Service 一个 gRPC 服务（UI 的「服务」下拉项）。
type Service struct {
	Name    string   `json:"name"`    // 完整名：demo.Greeter
	Comment string   `json:"comment"` // proto 注释（可为空）
	Methods []Method `json:"methods"`
}

// Method 一个 rpc 方法（UI 的「方法」下拉项）。
type Method struct {
	Name            string `json:"name"`            // SayHello
	FullName        string `json:"fullName"`        // demo.Greeter.SayHello
	Input           string `json:"input"`           // 入参完整类型名
	Output          string `json:"output"`          // 出参完整类型名
	Comment         string `json:"comment"`         // proto 注释（可为空）
	ClientStreaming bool   `json:"clientStreaming"` // 客户端流
	ServerStreaming bool   `json:"serverStreaming"` // 服务端流
	Stream          string `json:"stream"`          // unary | server | client | bidi
}

// Result 一次编译的产物：描述符 + 类型仓库 + 可直接展示的服务/方法清单。
type Result struct {
	// Files 本次编译的入口文件（其 import 依赖可通过 fd.Imports() 拿到）
	Files []protoreflect.FileDescriptor
	// Registry 按名查描述符（消息等）
	Registry *protoregistry.Files
	// Types protojson 解析 Any / 扩展时用的动态类型仓库
	Types *protoregistry.Types
	// Services 服务与方法清单（已按名字排序，供 UI 直接渲染）
	Services []Service
}

// Compile 编译 files（.proto 路径）并解析 importPaths（import 搜索目录）。
// 文件不在任何 import 路径下时，其所在目录会被临时加入搜索路径（用户直接点选单个文件也能编）。
func Compile(ctx context.Context, files []string, importPaths []string) (*Result, error) {
	names, paths, err := resolveNames(files, importPaths)
	if err != nil {
		return nil, err
	}
	compiler := protocompile.Compiler{
		Resolver:       protocompile.WithStandardImports(&protocompile.SourceResolver{ImportPaths: paths}),
		SourceInfoMode: protocompile.SourceInfoStandard, // 需要注释：服务/方法说明展示
	}
	compiled, err := compiler.Compile(ctx, names...)
	if err != nil {
		return nil, fmt.Errorf("解析 .proto 失败: %w", err)
	}

	res := &Result{Files: make([]protoreflect.FileDescriptor, 0, len(compiled))}
	for _, fd := range compiled {
		res.Files = append(res.Files, fd)
	}
	res.Registry = new(protoregistry.Files)
	for _, fd := range res.Files {
		registerRecursive(res.Registry, fd)
	}
	types, err := buildTypes(res.Files)
	if err != nil {
		return nil, err
	}
	res.Types = types
	for _, fd := range res.Files {
		res.Services = append(res.Services, servicesOf(fd)...)
	}
	sort.SliceStable(res.Services, func(i, j int) bool { return res.Services[i].Name < res.Services[j].Name })
	return res, nil
}

// ServiceByName 按完整名或短名（末段）查服务。
func (r *Result) ServiceByName(name string) (Service, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Service{}, false
	}
	for _, s := range r.Services {
		if s.Name == name || shortName(s.Name) == name {
			return s, true
		}
	}
	return Service{}, false
}

// MethodDescriptor 按服务（完整名或短名）+ 方法名取方法描述符。
func (r *Result) MethodDescriptor(service, method string) (protoreflect.MethodDescriptor, error) {
	if r == nil {
		return nil, errors.New("定义尚未解析")
	}
	svc, ok := r.ServiceByName(service)
	if !ok {
		return nil, fmt.Errorf("没有找到服务 %q", service)
	}
	for _, fd := range r.Files {
		svcs := fd.Services()
		for i := 0; i < svcs.Len(); i++ {
			sd := svcs.Get(i)
			if string(sd.FullName()) != svc.Name {
				continue
			}
			md := sd.Methods().ByName(protoreflect.Name(strings.TrimSpace(method)))
			if md == nil {
				return nil, fmt.Errorf("服务 %s 里没有方法 %q", svc.Name, method)
			}
			return md, nil
		}
	}
	return nil, fmt.Errorf("没有找到服务 %q 的描述符", service)
}

// MessageDescriptor 按完整类型名查消息描述符（比如生成样例消息时用）。
func (r *Result) MessageDescriptor(name string) (protoreflect.MessageDescriptor, error) {
	if r == nil {
		return nil, errors.New("定义尚未解析")
	}
	d, err := r.Registry.FindDescriptorByName(protoreflect.FullName(strings.TrimSpace(name)))
	if err != nil {
		return nil, fmt.Errorf("没有找到消息 %q: %w", name, err)
	}
	md, ok := d.(protoreflect.MessageDescriptor)
	if !ok {
		return nil, fmt.Errorf("%q 不是消息类型", name)
	}
	return md, nil
}

// resolveNames 把输入文件映射成「相对某个 import 路径」的编译名（protocompile 的 ImportPaths 语义）。
func resolveNames(files, importPaths []string) (names []string, paths []string, err error) {
	if len(files) == 0 {
		return nil, nil, errors.New("没有选中 .proto 文件")
	}
	paths = dedupeAbs(importPaths)
	for _, f := range files {
		abs, aerr := filepath.Abs(f)
		if aerr != nil {
			return nil, nil, fmt.Errorf("解析路径 %s 失败: %w", f, aerr)
		}
		if name, ok := nameWithin(abs, paths); ok {
			names = append(names, name)
			continue
		}
		dir := filepath.Dir(abs)
		if !containsPath(paths, dir) {
			paths = append(paths, dir)
		}
		names = append(names, filepath.Base(abs))
	}
	return names, paths, nil
}

// nameWithin 返回 abs 相对某个 import 路径的名字。
func nameWithin(abs string, paths []string) (string, bool) {
	for _, p := range paths {
		rel, err := filepath.Rel(p, abs)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		return filepath.ToSlash(rel), true
	}
	return "", false
}

// dedupeAbs 去重并转绝对路径（保持输入顺序）。
func dedupeAbs(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if !containsPath(out, abs) {
			out = append(out, abs)
		}
	}
	return out
}

func containsPath(paths []string, p string) bool {
	for _, x := range paths {
		if x == p {
			return true
		}
	}
	return false
}

// registerRecursive 先注册 import 依赖再注册自身（注册要求依赖已就位），重复注册忽略。
func registerRecursive(reg *protoregistry.Files, fd protoreflect.FileDescriptor) {
	imports := fd.Imports()
	for i := 0; i < imports.Len(); i++ {
		registerRecursive(reg, imports.Get(i).FileDescriptor)
	}
	_ = reg.RegisterFile(fd)
}

// buildTypes 把所有消息与扩展注册成动态类型（protojson 解析 Any / 扩展时需要）。
func buildTypes(files []protoreflect.FileDescriptor) (*protoregistry.Types, error) {
	types := new(protoregistry.Types)
	seenFile := map[string]bool{}
	seenMsg := map[string]bool{}

	var walkMessage func(md protoreflect.MessageDescriptor) error
	walkMessage = func(md protoreflect.MessageDescriptor) error {
		name := string(md.FullName())
		if seenMsg[name] {
			return nil
		}
		seenMsg[name] = true
		// 重复注册只可能来自菱形 import（上面已按完整名去重），失败不致命
		_ = types.RegisterMessage(dynamicpb.NewMessageType(md))
		if err := walkExtensions(types, md.Extensions()); err != nil {
			return err
		}
		nested := md.Messages()
		for i := 0; i < nested.Len(); i++ {
			if err := walkMessage(nested.Get(i)); err != nil {
				return err
			}
		}
		return nil
	}

	var walkFile func(fd protoreflect.FileDescriptor) error
	walkFile = func(fd protoreflect.FileDescriptor) error {
		if seenFile[fd.Path()] {
			return nil
		}
		seenFile[fd.Path()] = true
		imports := fd.Imports()
		for i := 0; i < imports.Len(); i++ {
			if err := walkFile(imports.Get(i).FileDescriptor); err != nil {
				return err
			}
		}
		msgs := fd.Messages()
		for i := 0; i < msgs.Len(); i++ {
			if err := walkMessage(msgs.Get(i)); err != nil {
				return err
			}
		}
		return walkExtensions(types, fd.Extensions())
	}

	for _, fd := range files {
		if err := walkFile(fd); err != nil {
			return nil, err
		}
	}
	return types, nil
}

func walkExtensions(types *protoregistry.Types, exts protoreflect.ExtensionDescriptors) error {
	for i := 0; i < exts.Len(); i++ {
		_ = types.RegisterExtension(dynamicpb.NewExtensionType(exts.Get(i)))
	}
	return nil
}

// servicesOf 枚举一个文件里的服务与方法。
func servicesOf(fd protoreflect.FileDescriptor) []Service {
	svcs := fd.Services()
	out := make([]Service, 0, svcs.Len())
	for i := 0; i < svcs.Len(); i++ {
		sd := svcs.Get(i)
		item := Service{Name: string(sd.FullName()), Comment: commentOf(sd)}
		methods := sd.Methods()
		item.Methods = make([]Method, 0, methods.Len())
		for j := 0; j < methods.Len(); j++ {
			item.Methods = append(item.Methods, methodOf(methods.Get(j)))
		}
		out = append(out, item)
	}
	return out
}

func methodOf(md protoreflect.MethodDescriptor) Method {
	return Method{
		Name:            string(md.Name()),
		FullName:        string(md.FullName()),
		Input:           string(md.Input().FullName()),
		Output:          string(md.Output().FullName()),
		Comment:         commentOf(md),
		ClientStreaming: md.IsStreamingClient(),
		ServerStreaming: md.IsStreamingServer(),
		Stream:          streamKind(md),
	}
}

// streamKind 方法形态：unary | server | client | bidi。
func streamKind(md protoreflect.MethodDescriptor) string {
	switch {
	case md.IsStreamingClient() && md.IsStreamingServer():
		return "bidi"
	case md.IsStreamingServer():
		return "server"
	case md.IsStreamingClient():
		return "client"
	default:
		return "unary"
	}
}

// commentOf 取 proto 注释（需要 SourceInfoStandard 才有）。
func commentOf(d protoreflect.Descriptor) string {
	loc := d.ParentFile().SourceLocations().ByDescriptor(d)
	if loc.LeadingComments != "" {
		return strings.TrimSpace(loc.LeadingComments)
	}
	return strings.TrimSpace(loc.TrailingComments)
}

func shortName(full string) string {
	if i := strings.LastIndex(full, "."); i >= 0 {
		return full[i+1:]
	}
	return full
}
