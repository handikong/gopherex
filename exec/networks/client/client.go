package client

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math/rand"
	"time"

	"github.com/coder/websocket"
)

type Client struct {
	URL          string
	Products     []string
	OnRawMessage func([]byte)
}

// 消息接受
type subscribeMsg struct {
	ProductIDs []string `json:"product_ids"`
	Type       string   `json:"type"`
	Channel    string   `json:"channel"`
	JWT        string   `json:"jwt,omitempty"`
}

func (c *Client) Run(ctx context.Context, stableReset time.Duration) error {
	backoff := 200 * time.Millisecond
	maxBackoff := 10 * time.Second
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	for ctx.Err() == nil {
		//  链接服务器
		dctx, cancelFunc := context.WithTimeout(ctx, time.Second*5)
		dial, _, err := websocket.Dial(dctx, c.URL, nil)
		cancelFunc()
		if err != nil {
			tSleep := jitter(rng, backoff)
			log.Printf("[dial] fail: %v; retry in %v", err, tSleep)
			if !sleepCtx(dctx, tSleep) {
				return err
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		start := time.Now()
		err = c.server(ctx, dial)
		_ = dial.CloseNow()
		if time.Since(start) >= stableReset {
			backoff = 200 * time.Millisecond
		}

		if err != nil && !errors.Is(err, context.Canceled) {
			cs := websocket.CloseStatus(err)
			log.Printf("[conn] end: err=%v closeStatus=%v", err, cs)
		}
	}
	return ctx.Err()
}

func (c *Client) server(ctx context.Context, conn *websocket.Conn) error {
	if err := writeJSON(ctx, conn, subscribeMsg{
		Type:    "subscribe",
		Channel: "heartbeats",
	}); err != nil {
		return err
	}
	if err := writeJSON(ctx, conn, subscribeMsg{
		Type:       "subscribe",
		Channel:    "ticker",
		ProductIDs: c.Products,
	}); err != nil {
		return err
	}

	errCh := make(chan error, 1)
	// 先定义一个读的请求
	go func() {
		for {
			rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			_, msg, err := conn.Read(rctx)
			cancel()
			if err != nil {
				errCh <- err
				return
			}
			if c.OnRawMessage != nil {
				c.OnRawMessage(msg)
			}
		}
	}()
	// 低频 ping：可选探活（心跳主要靠 heartbeats）。
	pingT := time.NewTicker(20 * time.Second)
	defer pingT.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = conn.Close(websocket.StatusNormalClosure, "bye")
			return ctx.Err()
		case err := <-errCh:
			return err
		case <-pingT.C:
			pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err := conn.Ping(pctx)
			cancel()
			if err != nil {
				return err
			}
		}

	}
}

func jitter(rng *rand.Rand, d time.Duration) time.Duration {
	// 0.5x ~ 1.5x
	f := 0.5 + rng.Float64()
	return time.Duration(float64(d) * f)
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
func writeJSON(ctx context.Context, conn *websocket.Conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, b)
}
