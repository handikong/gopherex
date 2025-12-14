package interceptor

import (
	"context"

	"github.com/sony/gobreaker/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"gopherex.com/pkg/metrics"
	"gopherex.com/pkg/ratelimit"
	"gopherex.com/pkg/xerr"
)

// UnaryClientInterceptor：按 method 使用熔断器
func CiruiteBreakUnaryClient(mgr *ratelimit.Manager, serviceName string) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		cb := mgr.Get(method)

		_, err := cb.Execute(func() (struct{}, error) {
			return struct{}{}, invoker(ctx, method, req, reply, cc, opts...)
		})

		// 熔断器拒绝：直接 fail-fast（不要继续打下游）
		if err == gobreaker.ErrOpenState || err == gobreaker.ErrTooManyRequests { // v2.3.0 提供这两个错误 :contentReference[oaicite:5]{index=5}
			metrics.CBRejectTotal.WithLabelValues(serviceName, method, "open").Inc()
			metrics.CBState.WithLabelValues(serviceName, method, "open").Set(1)

			return xerr.New(codes.Unavailable, "circuit breaker open")
		}

		// 其它：透传真实调用错误（由调用方/网关做映射/降级）
		return err
	}
}
