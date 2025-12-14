package error

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type HTTPError struct {
	Code    int    // HTTP status code
	Message string // 对外可见的信息
	Cause   error  // 原始错误（可选）
}

func (e *HTTPError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return fmt.Sprintf("http error (code=%d)", e.Code)
}

func (e *HTTPError) Unwrap() error { return e.Cause }

func GrpcNew(err error) error {
	return GrpcCovHttp(err)
}

func GrpcCovHttp(err error) error {
	if err == nil {
		return nil
	}

	// 已经是 HTTPError 就别重复包装
	var he *HTTPError
	if errors.As(err, &he) {
		return he
	}

	st, ok := status.FromError(err)
	if !ok {
		// 非 gRPC 错误：当成 500
		return &HTTPError{
			Code:    500,
			Message: "internal server error",
			Cause:   err,
		}
	}

	httpCode := grpcCodeToHTTP(st.Code())

	// 对外 message：你可以按需要做脱敏/隐藏
	msg := st.Message()
	if msg == "" {
		msg = defaultHTTPMessage(httpCode)
	}

	// codes.OK 理论上不该走到这里，如果走到，按 500 处理更合理
	if st.Code() == codes.OK {
		return &HTTPError{
			Code:    500,
			Message: "internal server error",
			Cause:   err,
		}
	}

	return &HTTPError{
		Code:    httpCode,
		Message: msg,
		Cause:   err,
	}
}

func grpcCodeToHTTP(c codes.Code) int {
	switch c {
	case codes.OK:
		return 200

	// 400
	case codes.InvalidArgument:
		return 400

	// 401 / 403
	case codes.Unauthenticated:
		return 401
	case codes.PermissionDenied:
		return 403

	// 404
	case codes.NotFound:
		return 404

	// 409
	case codes.Aborted, codes.AlreadyExists:
		return 409

	// 412
	case codes.FailedPrecondition:
		return 412

	// 416
	case codes.OutOfRange:
		return 416 // 也有人用 400；这里给你更语义化的选项
	// 取消：499（Nginx 常用，标准 HTTP 没有 499，但网关很常见）
	case codes.Canceled:
		return 499

	// 429
	case codes.ResourceExhausted:
		return 429

	// 503 / 504
	case codes.Unavailable:
		return 503
	case codes.DeadlineExceeded:
		return 504

	// 500（服务器内部/未知）
	case codes.Internal, codes.DataLoss, codes.Unknown:
		return 500

	// 502（上游/协议）
	case codes.Unimplemented:
		return 501
	default:
		return 500
	}
}

func defaultHTTPMessage(code int) string {
	switch code {
	case 400:
		return "bad request"
	case 401:
		return "unauthorized"
	case 403:
		return "forbidden"
	case 404:
		return "not found"
	case 409:
		return "conflict"
	case 412:
		return "precondition failed"
	case 416:
		return "range not satisfiable"
	case 429:
		return "too many requests"
	case 499:
		return "client closed request"
	case 500:
		return "internal server error"
	case 501:
		return "not implemented"
	case 503:
		return "service unavailable"
	case 504:
		return "gateway timeout"
	default:
		return "error"
	}
}
