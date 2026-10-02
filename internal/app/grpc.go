package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/proto"
	"api-doc-go-client/internal/runner"
	"api-doc-go-client/internal/varx"

	"github.com/leihenshang/api-doc-go-share/codegen"
)

// GrpcSchemaInfo 一次定义解析的结果（前端 Schema 分段与「服务 / 方法」选择器用）。
type GrpcSchemaInfo struct {
	// Proto 入口定义（相对集合目录）：请求的 grpc.proto 存它
	Proto string `json:"proto"`
	// Imports import 搜索路径（相对集合目录）
	Imports []string `json:"imports"`
	// Protos 集合内可用的定义清单（下拉用；导入后即为本次落盘结果）
	Protos []collection.ProtoFileInfo `json:"protos"`
	// Services 服务与方法清单（含注释与流式形态）
	Services []proto.Service `json:"services"`
}

// ImportGrpcProtos 导入 .proto：先编译校验，成功后复制进集合 protos/，再按集合内路径重新解析。
//
// 两步走是为了「导入动作原子化」（设计文档 D4）：解析失败时不复制、不落盘，直接把错误返回给界面。
// 定义内容变化后必须由用户再次导入 —— 不做文件监听（D3），故这里显式 Invalidate 缓存。
func (a *App) ImportGrpcProtos(files []string, imports []string) (*GrpcSchemaInfo, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("没有选择 .proto 文件")
	}
	// 1) 先按源路径编译（import 路径含集合内 protos/，便于引用已导入的公共定义）
	if _, err := proto.Compile(context.Background(), files, a.importPaths(c, imports)); err != nil {
		return nil, err
	}
	// 2) 复制进集合
	infos, err := c.ImportProtos(files)
	if err != nil {
		return nil, err
	}
	a.protoCache.Invalidate()
	// 3) 按集合内路径重新解析（返回给界面的路径必须是集合内相对路径）
	rels := make([]string, 0, len(infos))
	for _, item := range infos {
		rels = append(rels, item.Rel)
	}
	return a.compileCollectionProtos(c, rels, imports)
}

// ListGrpcProtos 列出集合内已导入的定义（UI 下拉，免点文件选择器）。
func (a *App) ListGrpcProtos() ([]collection.ProtoFileInfo, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	return c.ListProtos()
}

// RemoveGrpcProto 删除集合内的定义（调用方负责先确认没有请求在引用）。
func (a *App) RemoveGrpcProto(rel string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	if err := c.RemoveProto(rel); err != nil {
		return err
	}
	a.protoCache.Invalidate()
	return nil
}

// LoadGrpcSchema 解析请求里已保存的定义（打开请求 / 「更新定义」后调用），带缓存。
func (a *App) LoadGrpcSchema(protoRel string, imports []string) (*GrpcSchemaInfo, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	return a.compileCollectionProtos(c, []string{protoRel}, imports)
}

// GrpcSampleMessage 按定义生成请求消息样例（protojson 文本），供 Message 分段一键填充。
func (a *App) GrpcSampleMessage(protoRel string, imports []string, service, method string) (string, error) {
	c, err := a.requireCollection()
	if err != nil {
		return "", err
	}
	res, err := a.compileResolved(c, protoRel, imports)
	if err != nil {
		return "", err
	}
	md, err := res.MethodDescriptor(service, method)
	if err != nil {
		return "", err
	}
	return proto.SampleMessage(md.Input(), res.Types)
}

// GrpcMessageFields 列出入参消息的顶层字段（Message 分段的字段提示，G5.4）。
func (a *App) GrpcMessageFields(protoRel string, imports []string, service, method string) ([]proto.FieldInfo, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	res, err := a.compileResolved(c, protoRel, imports)
	if err != nil {
		return nil, err
	}
	md, err := res.MethodDescriptor(service, method)
	if err != nil {
		return nil, err
	}
	return proto.Fields(md.Input()), nil
}

// GrpcValidateMessage 校验请求消息（Message 分段的内联提示，G5.3）：
// 空串表示通过，否则返回带字段路径的错误文案；定义本身解析失败仍按 error 返回。
//
// 单独立一个方法而不是复用发送：发送要建连接，界面只需要「按定义校验」这一步。
func (a *App) GrpcValidateMessage(protoRel string, imports []string, service, method, message string) (string, error) {
	c, err := a.requireCollection()
	if err != nil {
		return "", err
	}
	res, err := a.compileResolved(c, protoRel, imports)
	if err != nil {
		return "", err
	}
	md, err := res.MethodDescriptor(service, method)
	if err != nil {
		return "", err
	}
	if verr := proto.ValidateMessage(md, message, res.Types); verr != nil {
		return verr.Error(), nil
	}
	return "", nil
}

// GrpcDefaultInfo 集合级默认定义的前端契约。
//
// 共享包的 GRPCDefault 只有 yaml 标签（它只用于清单落盘），直接返回会序列化成
// `{"Proto":…}` 这种大写键，前端按 `proto` 取不到值 —— 这里显式给一份小驼峰 DTO。
type GrpcDefaultInfo struct {
	Proto   string   `json:"proto"`
	Imports []string `json:"imports"`
}

// GetGrpcDefault 读集合级默认 gRPC 定义（P8；未配置返回 nil）。
func (a *App) GetGrpcDefault() (*GrpcDefaultInfo, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	def := c.GrpcDefault()
	if def == nil {
		return nil, nil
	}
	return &GrpcDefaultInfo{Proto: def.Proto, Imports: def.Imports}, nil
}

// SetGrpcDefault 写集合级默认 gRPC 定义（P8；proto 为空 = 清除）。
// 写盘后不做缓存失效：定义文件本身没变，只是「哪个请求以它为默认」变了。
func (a *App) SetGrpcDefault(proto string, imports []string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.SetGrpcDefault(proto, imports)
}

// generateGrpcCode 生成 grpcurl 片段（G11.5）：gRPC 只有这一种目标语言。
//
// 变量按当前环境渲染（地址 / 消息 / 元数据），明文与 TLS 决定 -plaintext / -insecure，
// 定义与 import 路径交给共享包拆成 -import-path / -proto。
func (a *App) generateGrpcCode(lang, envName string, r *collection.Request) (string, error) {
	if l := strings.ToLower(strings.TrimSpace(lang)); l != "" && l != "grpcurl" {
		return "", fmt.Errorf("gRPC 请求只能生成 grpcurl（当前选择 %q）", lang)
	}
	g := r.GRPC
	if g == nil {
		return "", errors.New("gRPC 请求缺少 grpc 段")
	}
	vars, err := a.envVars(envName)
	if err != nil {
		return "", err
	}
	target, _ := varx.Resolve(g.Target, vars)
	message, _ := varx.Resolve(g.Message, vars)
	metadata := make([]codegen.KV, 0, len(g.Metadata))
	for _, row := range g.Metadata {
		if !row.Enabled || strings.TrimSpace(row.Name) == "" {
			continue
		}
		name, _ := varx.Resolve(row.Name, vars)
		value, _ := varx.Resolve(row.Value, vars)
		metadata = append(metadata, codegen.KV{Name: name, Value: value})
	}
	mode := ""
	if g.TLS != nil {
		mode = strings.ToLower(strings.TrimSpace(g.TLS.Mode))
	}
	plaintext := mode == "" || mode == "none" || mode == "plaintext"
	insecure := false
	if !plaintext {
		a.mu.Lock()
		globalInsecure := a.settings.InsecureSSL
		a.mu.Unlock()
		insecure = g.TLS.InsecureSkipVerify || globalInsecure
	}
	return codegen.GrpcurlSnippet(codegen.GRPCRequest{
		Target:    target,
		Service:   g.Service,
		Method:    g.Method,
		Message:   message,
		Metadata:  metadata,
		Proto:     g.Proto,
		Imports:   g.Imports,
		Plaintext: plaintext,
		Insecure:  insecure,
	}), nil
}

// compileCollectionProtos 编译集合内的定义并汇总服务清单（带缓存）。
func (a *App) compileCollectionProtos(c *collection.Collection, rels []string, imports []string) (*GrpcSchemaInfo, error) {
	files, err := collectionProtoFiles(c, rels)
	if err != nil {
		return nil, err
	}
	importPaths := a.importPaths(c, imports)
	res, err := a.protoCache.Compile(context.Background(), files, importPaths)
	if err != nil {
		return nil, err
	}
	protos, lerr := c.ListProtos()
	if lerr != nil {
		return nil, lerr
	}
	entry := entryProtoWithServices(rels, res)
	return &GrpcSchemaInfo{
		Proto:    entry,
		Imports:  normalizeImports(imports),
		Protos:   protos,
		Services: res.Services,
	}, nil
}

// importPaths import 搜索路径：集合内 protos/ 始终参与，再叠加用户额外指定（相对集合目录或绝对路径）。
func (a *App) importPaths(c *collection.Collection, imports []string) []string {
	paths := []string{c.ProtoDir()}
	for _, rel := range imports {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		if filepath.IsAbs(rel) {
			paths = append(paths, filepath.Clean(rel))
			continue
		}
		paths = append(paths, filepath.Join(c.Dir, filepath.FromSlash(rel)))
	}
	return paths
}

// collectionProtoFiles 把集合内相对定义路径展开成绝对路径（读路径宽松：允许引用集合外的绝对路径）。
func collectionProtoFiles(c *collection.Collection, rels []string) ([]string, error) {
	if len(rels) == 0 {
		return nil, errors.New("还没有导入 .proto 定义")
	}
	files := make([]string, 0, len(rels))
	for _, rel := range rels {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			return nil, errors.New("定义路径为空（请先导入 .proto）")
		}
		if filepath.IsAbs(rel) {
			files = append(files, filepath.Clean(rel))
			continue
		}
		full := filepath.Join(c.Dir, filepath.FromSlash(rel))
		if _, err := os.Stat(full); err != nil {
			return nil, fmt.Errorf("找不到定义 %s（可能未导入或已被移除）", rel)
		}
		files = append(files, full)
	}
	return files, nil
}

// entryProtoWithServices 选入口定义：优先取「自身定义了服务」的第一个文件（多文件导入时
// 用户往往把 message 定义和 service 定义分开），都没有服务时退回第一个。
// proto.Compile 返回的 Files 与入参文件顺序一一对应，可以按下标回推。
func entryProtoWithServices(rels []string, res *proto.Result) string {
	if len(rels) == 0 {
		return ""
	}
	if res != nil && len(res.Files) == len(rels) {
		for i, fd := range res.Files {
			if fd.Services().Len() > 0 {
				return rels[i]
			}
		}
	}
	return rels[0]
}

// normalizeImports 去掉空项（保持顺序）。
func normalizeImports(imports []string) []string {
	out := make([]string, 0, len(imports))
	for _, rel := range imports {
		if rel = strings.TrimSpace(rel); rel != "" {
			out = append(out, rel)
		}
	}
	return out
}

// sendGRPC 解析定义并把 grpc 段交给执行器（SendRequest 的 gRPC 分支）。
func (a *App) sendGRPC(ctx context.Context, r *collection.Request, vars map[string]string) (*runner.Result, error) {
	g := r.GRPC
	if g == nil {
		return nil, errors.New("gRPC 请求缺少 grpc 段")
	}
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	res, err := a.compileResolved(c, g.Proto, g.Imports)
	if err != nil {
		return nil, err
	}
	md, err := res.MethodDescriptor(g.Service, g.Method)
	if err != nil {
		return nil, err
	}
	return runner.SendGRPC(ctx, runner.GrpcRequest{
		Target:   g.Target,
		Method:   md,
		Metadata: g.Metadata,
		Message:  g.Message,
		TLS:      toRunnerTLS(g.TLS),
		Settings: r.Settings, // 请求级超时 / 忽略证书校验（G7.3/G7.4）
		Compress: g.Compress, // gzip 请求压缩（G7.5）
		Types:    res.Types,
	}, vars, a.sendOptions())
}

// compileResolved 编译单个定义（sendGRPC 与样例消息共用）。
func (a *App) compileResolved(c *collection.Collection, protoRel string, imports []string) (*proto.Result, error) {
	files, err := collectionProtoFiles(c, []string{protoRel})
	if err != nil {
		return nil, err
	}
	return a.protoCache.Compile(context.Background(), files, a.importPaths(c, imports))
}

// toRunnerTLS 共享包 TLS 设置 → 执行器设置。
func toRunnerTLS(tls *collection.GrpcTLS) runner.GrpcTLS {
	if tls == nil {
		return runner.GrpcTLS{}
	}
	return runner.GrpcTLS{
		Mode:               tls.Mode,
		CA:                 tls.CA,
		Cert:               tls.Cert,
		Key:                tls.Key,
		InsecureSkipVerify: tls.InsecureSkipVerify,
	}
}
