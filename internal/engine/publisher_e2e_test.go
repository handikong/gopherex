package engine

import (
	"context"
	"runtime"
	"testing"
	"time"

	"gopherex.com/internal/matching"
)

/************ JSON Codecs (test-only) ************/

/************ Helpers ************/

func waitEventType(t *testing.T, ch <-chan Event, tp uint8, timeout time.Duration) Event {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	for {
		select {
		case ev := <-ch:
			if uint8(ev.Type) == tp {
				return ev
			}
		case <-deadline.C:
			t.Fatalf("timeout waiting event type=%d", tp)
		}
	}
}

func assertNoEvent(t *testing.T, ch <-chan Event, d time.Duration) {
	t.Helper()
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case ev := <-ch:
		t.Fatalf("expected no event, but got type=%d seq=%d idx=%d", ev.Type, ev.Seq, ev.Idx)
	case <-timer.C:
		// ok
	}
}

/************ E2E Tests ************/

func TestE2E_Publisher_SubmitToTradeFlow(t *testing.T) {
	const sym = "BTCUSDT"
	dir := "./logs/"

	bus := NewChanBus(1 << 16)

	cfg := EngineConfig{
		WALDir:          dir,
		EnableCmdWAL:    true,
		EnableOutbox:    true,
		WALBufSize:      1 << 16,
		OutboxBufSize:   1 << 16,
		PublisherPoll:   10 * time.Millisecond,
		EnablePublisher: true,
		bus:             bus,

		// ✅ 测试用 JSON：方便读 ev/cmd.wal，也确保 publisher decode 能过
		CmdCodec: JSONCmdCodec{Version: 1},
		EvCodec:  JSONEvCodec{Version: 1},

		ActorCfg: ActorConfig{MailboxSize: 4096, BatchMax: 256},

		// ✅ 用你已经实现过的订单簿（如果你现在还没把 HeapBookAdapter 接到最新 OrderBook 接口，
		// 这里换成 simpleMatchBook / 你当前可编译的 adapter 即可）
		BookFactory: func(symbol string) (OrderBook, error) {
			return &HeapBookAdapter{B: matching.NewLevelOrderHeapBook()}, nil
		},
	}

	eng := NewEngine(cfg)

	// 1) Submit 两笔对手单，确保撮合出 Trade
	if err := eng.TrySubmit(sym, Command{
		Type:    CmdSubmitLimit,
		ReqID:   1,
		OrderID: 1001,
		UserID:  2001,
		Side:    Buy,
		Price:   100,
		Qty:     10,
	}); err != nil {
		t.Fatal(err)
	}
	if err := eng.TrySubmit(sym, Command{
		Type:    CmdSubmitLimit,
		ReqID:   2,
		OrderID: 1002,
		UserID:  2002,
		Side:    Sell,
		Price:   100,
		Qty:     10,
	}); err != nil {
		t.Fatal(err)
	}
}

type NopBus struct{}

func (NopBus) Publish(ctx context.Context, ev Event) error { return nil }

func BenchmarkE2E_SubmitToTradeFlow(b *testing.B) {
	const sym = "BTCUSDT"
	dir := b.TempDir()

	//bus := NewChanBus(1024) // 或者你已经关 publisher 就不用创建
	cfg := EngineConfig{
		WALDir:          dir,
		EnableCmdWAL:    true,
		EnableOutbox:    true,
		EnablePublisher: false,
		bus:             nil,
		PublisherPoll:   1 * time.Millisecond,
		CmdCodec:        BinaryCMDCode{},
		EvCodec:         EvCmdCodec{},
		ActorCfg:        ActorConfig{MailboxSize: 4096, BatchMax: 256},
		BookFactory: func(symbol string) (OrderBook, error) {
			return &HeapBookAdapter{B: matching.NewLevelOrderHeapBook()}, nil
		},
	}

	eng := NewEngine(cfg)

	// 预热创建 actor
	if err := eng.TrySubmit(sym, Command{Type: CmdSubmitLimit, ReqID: 1, OrderID: 1001, UserID: 1, Side: Buy, Price: 100, Qty: 1}); err != nil {
		b.Fatal(err)
	}
	a := eng.actors[sym] // 你用能拿到 actor 的方式（同包可直接访问 map）

	startSeq := a.seq

	const batchPairs = 512 // 每批 512 对单 = 1024 条命令，刚好 <= 4096 mailbox 的安全范围
	submitted := 0

	b.ReportAllocs()
	b.ResetTimer()

	i := 0
	for i < b.N {
		end := i + batchPairs
		if end > b.N {
			end = b.N
		}

		for ; i < end; i++ {
			base := uint64(i) * 2

			if err := eng.TrySubmit(sym, Command{
				Type: CmdSubmitLimit, ReqID: 100 + base, OrderID: 1_000_000 + base,
				UserID: 1, Side: Buy, Price: 100, Qty: 10,
			}); err == nil {
				submitted++
			}

			if err := eng.TrySubmit(sym, Command{
				Type: CmdSubmitLimit, ReqID: 101 + base, OrderID: 1_000_001 + base,
				UserID: 2, Side: Sell, Price: 100, Qty: 10,
			}); err == nil {
				submitted++
			}
		}

		// 等 actor 追上“成功提交的数量”
		target := startSeq + uint64(submitted)
		waitSeq(b, a, target, 3*time.Second)
	}

	b.StopTimer()

	// 最后再等一次收尾（避免最后一批未追上）
	target := startSeq + uint64(submitted)
	waitSeq(b, a, target, 5*time.Second)

	// 记录一下提交成功率（可选）
	if submitted == 0 {
		b.Fatalf("submitted=0, mailbox full or TrySubmit always fails")
	}
}

func waitSeq(b *testing.B, a *SymbolActor, target uint64, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for a.seq < target {
		if time.Now().After(deadline) {
			b.Fatalf("timeout waiting actor seq=%d target=%d", a.seq, target)
		}
		runtime.Gosched()
	}
}

//func TestE2E_Publisher_Restart_NoDuplicateFromCursor(t *testing.T) {
//	const sym = "BTCUSDT"
//	dir := "./logs/"
//
//	// ---------- Run #1：产出事件并让 publisher 推进 cursor ----------
//	{
//		bus := NewChanBus(1 << 16)
//
//		cfg := EngineConfig{
//			WALDir:          dir,
//			EnableCmdWAL:    true,
//			EnableOutbox:    true,
//			EnablePublisher: true,
//
//			WALBufSize:    1 << 16,
//			OutboxBufSize: 1 << 16,
//			PublisherPoll: 10 * time.Millisecond,
//
//			CmdCodec: JSONCmdCodec{Version: 1},
//			EvCodec:  JSONEvCodec{Version: 1},
//			bus:      bus,
//			ActorCfg: ActorConfig{MailboxSize: 4096, BatchMax: 256},
//			BookFactory: func(symbol string) (OrderBook, error) {
//				return &HeapBookAdapter{B: matching.NewLevelOrderHeapBook()}, nil
//			},
//		}
//
//		eng := NewEngine(cfg)
//
//		_ = eng.TrySubmit(sym, Command{
//			Type: CmdSubmitLimit, ReqID: 1, OrderID: 1001, UserID: 2001, Side: Buy, Price: 100, Qty: 10,
//		})
//		_ = eng.TrySubmit(sym, Command{
//			Type: CmdSubmitLimit, ReqID: 2, OrderID: 1002, UserID: 2002, Side: Sell, Price: 100, Qty: 10,
//		})
//
//		_ = waitEventType(t, bus.C(), 5 /*Trade*/, 2*time.Second)
//
//		time.Sleep(50 * time.Millisecond)
//	}
//
//	// ---------- Run #2：同目录重启，触发 actor 创建 + publisher 启动，但不应重复发布旧事件 ----------
//	{
//		bus := NewChanBus(1 << 16)
//
//		cfg := EngineConfig{
//			WALDir:          dir,
//			EnableCmdWAL:    true,
//			EnableOutbox:    true,
//			EnablePublisher: true,
//
//			WALBufSize:    1 << 16,
//			OutboxBufSize: 1 << 16,
//			PublisherPoll: 10 * time.Millisecond,
//
//			CmdCodec: JSONCmdCodec{Version: 1},
//			EvCodec:  JSONEvCodec{Version: 1},
//			bus:      bus,
//			ActorCfg: ActorConfig{MailboxSize: 4096, BatchMax: 256},
//			BookFactory: func(symbol string) (OrderBook, error) {
//				// 重启时空簿，靠 cmd.wal replay 恢复
//				return &HeapBookAdapter{B: matching.NewLevelOrderHeapBook()}, nil
//			},
//		}
//
//		eng := NewEngine(cfg)
//
//		// 关键：触发 getOrCreateActor（publisher 才会启动）
//		if _, err := eng.getOrCreateActor(sym); err != nil {
//			t.Fatal(err)
//		}
//
//		// 不提交新命令：不应收到旧事件（cursor 应该阻止重复发布）
//		assertNoEvent(t, bus.C(), 200*time.Millisecond)
//	}
//}
