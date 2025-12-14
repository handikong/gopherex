package common

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	HeaderRequestID = "X-Request-Id"
	MetaRequestID   = "x-request-id" // grpc metadata key 用小写
	CtxKeyRequestID = "request_id"
)

func New() string { return uuid.NewString() }

// 获取id
func RequestIDFromGin(c *gin.Context) string {
	if v, ok := c.Get(CtxKeyRequestID); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
