package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"gopherex.com/exec/networks/coinbase"
	"gopherex.com/exec/networks/md"
	"gopherex.com/exec/networks/wsapi"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	hub := md.NewHub(2048)

	agg := md.NewKlineAgg([]time.Duration{time.Second, time.Minute}, func(k md.KlineEvent) {
		hub.Publish(md.Topic{Exchange: k.Exchange, Symbol: k.Symbol, Interval: k.Interval}, k)
	})
	agg.EmitEvery = 200 * time.Millisecond

	evCh := make(chan md.Event, 8192)

	client := md.WSClient{
		Adapter:     coinbase.Adapter{},
		Products:    []string{"BTC-USD", "ETH-USD"},
		StableReset: 10 * time.Second,
		PingEvery:   20 * time.Second,
		ReadTimeout: 30 * time.Second,
		OnEvent: func(ev md.Event) {
			select {
			case evCh <- ev:
			default:
				// M0：满了就丢，避免读循环被阻塞
			}
		},
	}

	// 消费事件 -> 喂给聚合器
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-evCh:
				if t, ok := ev.(md.TradeEvent); ok {
					agg.OnTrade(t)
				}
			}
		}
	}()

	// WS 推送服务
	mux := http.NewServeMux()
	mux.Handle("/ws", wsapi.NewServer(hub))
	srv := &http.Server{Addr: ":8081", Handler: mux}

	go func() {
		log.Println("ws push listen :8081  e.g. ws://127.0.0.1:8081/ws?exchange=coinbase&symbol=BTC-USD&interval=1s")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("ListenAndServe: %v", err)
		}
	}()

	// 启动 coinbase ingest
	go func() {
		_ = client.Run(ctx)
	}()

	<-ctx.Done()
	ctx2, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx2)

}
