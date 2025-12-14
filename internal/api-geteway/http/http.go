package http

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	ginprom "github.com/zsais/go-gin-prometheus"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"gopherex.com/internal/api-geteway/http/router"
	middleware2 "gopherex.com/pkg/middleware"
	"gopherex.com/pkg/ratelimit"
)

func NewRouter(addr string) *http.Server {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// 限流
	store := ratelimit.NewStore(1000, 2000, 10*time.Minute) // 50 rps，突发 100
	store.StartJanitor(ctx, time.Minute)
	// 监控
	r := gin.New()
	p := ginprom.NewPrometheus("gopherex")
	p.Use(r)
	r.Use(
		otelgin.Middleware("api-gateway"),
		middleware2.ReqId(),
		cors.Default(),
		middleware2.Recover(),
		middleware2.RateLimit(store),
	)
	api := r.Group("/api")
	router.User(api)
	router.Waller(api)
	s := &http.Server{
		Addr:           addr,
		Handler:        r,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	return s
}
