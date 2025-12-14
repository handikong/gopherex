package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gopherex.com/pkg/common"
	"gopherex.com/pkg/logger"
)

func Recover() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				rid := common.RequestIDFromGin(c)
				logger.Error(c, "http panic",
					zap.String("request_id", rid),
					zap.String("method", c.Request.Method),
					zap.String("path", c.Request.URL.Path),
					zap.Any("panic", err),
					zap.ByteString("stack", debug.Stack()),
				)
				common.Fail(c, http.StatusInternalServerError, 5000000, "internal error")
				c.Abort()
			}
		}()
	}
}
