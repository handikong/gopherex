package common

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gopherex.com/pkg/logger"
	"gopherex.com/pkg/xerr"
)

// 定义http返回格式
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

func Success(ctx *gin.Context, data interface{}) {
	ctx.JSON(http.StatusOK, Response{
		Code:    http.StatusOK,
		Message: http.StatusText(http.StatusOK),
		Data:    data,
	})
}
func Fail(c *gin.Context, httpStatus int, code int, message string) {
	c.JSON(httpStatus, Response{
		Code:    code,
		Message: message,
		Data:    nil,
	})
}

func FailLogged(c *gin.Context, httpStatus int, code int, msg string, err error) {
	logger.Warn(c, "http error",
		zap.String("request_id", RequestIDFromGin(c)),
		zap.String("method", c.Request.Method),
		zap.String("path", c.Request.URL.Path),
		zap.Int("biz_code", code),
		zap.String("message", msg),
		zap.Error(err),
		zap.ByteString("stack", debug.Stack()), // ✅ 非 panic 也打堆栈（按你要求）
	)
	Fail(c, httpStatus, code, msg)
}

// 网关对外只回 biz_code + message（data=null）
// 但网关日志必须记录：grpc_code + err + stack
func FailFromGRPC(c *gin.Context, err error) {
	// rid := RequestIDFromGin(c)

	st, _ := status.FromError(err)
	bizCode, msg, httpStatus := mapGrpcToHTTPBiz(st.Code(), st.Message())
	Fail(c, httpStatus, int(bizCode), msg)
}

func mapGrpcToHTTPBiz(gc codes.Code, grpcMsg string) (biz int32, msg string, httpStatus int) {
	// 你可以用固定文案（更安全），不要直接透出 grpcMsg（可能包含内部信息）
	switch gc {
	case codes.InvalidArgument:
		return 1001001, "参数错误", http.StatusBadRequest
	case codes.Unauthenticated:
		return 1002001, "未登录", http.StatusUnauthorized
	case codes.PermissionDenied:
		return 1002003, "无权限", http.StatusForbidden
	case codes.ResourceExhausted:
		return 1003001, "请求过于频繁", http.StatusTooManyRequests
	case codes.Unavailable:
		return 1004001, "服务繁忙", http.StatusServiceUnavailable
	default:
		return 5000000, "internal error", http.StatusInternalServerError
	}
}

func WrapPreserveCode(err error, fallback codes.Code, msg string) error {
	if err == nil {
		return nil
	}
	if st, ok := status.FromError(err); ok {
		return xerr.Wrap(err, st.Code(), msg)
	}
	return xerr.Wrap(err, fallback, msg)
}
