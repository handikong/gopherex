package engine

//
//import (
//	"sync"
//	"sync/atomic"
//	"testing"
//)
//
//// noopBook：测试只关心“actor 是否只创建一次”，不关心撮合逻辑
//type noopBook struct{}
//
//func (b *noopBook) SubmitLimit(orderID, userID uint64, side uint8, price, qty int64, emit Emitter) {
//	// no-op
//}
//func (b *noopBook) Cancel(orderID uint64, emit Emitter) bool { return false }
//
//func TestLazyCreateActorOnlyOnce_ConcurrentFirstSubmit(t *testing.T) {
//	var factoryCalls uint64
//
//	eng := NewEngine(EngineConfig{
//		EventBusSize: 1024,
//		ActorCfg: ActorConfig{
//			MailboxSize: 1 << 16, // 给大点，避免并发首单把 mailbox 塞满导致 ErrEngineBusy 干扰断言
//			BatchMax:    256,
//		},
//		BookFactory: func(symbol string) (OrderBook, error) {
//			atomic.AddUint64(&factoryCalls, 1)
//			return &noopBook{}, nil
//		},
//	})
//	defer eng.Stop()
//
//	const sym = "AAPL"
//	const N = 200
//
//	start := make(chan struct{})
//	var wg sync.WaitGroup
//	errCh := make(chan error, N)
//
//	// 并发“首单”同时打到同一个 symbol
//	for i := 0; i < N; i++ {
//		wg.Add(1)
//		go func(i int) {
//			defer wg.Done()
//			<-start
//			err := eng.TrySubmit(sym, Command{
//				Type:    CmdSubmitLimit,
//				ReqID:   uint64(i + 1),
//				OrderID: uint64(i + 1),
//				UserID:  1,
//				Side:    Buy,
//				Price:   100,
//				Qty:     1,
//			})
//			if err != nil {
//				errCh <- err
//			}
//		}(i)
//	}
//
//	close(start)
//	wg.Wait()
//	close(errCh)
//
//	// 1) 不应该有错误（如果你仍可能出现 EngineBusy，可把 EngineBusy 从这里排除掉）
//	for err := range errCh {
//		t.Fatalf("unexpected error: %v", err)
//	}
//
//	// 2) BookFactory 必须只被调用一次（并发下只允许一个 winner 创建 Actor）
//	if got := atomic.LoadUint64(&factoryCalls); got != 1 {
//		t.Fatalf("BookFactory called %d times, want 1", got)
//	}
//
//	// 3) Engine 内部 actor map 里也只能有 1 个 symbol，并且该 symbol 的 actor 非空
//	eng.mu.RLock()
//	defer eng.mu.RUnlock()
//
//	if len(eng.actors) != 1 {
//		t.Fatalf("actors map size=%d, want 1", len(eng.actors))
//	}
//	if eng.actors[sym] == nil {
//		t.Fatalf("actor for %s is nil", sym)
//	}
//}
