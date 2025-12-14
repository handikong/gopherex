package xerr

import (
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

/*
Biz/HTTP 常用码（你原来的保留）
注意：这组更多是 “业务码/内部码”，HTTP Status 建议用 HTTPStatus() 决定
*/
const (
	OK                 = 200
	RequestParamsError = 400
	RecordNotFound     = 404

	ServerCommonError = 500
	DbError           = 501
)

type CodeError struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// Stack 保存调用栈PC
type Stack []uintptr

func callers(skip int) Stack {
	pcs := make([]uintptr, 64)
	n := runtime.Callers(skip, pcs)
	return pcs[:n]
}

func (s Stack) String() string {
	var b strings.Builder
	frames := runtime.CallersFrames([]uintptr(s))
	for {
		f, more := frames.Next()
		b.WriteString(fmt.Sprintf("%s\n\t%s:%d\n", f.Function, f.File, f.Line))
		if !more {
			break
		}
	}
	return b.String()
}

// Error：统一错误结构（Gin/HTTP + gRPC 都能用）
type Error struct {
	Code    any
	Message string
	Cause   error
	Stack   Stack
}

func (e *Error) Error() string {
	// 用 %v：兼容 int / codes.Code / string 等
	return fmt.Sprintf("biz=%v msg=%s", e.Code, e.Message)
}
func (e *Error) Unwrap() error { return e.Cause }

// New 创建一个 xerr（会捕获栈）
func New(code any, msg string) *Error {
	// skip=3：New -> callers -> runtime.Callers，基本能定位到调用方
	return &Error{Code: code, Message: msg, Stack: callers(3)}
}

// Wrap 包装一个底层 err（会捕获栈）
func Wrap(err error, code any, msg string) *Error {
	return &Error{Code: code, Message: msg, Cause: err, Stack: callers(3)}
}

// Newf/Wrapf（可选便捷）
func Newf(code any, format string, args ...any) *Error {
	return New(code, fmt.Sprintf(format, args...))
}
func Wrapf(err error, code any, format string, args ...any) *Error {
	return Wrap(err, code, fmt.Sprintf(format, args...))
}

// As：取到最外层/链路中最近的 *xerr.Error
func As(err error) (*Error, bool) {
	var xe *Error
	return xe, errors.As(err, &xe)
}

/*
========================
gRPC 兼容：关键点在这里
========================
实现 GRPCStatus() 让 gRPC 能识别错误码，而不是默认 Unknown。
*/
func (e *Error) GRPCStatus() *status.Status {
	// 1) 如果 Code 是 gRPC codes.Code，优先使用它
	if c, ok := asGRPCCode(e.Code); ok {
		return status.New(c, e.Message)
	}

	// 2) 如果 Cause 本身已经是一个 gRPC status error，尽量保留它的 code（但 message 用我们自己的）
	if e.Cause != nil {
		if st, ok := status.FromError(e.Cause); ok {
			// 保留 st.Code()，message 用 e.Message（更符合你统一 message 的目标）
			return status.New(st.Code(), e.Message)
		}
	}

	// 3) 否则默认 Internal（不要让框架回 Unknown）
	return status.New(codes.Internal, e.Message)
}

// GRPCCode：拿到这个错误最终应该呈现的 gRPC code（用于拦截器做分级）
func (e *Error) GRPCCode() codes.Code {
	if c, ok := asGRPCCode(e.Code); ok {
		return c
	}
	if e.Cause != nil {
		if st, ok := status.FromError(e.Cause); ok {
			return st.Code()
		}
	}
	return codes.Internal
}

/*
========================
HTTP 兼容：用于网关决定 HTTP Status
========================
*/
func (e *Error) HTTPStatus() int {
	// 1) Code 直接是 httpStatus（如果你愿意这样用）
	if hs, ok := asHTTPStatus(e.Code); ok {
		return hs
	}

	// 2) Code 是 gRPC code：映射到 HTTP
	if gc, ok := asGRPCCode(e.Code); ok {
		return httpStatusFromGRPC(gc)
	}

	// 3) Code 是业务 int：映射常见 HTTP（可按你规范调整）
	if bc, ok := asBizCode(e.Code); ok {
		return httpStatusFromBiz(bc)
	}

	// 4) Cause 如果是 gRPC status：映射 HTTP
	if e.Cause != nil {
		if st, ok := status.FromError(e.Cause); ok {
			return httpStatusFromGRPC(st.Code())
		}
	}

	return http.StatusInternalServerError
}

// BizCode：尽量返回 int 的业务码（Gin 返回用）
func (e *Error) BizCode() int {
	if bc, ok := asBizCode(e.Code); ok {
		return bc
	}
	// Code 是 gRPC code 的场景：给一个可用的默认映射（你也可以在网关映射表里覆盖）
	if gc, ok := asGRPCCode(e.Code); ok {
		return bizCodeFromGRPC(gc)
	}
	// 没法识别就给 500
	return ServerCommonError
}

/*
========================
internal helpers
========================
*/

func asBizCode(v any) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case int32:
		return int(t), true
	case int64:
		return int(t), true
	default:
		return 0, false
	}
}

func asHTTPStatus(v any) (int, bool) {
	// 允许你传 http.StatusTooManyRequests 这类常量
	if i, ok := asBizCode(v); ok && i >= 100 && i <= 599 {
		return i, true
	}
	return 0, false
}

func asGRPCCode(v any) (codes.Code, bool) {
	switch t := v.(type) {
	case codes.Code:
		return t, true
	case int:
		// 如果你有人直接传 int(8) 这种，也尽量兼容（合法范围 0..16）
		if t >= 0 && t <= 16 {
			return codes.Code(t), true
		}
	}
	return codes.Internal, false
}

func httpStatusFromBiz(biz int) int {
	// 你可以按你项目规范调整：比如所有业务错误都返回 200
	// 这里给一个更“标准 HTTP”的映射
	switch biz {
	case RequestParamsError:
		return http.StatusBadRequest
	case RecordNotFound:
		return http.StatusNotFound
	case OK:
		return http.StatusOK
	default:
		// 5xx 类业务码
		if biz >= 500 {
			return http.StatusInternalServerError
		}
		return http.StatusBadRequest
	}
}

func httpStatusFromGRPC(c codes.Code) int {
	// 常见映射（可按你项目规范覆盖）
	switch c {
	case codes.OK:
		return http.StatusOK
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout
	case codes.Unavailable:
		return http.StatusServiceUnavailable
	case codes.InvalidArgument:
		return http.StatusBadRequest
	case codes.NotFound:
		return http.StatusNotFound
	case codes.Unauthenticated:
		return http.StatusUnauthorized
	case codes.PermissionDenied:
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}

func bizCodeFromGRPC(c codes.Code) int {
	// 给一个默认兜底映射：你可以在网关层用更精确的 biz_code 表覆盖
	switch c {
	case codes.ResourceExhausted:
		return 1003001 // 你之前用的“请求过于频繁”
	case codes.DeadlineExceeded:
		return 1003002 // 建议：超时
	case codes.Unavailable:
		return 1003003 // 建议：服务不可用
	case codes.InvalidArgument:
		return RequestParamsError
	case codes.NotFound:
		return RecordNotFound
	default:
		return ServerCommonError
	}
}
