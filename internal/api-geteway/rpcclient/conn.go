package rpcclient

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"gopherex.com/pkg/common"
)

type ctxKey string

const ctxKeyReqID = ctxKey(common.CtxKeyRequestID)

func WithRequestID(ctx context.Context, rid string) context.Context {
	return context.WithValue(ctx, ctxKeyReqID, rid)
}

// client interceptor：把 ctx 里的 rid 写进 outgoing metadata
func RequestIDUnaryClient() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if v := ctx.Value(ctxKeyReqID); v != nil {
			if rid, ok := v.(string); ok && rid != "" {
				ctx = metadata.AppendToOutgoingContext(ctx, common.MetaRequestID, rid)
			}
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}
