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

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopherex.com/internal/quotes/gateway"
	"gopherex.com/internal/quotes/ws"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	hub := ws.NewHub()
	wss := ws.NewServer(ctx, hub)

	// NATS broker
	natsURL := getenv("NATS_URL", "nats://127.0.0.1:4222")
	broker, err := gateway.NewNatsBroker(natsURL)
	if err != nil {
		log.Fatalf("connect nats err: %v", err)
	}
	defer broker.Close()

	// Gateway：订阅所有 kline
	gw := gateway.NewGateway(hub, wss, broker)
	go func() {
		// 订阅所有 kline：kline:> => NATS subject kline.>
		if err := gw.Run(ctx, []string{"kline:>"}); err != nil && ctx.Err() == nil {
			log.Printf("gateway run err: %v", err)
		}
	}()

	//prometheus.MustRegister(
	//	collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	//	collectors.NewGoCollector(),
	//)

	// http server
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wss.ServeWS)
	mux.Handle("/metrics", promhttp.Handler())

	// pprof
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	addr := getenv("WS_ADDR", ":8080")
	srv := &http.Server{Addr: addr, Handler: mux}

	go func() {
		log.Printf("ws-gateway listening on %s, nats=%s", addr, natsURL)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
