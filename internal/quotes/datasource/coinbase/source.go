package coinbase

import (
	"context"
	"time"

	"github.com/gorilla/websocket"
	"gopherex.com/internal/quotes/mdsource"

	"gopherex.com/internal/quotes/datasource/model"
)

type Source struct {
	URL        string   // e.g. wss://advanced-trade-ws.coinbase.com
	ProductIDs []string // e.g. BTC-USD, ETH-USD

	ReadLimit int64
	PongWait  time.Duration
	WriteWait time.Duration
	Dialer    *websocket.Dialer
}

func NewSource(productIDs []string) *Source {
	return &Source{
		URL:        "wss://advanced-trade-ws.coinbase.com",
		ProductIDs: productIDs,
		ReadLimit:  1 << 20,
		PongWait:   60 * time.Second,
		WriteWait:  5 * time.Second,
		Dialer:     websocket.DefaultDialer,
	}
}

func (s *Source) Name() string { return "coinbase" }

// Run: 一次连接生命周期（不做重连；重连交给 Runner）
func (s *Source) Run(ctx context.Context, out chan<- model.Trade) error {
	c, _, err := s.Dialer.DialContext(ctx, s.URL, nil)
	if err != nil {
		return err
	}
	defer c.Close()

	c.SetReadLimit(s.ReadLimit)
	_ = c.SetReadDeadline(time.Now().Add(s.PongWait))
	c.SetPongHandler(func(string) error {
		_ = c.SetReadDeadline(time.Now().Add(s.PongWait))
		return nil
	})

	// subscribe（你之前应该也有，这里保持最小逻辑）
	sub := map[string]any{
		"type":        "subscribe",
		"channel":     "market_trades",
		"product_ids": s.ProductIDs,
	}
	_ = c.SetWriteDeadline(time.Now().Add(s.WriteWait))
	if err := c.WriteJSON(sub); err != nil {
		return err
	}

	for ctx.Err() == nil {
		_, msg, err := c.ReadMessage()
		if err != nil {
			return err
		}

		trades, err := ParseCoinbaseMarketTrades(msg)
		if err != nil {
			continue
		}
		for _, t := range trades {
			select {
			case out <- t:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return ctx.Err()
}

var _ mdsource.Source = (*Source)(nil)
