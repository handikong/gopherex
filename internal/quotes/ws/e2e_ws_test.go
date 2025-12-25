package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"gopherex.com/internal/quotes/kline"
)

func TestWSHub_E2E_AggToClient(t *testing.T) {
	// 1) WS server
	hub := NewHub()
	srv := NewServer(hub)

	mux := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" {
			srv.ServeWS(w, r)
			return
		}
		w.WriteHeader(404)
	}))
	defer mux.Close()

	wsURL := "ws" + strings.TrimPrefix(mux.URL, "http") + "/ws"

	c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial err=%v", err)
	}
	defer c.Close()

	// 2) subscribe topic
	sub := ClientMsg{Type: "sub", Topics: []string{"kline:1m:BTC-USDT"}}
	bsub, _ := json.Marshal(sub)
	if err := c.WriteMessage(websocket.TextMessage, bsub); err != nil {
		t.Fatalf("sub write err=%v", err)
	}

	// 3) Aggregator + bridge
	cfg := kline.ShardedAggConfig{
		Shards:        2,
		ReorderWindow: 0, // 测试更确定
		TZOffset:      0,
		FillGaps1m:    true,
		FillGaps1h:    false,
		FillGaps1d:    false,
		InboxSize:     1024,
		DropWhenFull:  false,
	}
	agg, err := kline.NewShardedAggregator(cfg)
	if err != nil {
		t.Fatalf("new agg err=%v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	agg.Run(ctx)

	// bridge goroutine
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range agg.Out() {
			Bridge(hub, ev)
		}
	}()

	// 4) feed trades: minute0 + minute2 (minute1 should be filled)
	_ = agg.OfferTrade(kline.Trade{
		Symbol:   "BTC-USDT",
		PriceStr: "100.00000000",
		SizeStr:  "1.00000000",
		TsUnixMs: 500,
	})
	_ = agg.OfferTrade(kline.Trade{
		Symbol:   "BTC-USDT",
		PriceStr: "110.00000000",
		SizeStr:  "1.00000000",
		TsUnixMs: 120_500,
	})

	// shutdown to flush and close out
	cancel()
	agg.Close()
	<-done

	// 5) read ws messages and find filled minute1 bar
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))

	foundFilled := false
	for i := 0; i < 10; i++ { // 最多读几条
		_, msg, err := c.ReadMessage()
		if err != nil {
			break
		}
		var sm ServerMsg
		if json.Unmarshal(msg, &sm) != nil {
			continue
		}
		if sm.Type != "kline" || sm.Topic != "kline:1m:BTC-USDT" {
			continue
		}
		// minute1: [60000,120000), OHLC 应该等于上一根 close=100，V=0 Count=0
		if sm.Bar.StartMs == 60_000 && sm.Bar.EndMs == 120_000 &&
			sm.Bar.Open == "100.00000000" && sm.Bar.High == "100.00000000" &&
			sm.Bar.Low == "100.00000000" && sm.Bar.Close == "100.00000000" &&
			sm.Bar.Volume == "0.00000000" && sm.Bar.Count == 0 {
			foundFilled = true
			break
		}
	}

	if !foundFilled {
		t.Fatalf("did not receive filled 1m bar for minute1")
	}
}
