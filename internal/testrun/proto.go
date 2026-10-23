package testrun

// gRPC 分支：复用 internal/proto 编译定义与 runner.SendGRPC（unary + 服务器流）。
// 客户端流/双工仍由 runner 明确拒绝。

import (
	"context"
	"errors"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/proto"
	"api-doc-go-client/internal/runner"
)

// sendGRPC 编译集合内定义并执行一次 gRPC 调用。
func sendGRPC(ctx context.Context, c *collection.Collection, r *collection.Request, vars map[string]string, opts runner.Options, cache *proto.Cache) (*runner.Result, error) {
	g := r.GRPC
	if g == nil {
		return nil, errors.New("gRPC 请求缺少 grpc 段")
	}
	if cache == nil {
		cache = proto.NewCache()
	}
	files, err := c.ProtoFiles([]string{g.Proto})
	if err != nil {
		return nil, err
	}
	res, err := cache.Compile(ctx, files, c.ProtoImportPaths(g.Imports))
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
		Settings: r.Settings,
		Compress: g.Compress,
		Types:    res.Types,
	}, vars, opts)
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
