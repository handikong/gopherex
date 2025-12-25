package binance

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"gopherex.com/internal/quotes/datasource/model"
	"gopherex.com/internal/quotes/mdsource"
)

type Source struct {
	BaseURL string   // e.g. wss://stream.binance.com:9443
	Streams []string // e.g. []{"btcusdt@aggTrade","ethusdt@aggTrade"}

	ReadLimit int64
	PongWait  time.Duration
	WriteWait time.Duration
	Dialer    *websocket.Dialer
}

func NewSource(streams []string) *Source {
	return &Source{
		BaseURL:   "wss://stream.binance.com:9443",
		Streams:   streams,
		ReadLimit: 1 << 20,
		PongWait:  60 * time.Second,
		WriteWait: 2 * time.Second,
		Dialer:    websocket.DefaultDialer,
	}
}

func (s *Source) Name() string { return "binance" }

func (s *Source) Run(ctx context.Context, out chan<- model.Trade) error {
	url := s.BaseURL + "/stream?streams=" + strings.Join(s.Streams, "/")

	c, _, err := s.Dialer.DialContext(ctx, url, nil)
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

	// 你原来 main 里的 writeMu + ping->pong 逻辑：原封不动搬进来
	var writeMu sync.Mutex
	c.SetPingHandler(func(appData string) error {
		// 拷贝 payload（你原注释也写了）
		b := []byte(appData)
		cp := make([]byte, len(b))
		copy(cp, b)

		writeMu.Lock()
		defer writeMu.Unlock()
		_ = c.SetWriteDeadline(time.Now().Add(s.WriteWait))
		return c.WriteControl(websocket.PongMessage, cp, time.Now().Add(s.WriteWait))
	})

	for ctx.Err() == nil {
		_, msg, err := c.ReadMessage()
		if err != nil {
			return err
		}
		tr, err := ParseBinanceAggTradeCombined(msg)
		if err != nil {
			continue
		}
		select {
		case out <- tr:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return ctx.Err()
}

var _ mdsource.Source = (*Source)(nil)
