package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
	"gopherex.com/pkg/common"
)

func ReqId() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader(common.HeaderRequestID)
		if rid == "" {
			rid = common.New()
		}
		c.Set(common.CtxKeyRequestID, rid)
		// 将 request id 写入 request context，方便后续 gRPC 调用链读取
		ctx := context.WithValue(c.Request.Context(), common.CtxKeyRequestID, rid)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
