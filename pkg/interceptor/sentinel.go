package interceptor

import (
	"context"

	sentinels "github.com/alibaba/sentinel-golang/api"
	"github.com/alibaba/sentinel-golang/core/base"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gopherex.com/pkg/logger"
)

func SentinelUnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// 1. 获取资源名称，通常使用 gRPC 的 FullMethod (例如: /user.UserService/Login)
		resourceName := info.FullMethod
		// 2. Sentinel 入口 (Entry)
		// 这里的 sentinel.Entry 会检查所有的规则（限流、熔断等）
		// 如果被拒绝，blockError 会非空
		entry, blockError := sentinels.Entry(resourceName, sentinels.WithTrafficType(base.Inbound))

		if blockError != nil {
			// 用 zap 日志，并返回 ResourceExhausted 错误
			// 打印详细的阻塞信息，帮助调试
			logger.Warn(ctx, "request blocked by sentinel",
				zap.String("method", resourceName),
				zap.String("blockType", blockError.BlockType().String()),
				zap.String("blockMsg", blockError.Error()),
			)
			// 返回 ResourceExhausted 状态码，告诉客户端"资源耗尽/请求过多"
			return nil, status.Error(codes.ResourceExhausted, "service is busy, please try again later")
		}

		// 4. 确保在函数返回前退出 Sentinel Entry
		// 这一步非常关键，它负责统计请求耗时、成功/失败状态，用于后续的熔断判断
		defer entry.Exit()

		// 5. 执行真正的业务逻辑
		resp, err := handler(ctx, req)

		// 6. 如果业务执行出错，需要记录给 Sentinel，以便触发熔断
		// 重要：只记录系统错误（如数据库连接失败、超时等），不记录业务逻辑错误
		// 业务错误（如账号不存在、密码错误）不应该触发熔断器
		if err != nil {
			// 检查是否是系统错误（需要触发熔断的错误）
			if isSystemError(err) {
				sentinels.TraceError(entry, err)
			}
			// 业务错误不记录，让它们正常返回给客户端
		}

		return resp, err
	}
}

// isSystemError 判断是否是系统错误（需要触发熔断的错误）
// 业务错误（如账号不存在、密码错误）不应该触发熔断器
func isSystemError(err error) bool {
	if err == nil {
		return false
	}

	// 检查 gRPC status code
	st, ok := status.FromError(err)
	if !ok {
		// 如果不是 gRPC 错误，默认认为是系统错误（保守策略）
		return true
	}

	// 只记录系统错误，不记录业务逻辑错误
	switch st.Code() {
	case codes.Internal: // 内部错误（数据库错误等）
		return true
	case codes.Unavailable: // 服务不可用
		return true
	case codes.DeadlineExceeded: // 超时
		return true
	case codes.ResourceExhausted: // 资源耗尽（如连接池耗尽）
		return true
	case codes.DataLoss: // 数据丢失
		return true
	case codes.Unknown: // 未知错误
		return true
	default:
		// 业务错误不触发熔断：
		// - codes.InvalidArgument: 参数错误
		// - codes.NotFound: 记录不存在
		// - codes.Unauthenticated: 认证失败（密码错误等）
		// - codes.PermissionDenied: 权限不足
		// - codes.AlreadyExists: 记录已存在
		// - codes.FailedPrecondition: 前置条件不满足
		return false
	}
}
