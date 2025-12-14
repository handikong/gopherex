package interceptor

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"gopherex.com/pkg/metrics"
	"gopherex.com/pkg/ratelimit"
	"gopherex.com/pkg/xerr"
)

func RateLimitByMethodUnary(store *ratelimit.Store, serviceName string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		// key 只用调用路径（FullMethod），不区分 uid
		key := info.FullMethod
		if !store.Allow(key) {
			metrics.RateLimitBlockTotal.WithLabelValues(serviceName, info.FullMethod, "token_bucket").Inc()

			// 限流错误语义：ResourceExhausted
			return nil, xerr.New(codes.ResourceExhausted, "rate limited")
		}
		return handler(ctx, req)
	}
}
