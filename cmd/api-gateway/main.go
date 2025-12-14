package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"gopherex.com/internal/api-geteway/app"
)

func main() {
	// 1. 支持 Ctrl+C / kubernetes 停止信号的 context
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 2. 初始化 App
	gwApp, err := app.New("api-gateway")
	if err != nil {
		log.Fatalf("init gateway-service error: %v", err)
	}
	//  启动服务
	cleanUp := gwApp.StartService(ctx, "api-gateway")
	defer cleanUp()
	srv := gwApp.StartHttp()
	// 4. 启动app
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			// 可以在这里判断是否 ErrServerClosed，简单点直接 fatal 也行
			log.Fatalf("gateway ListenAndServe error: %v", err)

		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("gateway shutdown error: %v", err)
	}
	log.Println("gateway exit")
}
