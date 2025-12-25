package md

import (
	"context"
	"errors"
	"log"
	"math/rand"
	"time"

	"github.com/coder/websocket"
)

type WSClient struct {
	Adapter     Adapter
	Products    []string
	OnEvent     func(Event)   // 归一化后事件回调（可接聚合器/Hub）
	OnRaw       func([]byte)  // 可选：调试用，打印原始 JSON
	StableReset time.Duration // 连接存活多久才重置 backoff（避免抖动重连）
	PingEvery   time.Duration // 0 表示不 ping
	ReadTimeout time.Duration // 0 表示不设（一般 Coinbase 有 heartbeats 可设短一点）
}

func (c *WSClient) Run(ctx context.Context) error {
	if c.Adapter == nil {
		return errors.New("nil adapter")
	}
	if c.StableReset == 0 {
		c.StableReset = 10 * time.Second
	}
	if c.PingEvery == 0 {
		c.PingEvery = 20 * time.Second
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = 30 * time.Second
	}

	backoff := 200 * time.Millisecond
	maxBackoff := 10 * time.Second
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	for ctx.Err() == nil {
		// Dial timeout：避免网络黑洞卡死
		dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		conn, _, err := websocket.Dial(dctx, c.Adapter.URL(), nil)
		cancel()
		if err != nil {
			sleep := jitter(rng, backoff)
			log.Printf("[%s][dial] fail: %v; retry in %v", c.Adapter.Name(), err, sleep)
			if !sleepCtx(ctx, sleep) {
				return ctx.Err()
			}
			backoff = minDur(backoff*2, maxBackoff)
			continue
		}

		log.Printf("[%s][dial] connected: %s", c.Adapter.Name(), c.Adapter.URL())
		start := time.Now()

		err = c.serveConn(ctx, conn)

		_ = conn.CloseNow()

		// 连接稳定才重置 backoff，避免“连上马上断→200ms又连”的重连风暴
		if time.Since(start) >= c.StableReset {
			backoff = 200 * time.Millisecond
		}

		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("[%s][conn] end: err=%v closeStatus=%v", c.Adapter.Name(), err, websocket.CloseStatus(err))
		}
	}
	return ctx.Err()
}

func (c *WSClient) serveConn(ctx context.Context, conn *websocket.Conn) error {
	// 1) 连接成功后：发订阅消息（Adapter 定义）
	// 格式化数据 写个第三方
	for _, msg := range c.Adapter.SubscribeMessages(c.Products) {
		wctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := conn.Write(wctx, websocket.MessageText, msg)
		cancel()
		if err != nil {
			return err
		}
	}

	errCh := make(chan error, 1)

	// 2) Reader：持续 Read，然后交给 Adapter.Decode -> emit 事件
	go func() {
		for {
			rctx := ctx
			var cancel func()
			if c.ReadTimeout > 0 {
				rctx, cancel = context.WithTimeout(ctx, c.ReadTimeout)
			} else {
				cancel = func() {}
			}

			_, raw, err := conn.Read(rctx)
			cancel()

			if err != nil {
				errCh <- err
				return
			}
			// 回调函数
			if c.OnRaw != nil {
				c.OnRaw(raw)
			}
			// 解析数据
			_ = c.Adapter.Decode(raw, func(ev Event) {
				if c.OnEvent != nil {
					c.OnEvent(ev) // 注意：回调别阻塞太久，否则会拖慢 Read
				}
			})
		}
	}()

	// 3) Ping：可选探活（如果交易所自带 heartbeats，也可以 PingEvery=0）
	var pingT *time.Ticker
	if c.PingEvery > 0 {
		pingT = time.NewTicker(c.PingEvery)
		defer pingT.Stop()
	}

	for {
		select {
		case <-ctx.Done():
			_ = conn.Close(websocket.StatusNormalClosure, "bye")
			return ctx.Err()

		case err := <-errCh:
			return err

		case <-tickChan(pingT):
			pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err := conn.Ping(pctx)
			cancel()
			if err != nil {
				return err
			}
		}
	}
}

func tickChan(t *time.Ticker) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

func jitter(rng *rand.Rand, d time.Duration) time.Duration {
	f := 0.5 + rng.Float64() // 0.5x~1.5x
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
func minDur(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
