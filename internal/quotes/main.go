// cmd/md-gateway/main.go (示例)
package main

import (
	"context"
	"log"
	"net/http"
	"net/http/pprof"
	"os/signal"
	"syscall"
	"time"

	"gopherex.com/internal/quotes/datasource/coinbase"
	"gopherex.com/internal/quotes/gateway"
	"gopherex.com/internal/quotes/kline"
	"gopherex.com/internal/quotes/mdsource"
	"gopherex.com/internal/quotes/ws"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	hub := ws.NewHub()
	wss := ws.NewServer(ctx, hub)

	cfg := kline.ShardedAggConfig{
		Shards:        8,
		ReorderWindow: 2 * time.Second,
		TZOffset:      0,
		FillGaps1m:    true,
		FillGaps1h:    true,
		FillGaps1d:    true,
		InboxSize:     8192,
		// v0 强烈建议先 true，防止全链路卡死
		DropWhenFull: true,
	}
	agg, err := kline.NewShardedAggregator(cfg)
	if err != nil {
		panic(err)
	}

	agg.Run(ctx)

	broker, _ := gateway.NewNatsBroker("nats://127.0.0.1:4222")
	defer broker.Close()

	// bridge：agg -> hub
	go func() {
		for ev := range agg.Out() {
			topic, payload, _ := ws.EncodeEvent(ev) // 你把 Bridge 里“算 topic + marshal”那段抽出来
			_ = broker.Publish(ctx, topic, payload)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wss.ServeWS)

	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	srv := &http.Server{Addr: ":8080", Handler: mux}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	// data sources
	runner := mdsource.NewRunner(
		coinbase.NewSource([]string{"BTC-USD", "ETH-USD"}),
		// binance.NewSource([]string{"btcusdt@aggTrade", "ethusdt@aggTrade"}),
	)
	runner.Run(ctx)

	// runner -> agg
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case tr, ok := <-runner.Out:
				if !ok {
					return
				}
				agg.OfferTrade(tr) // DropWhenFull=true 时不会卡死
			}
		}
	}()
	go func() {
		log.Println("pprof listening on http://127.0.0.1:6060/debug/pprof/")
		if err := http.ListenAndServe("127.0.0.1:6060", nil); err != nil {
			log.Println("pprof server error:", err)
		}
	}()

	log.Println("listening on :8080")
	<-ctx.Done() // 等信号

	// 优雅退出：先关 http，再关 agg（顺序不绝对，但要有超时）
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)

	agg.Close()
}
