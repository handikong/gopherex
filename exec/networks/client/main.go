package client

//
//import (
//	"context"
//	"errors"
//	"log"
//	"math/rand"
//	"net/http"
//	"os"
//	"os/signal"
//	"syscall"
//	"time"
//
//	"github.com/coder/websocket"
//)
//
//func main() {
//	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
//	defer stop()
//
//	url := "ws://127.0.0.1:8080/ws" // 先连你 Step1 的 echo server
//	runWithReconnect(ctx, url)
//}
//
//func runWithReconnect(ctx context.Context, url string) {
//	backoff := time.Millisecond * 20 // 每次等待时间
//	maxoff := time.Second * 10       // 最大等待时间
//	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
//	// 循环链接进行等待
//	for ctx.Err() == nil {
//		dCtx, cancelFunc := context.WithTimeout(ctx, time.Second*5)
//		dial, _, err := websocket.Dial(dCtx, url, &websocket.DialOptions{
//			HTTPHeader: http.Header{"Origin": []string{"http://localhost"}},
//		})
//		cancelFunc()
//		if err != nil {
//			// 如果发生出错误了 就继续重新连接
//			sleep := jmmter(rng, backoff)
//			log.Printf("[dial] fail: %v; retry in %v", err, sleep)
//			// 判断是先收到ctx done 还是sleep
//			if !ctxSleep(ctx, sleep) {
//				return
//			}
//			// 更新backoff
//			backoff = min(backoff*2, maxoff)
//			continue
//		}
//		// 启动监听服务
//		backoff = 200 * time.Millisecond
//		log.Printf("[dial] connected")
//		err = serveConn(ctx, dial)
//		// 断开连接
//		_ = dial.CloseNow()
//		if err != nil && !errors.Is(err, context.Canceled) {
//			cs := websocket.CloseStatus(err)
//			log.Printf("[conn] end: err=%v closeStatus=%v", err, cs)
//		}
//	}
//}
//
//func serveConn(ctx context.Context, dial *websocket.Conn) error {
//	// 设置四个时间
//	const (
//		pingInterval = 20 * time.Second
//		readTimeout  = 45 * time.Second // 通常 ~ 2*pingInterval
//		writeTimeout = 2 * time.Second
//		sendInterval = 1 * time.Second
//	)
//	// 设置一个err chan
//	errCh := make(chan error, 1)
//	// 启动一个监听读请求的
//	go func() {
//		for {
//			rCtx, cancelFunc := context.WithTimeout(ctx, readTimeout)
//			_, msg, err := dial.Read(rCtx)
//			cancelFunc()
//			if err != nil {
//				errCh <- err
//				return
//			}
//			log.Printf("[recv] %s", string(msg))
//		}
//	}()
//	// 阻塞主线程
//	pingT := time.NewTicker(pingInterval)
//	defer pingT.Stop()
//	sendT := time.NewTicker(sendInterval)
//	defer sendT.Stop()
//	for {
//		select {
//		case <-ctx.Done():
//			_ = dial.Close(websocket.StatusNormalClosure, "bye")
//			return ctx.Err()
//		case err := <-errCh:
//			return err
//		case <-pingT.C:
//			// Ping 会等待 Reader 读到 Pong（Ping 本身不读连接）。:contentReference[oaicite:6]{index=6}
//			wCtx, cancelFunc := context.WithTimeout(ctx, writeTimeout)
//			err := dial.Ping(wCtx)
//			cancelFunc()
//			if err != nil {
//				return err
//			}
//			log.Printf("[ping] ok")
//		case <-sendT.C:
//			wCtx, cancelFunc := context.WithTimeout(ctx, sendInterval)
//			err := dial.Write(wCtx, websocket.MessageText, []byte("hi "+time.Now().Format(time.RFC3339Nano)))
//			cancelFunc()
//			if err != nil {
//				return err
//			}
//		}
//	}
//
//}
//
//func ctxSleep(ctx context.Context, sleep time.Duration) bool {
//	tick := time.NewTicker(sleep)
//	defer tick.Stop()
//	select {
//	case <-tick.C:
//		return true
//	case <-ctx.Done():
//		return false
//	}
//}
//
//func jmmter(rng *rand.Rand, d time.Duration) time.Duration {
//	f := 0.5 + rng.Float64()
//	return time.Duration(float64(d) * f)
//}
