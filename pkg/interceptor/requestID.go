package interceptor

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"gopherex.com/pkg/common"
)

type ctxKey string

func RequestIDUnary() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		// 从上下文中获取 request id，并写入 outgoing metadata，方便服务端获取
		var rid string
		if v := ctx.Value(common.CtxKeyRequestID); v != nil {
			if s, ok := v.(string); ok && s != "" {
				rid = s
			}
		}
		if rid == "" {
			if md, ok := metadata.FromIncomingContext(ctx); ok {
				if vals := md.Get(common.MetaRequestID); len(vals) > 0 {
					rid = vals[0]
				}
			}
		}
		if rid != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, common.MetaRequestID, rid)
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func RequestIDServerUnary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		// 优先从 metadata 中取 request id；没有则生成一个新的
		rid := ""
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if vals := md.Get(common.MetaRequestID); len(vals) > 0 {
				rid = vals[0]
			}
		}
		if rid == "" {
			rid = common.New()
		}
		// 同时写入本地私有 key 和 string key，便于业务代码和下游 gRPC 调用读取
		ctx = context.WithValue(ctx, ctxKey(common.CtxKeyRequestID), rid)
		ctx = context.WithValue(ctx, common.CtxKeyRequestID, rid)
		return handler(ctx, req)
	}
}

func RequestIDFromCtx(ctx context.Context) string {
	if v := ctx.Value(ctxKey(common.CtxKeyRequestID)); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
