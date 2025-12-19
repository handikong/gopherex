package engine

import (
	"testing"
	"time"

	"gopherex.com/internal/matching"
)

// 等到 bus 出现一笔 Trade（用于验证“撮合→事件→bus”链路）
func waitTrade(t *testing.T, ch <-chan Event, timeout time.Duration) Event {
	t.Helper()

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	for {
		select {
		case ev := <-ch:
			if ev.Type == EvTrade {
				return ev
			}
		case <-deadline.C:
			t.Fatalf("timeout waiting EvTrade")
		}
	}
}

func TestStep5_3_E2E_JSONWalDump(t *testing.T) {
	const sym = "BTCUSDT"
	walDir := "./logs/"

	// Run1：开 cmdWAL + outbox，但不开 publisher（模拟“先把事实写进 outbox”，不发给下游）
	{

		cfg := EngineConfig{
			WALDir: walDir,

			EnableCmdWAL:    true,
			EnableOutbox:    true,
			EnablePublisher: false, // 关键：Run1 不发

			// 你已实现的注入点：用 JSON 让 payload 可读
			CmdCodec: JSONCmdCodec{Version: 1},
			EvCodec:  JSONEvCodec{Version: 1},

			ActorCfg: ActorConfig{MailboxSize: 4096, BatchMax: 256},

			// 用你现有的 LevelOrderBookHeap（通过 Adapter）
			BookFactory: func(symbol string) (OrderBook, error) {
				// 这里假设你已经写好了 HeapBookAdapter，并实现了新的 OrderBook 接口（带 reqID）
				return &HeapBookAdapter{B: matching.NewLevelOrderHeapBook()}, nil
			},
		}

		eng := NewEngine(cfg) // 按你仓库实际构造函数名

		// 两笔单撮合成交
		if err := eng.TrySubmit(sym, Command{
			Type:    CmdSubmitLimit,
			ReqID:   5,
			OrderID: 1005,
			UserID:  2001,
			Side:    Buy,
			Price:   90,
			Qty:     100,
		}); err != nil {
			t.Fatal(err)
		}
		if err := eng.TrySubmit(sym, Command{
			Type:    CmdSubmitLimit,
			ReqID:   6,
			OrderID: 1006,
			UserID:  2002,
			Side:    Sell,
			Price:   89,
			Qty:     20,
		}); err != nil {
			t.Fatal(err)
		}

		// 让 actor 有时间跑完 batch（也可以更严谨：等待某个条件）
		time.Sleep(80 * time.Millisecond)

	}
}
func TestReader(t *testing.T) {
	DumpCmdWALPretty(t, cmdWalPath("./logs/", "BTCUSDT"))
	DumpEvWALPretty(t, outboxWalPath("./logs/", "BTCUSDT"))

}
