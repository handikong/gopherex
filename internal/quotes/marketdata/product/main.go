package main

import (
	"context"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
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

	// pprof（可选）
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
		log.Println("marketdata pprof on http://127.0.0.1:6061/debug/pprof/")
		_ = http.ListenAndServe("127.0.0.1:6061", mux)
	}()

	// agg
	cfg := kline.ShardedAggConfig{
		Shards:        8,
		ReorderWindow: 2 * time.Second,
		TZOffset:      0,
		FillGaps1m:    true,
		FillGaps1h:    true,
		FillGaps1d:    true,
		InboxSize:     8192,
		DropWhenFull:  true,
	}
	agg, err := kline.NewShardedAggregator(cfg)
	if err != nil {
		panic(err)
	}
	agg.Run(ctx)

	// 通过run生成coinbase的数据
	runner := mdsource.NewRunner(
		coinbase.NewSource([]string{"BTC-USD", "ETH-USD"}),
	)
	// 最终生层的数据都Out chan kline.Trade 到这个接口
	go runner.Run(ctx)
	// 将数据从 runner 取出  传入agg
	// runner -> agg
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case tr, ok := <-runner.Get():
				if !ok {
					return
				}
				// 将数据传递到agg
				agg.OfferTrade(tr)
			}
		}
	}()

	// NATS broker
	natsURL := getenv("NATS_URL", "nats://127.0.0.1:4222")
	broker, err := gateway.NewNatsBroker(natsURL)
	if err != nil {
		log.Fatalf("connect nats err: %v", err)
	}
	defer broker.Close()

	// agg.Out -> NATS Publish
	// 从agg 传入 NATS
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-agg.Out():
				if !ok {
					return
				}
				topic, payload, err := ws.EncodeEvent(ev.Bar)
				if err != nil {
					// v0：打日志即可；后面你可以做 counter 指标
					log.Printf("EncodeEvent err: %v", err)
					continue
				}
				if err := broker.Publish(ctx, topic, payload); err != nil {
					log.Printf("nats publish err: %v", err)
				}
			}
		}
	}()

	log.Println("marketdata running, publishing to", natsURL)
	<-ctx.Done()
	agg.Close()
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
