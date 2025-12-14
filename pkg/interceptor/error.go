package interceptor

import (
	"context"
	"runtime/debug"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gopherex.com/pkg/logger"
	"gopherex.com/pkg/xerr"
)

func ErrorUnary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		resp, err := handler(ctx, req)
		if err == nil {
			return resp, nil
		}
		st, ok := status.FromError(err)
		if ok && st.Code() == codes.ResourceExhausted {
			// 限流是可控拒绝：不打堆栈（最多 debug/warn 一条即可）
			return nil, err
		}

		rid := RequestIDFromCtx(ctx)

		// 业务错误：记录 xerr.Stack
		if xe, ok := xerr.As(err); ok {
			logger.Warn(ctx, "grpc biz error",
				zap.String("request_id", rid),
				zap.String("grpc_method", info.FullMethod),
				zap.Any("biz_code", xe.Code),
				zap.String("message", xe.Message),
				zap.String("stack", xe.Stack.String()),
				zap.Error(xe.Cause),
			)
			return nil, status.Error(mapBizToGrpc(xe.Code), xe.Message)
		}

		// 未知错误：兜底堆栈
		logger.Error(ctx, "grpc unknown error",
			zap.String("request_id", rid),
			zap.String("grpc_method", info.FullMethod),
			zap.Error(err),
			zap.ByteString("stack", debug.Stack()),
		)
		return nil, status.Error(codes.Internal, "internal error")
	}
}

// 示例映射：你按业务码体系完善
func mapBizToGrpc(biz any) codes.Code {
	switch biz {
	case 1001001:
		return codes.InvalidArgument
	case 1002001:
		return codes.Unauthenticated
	case 1002003:
		return codes.PermissionDenied
	default:
		return codes.Unknown
	}
}
