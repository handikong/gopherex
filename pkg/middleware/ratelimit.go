package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
	"gopherex.com/pkg/common"
	"gopherex.com/pkg/interceptor"
	"gopherex.com/pkg/logger"
	"gopherex.com/pkg/ratelimit"
)

type RateLimitConfig struct {
	Rate  rate.Limit
	Burst int
	TTL   time.Duration
}

func RateLimit(store *ratelimit.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		route := c.FullPath()
		if route == "" {
			route = c.Request.URL.Path
		}
		key := c.ClientIP() + ":" + route

		if !store.Allow(key) {
			// 限流属于“可控拒绝”，不要打堆栈（压测会炸日志）
			logger.Warn(c, "http rate limited",
				zap.String("request_id", interceptor.RequestIDFromCtx(c)),
				zap.String("ip", c.ClientIP()),
				zap.String("route", route),
			)
			// 你要求：错误返回只要 code/message/data=null
			common.Fail(c, http.StatusTooManyRequests, 1003001, "请求过于频繁")
			c.Abort()
			return
		}
		c.Next()
	}
}
