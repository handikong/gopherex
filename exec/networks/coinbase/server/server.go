package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gopherex.com/exec/networks/client"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	c := client.Client{
		URL:      "wss://advanced-trade-ws.coinbase.com",
		Products: []string{"BTC-USD", "ETH-USD"},
		OnRawMessage: func(b []byte) {
			// M0：先原样打印，后面再解析/归一化
			log.Printf("recv: %s", b)
		},
	}

	// 运行直到 ctx 取消
	if err := c.Run(ctx, 10*time.Second); err != nil && ctx.Err() == nil {
		log.Printf("client exit: %v", err)
	}
}
