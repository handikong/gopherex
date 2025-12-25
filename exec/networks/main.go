package main

import (
	"context"
	"errors"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/coder/websocket"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	url := "ws://127.0.0.1:8080/ws" // 先连你 Step1 的 echo server
	runWithReconnect(ctx, url)

}

func runWithReconnect(ctx context.Context, url string) {
	// 链接服务器
	backoff := 200 * time.Millisecond
	maxBackoff := 10 * time.Second
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	for ctx.Err() == nil {
		// 链接超时时间
		dCtx, cancelFunc := context.WithTimeout(ctx, time.Second*5)
		c, _, err := websocket.Dial(dCtx, url, &websocket.DialOptions{
			HTTPHeader: http.Header{"Origin": []string{"http://localhost"}}, // 非浏览器客户端可补 Origin
		})
		cancelFunc()
		if err != nil {
			sleep := jitter(rng, backoff)
			log.Printf("[dial] fail: %v; retry in %v", err, sleep)
			if !sleepCtx(ctx, sleep) {
				return
			}
			backoff = minDur(backoff*2, maxBackoff)
			continue
		}
		backoff = 200 * time.Millisecond
		log.Printf("[dial] connected")
		err = serveConn(ctx, c)
		_ = c.CloseNow() // 直接断开（demo里用这个最干脆）
		if err != nil && !errors.Is(err, context.Canceled) {
			cs := websocket.CloseStatus(err)
			log.Printf("[conn] end: err=%v closeStatus=%v", err, cs)
		}
	}
}

func serveConn(ctx context.Context, c *websocket.Conn) error {
	const (
		pingInterval = 20 * time.Second
		readTimeout  = 45 * time.Second // 通常 ~ 2*pingInterval
		writeTimeout = 2 * time.Second
		sendInterval = 1 * time.Second
	)
	errCh := make(chan error, 1)
	go func() {
		for {
			rctx, cancel := context.WithTimeout(ctx, readTimeout)
			_, msg, err := c.Read(rctx)
			cancel()
			if err != nil {
				errCh <- err
				return
			}
			log.Printf("[recv] %s", string(msg))
		}
	}()
	pingT := time.NewTicker(pingInterval)
	defer pingT.Stop()
	sendT := time.NewTicker(sendInterval)
	defer sendT.Stop()

	// 心跳检测
	for {
		select {
		case <-ctx.Done():
			_ = c.Close(websocket.StatusNormalClosure, "bye")
			return ctx.Err()
		case err := <-errCh:
			return err
		case <-pingT.C:
			// Ping 会等待 Reader 读到 Pong（Ping 本身不读连接）。:contentReference[oaicite:6]{index=6}
			pctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.Ping(pctx)
			cancel()
			if err != nil {
				return err
			}
			log.Printf("[ping] ok")
		case <-sendT.C:
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.Write(wctx, websocket.MessageText, []byte("hi "+time.Now().Format(time.RFC3339Nano)))
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

func minDur(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
