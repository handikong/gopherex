package xerr

import "fmt"

// 常用错误码定义
const (
	OK                 = 200
	ServerCommonError  = 500
	RequestParamsError = 400
	DbError            = 501
	RecordNotFound     = 404
)

type CodeError struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func (e *CodeError) Error() string {
	return fmt.Sprintf("ErrCode:%d, Msg:%s", e.Code, e.Msg)
}

func New(code int, msg string) error {
	return &CodeError{Code: code, Msg: msg}
}

func NewErrCode(code int) error {
	return &CodeError{Code: code, Msg: MapErrMsg(code)}
}

func MapErrMsg(code int) string {
	switch code {
	case ServerCommonError:
		return "服务器开小差了"
	case RequestParamsError:
		return "参数错误"
	case DbError:
		return "数据库繁忙"
	case RecordNotFound:
		return "记录不存在"
	default:
		return "未知错误"
	}
}
