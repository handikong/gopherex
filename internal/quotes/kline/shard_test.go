package kline

import (
	"context"
	"testing"
	"time"
)

func collectAll(t *testing.T, ch <-chan KlineEvent, timeout time.Duration) []KlineEvent {
	t.Helper()
	var out []KlineEvent
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
		case <-timer.C:
			t.Fatalf("timeout collecting events, collected=%d", len(out))
		}
	}
}

func filterBars(evs []KlineEvent, interval time.Duration, symbol string) []Bar {
	var bars []Bar
	for _, ev := range evs {
		if ev.Bar.Interval == interval && ev.Bar.Symbol == symbol {
			bars = append(bars, ev.Bar)
		}
	}
	// 这里不排序也行，但为了断言稳定，我们按 StartMs 排序（小规模用插入排序）
	for i := 1; i < len(bars); i++ {
		x := bars[i]
		j := i - 1
		for j >= 0 && bars[j].StartMs > x.StartMs {
			bars[j+1] = bars[j]
			j--
		}
		bars[j+1] = x
	}
	return bars
}

func TestShardedAggregator_E2E_MinuteGapFill(t *testing.T) {
	cfg := ShardedAggConfig{
		Shards:        4,
		ReorderWindow: 0, // 让测试更容易（无需等 watermark 推太远）
		TZOffset:      0,

		FillGaps1m: true,
		FillGaps1h: false,
		FillGaps1d: false,

		InboxSize:    1024,
		DropWhenFull: false,
	}

	agg, err := NewShardedAggregator(cfg)
	if err != nil {
		t.Fatalf("NewShardedAggregator err=%v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agg.Run(ctx)

	// 目标：构造 minute0 有成交，minute1 没成交，minute2 有成交
	// minute0: [0, 60000)
	// minute1: [60000, 120000) -> 期待补空
	// minute2: [120000, 180000)

	// 1) BTC-USDT minute0: 一笔 trade 在 500ms
	ok := agg.OfferTrade(Trade{
		Symbol:   "BTC-USDT",
		PriceStr: "100.00000000",
		SizeStr:  "1.00000000",
		TsUnixMs: 500,
	})
	if !ok {
		t.Fatalf("OfferTrade dropped unexpectedly")
	}

	// 2) BTC-USDT minute2: 一笔 trade 在 120500ms（中间 minute1 故意空）
	ok = agg.OfferTrade(Trade{
		Symbol:   "BTC-USDT",
		PriceStr: "110.00000000",
		SizeStr:  "1.00000000",
		TsUnixMs: 120_500,
	})
	if !ok {
		t.Fatalf("OfferTrade dropped unexpectedly")
	}

	// 再喂一个 ETH-USDT，验证多 symbol 也没问题（不强求 gap fill）
	_ = agg.OfferTrade(Trade{
		Symbol:   "ETH-USDT",
		PriceStr: "200.00000000",
		SizeStr:  "2.00000000",
		TsUnixMs: 700,
	})

	// 触发 flush：cancel ctx 后 Close() 等待 shard flush 完并关闭 out
	cancel()
	agg.Close()

	evs := collectAll(t, agg.Out(), 2*time.Second)

	// 只看 BTC-USDT 的 1m bars
	minBars := filterBars(evs, 1*time.Minute, "BTC-USDT")

	// 我们期望至少看到 minute0、minute1(空)、minute2（minute2 通常由 flush 吐出）
	if len(minBars) < 3 {
		t.Fatalf("expected >=3 minute bars for BTC-USDT, got=%d bars=%v", len(minBars), minBars)
	}

	// minute0
	b0 := minBars[0]
	if b0.StartMs != 0 || b0.EndMs != 60_000 {
		t.Fatalf("minute0 range mismatch: [%d,%d)", b0.StartMs, b0.EndMs)
	}
	if b0.Open != 100*Scale || b0.High != 100*Scale || b0.Low != 100*Scale || b0.Close != 100*Scale {
		t.Fatalf("minute0 OHLC mismatch: O=%d H=%d L=%d C=%d", b0.Open, b0.High, b0.Low, b0.Close)
	}
	if b0.Volume != 1*Scale {
		t.Fatalf("minute0 volume mismatch: got=%d", b0.Volume)
	}

	// minute1（补空K）
	b1 := minBars[1]
	if b1.StartMs != 60_000 || b1.EndMs != 120_000 {
		t.Fatalf("minute1 range mismatch: [%d,%d)", b1.StartMs, b1.EndMs)
	}
	// 补空规则：OHLC=上一根 close（100），V=0，Count=0
	if b1.Open != 100*Scale || b1.High != 100*Scale || b1.Low != 100*Scale || b1.Close != 100*Scale {
		t.Fatalf("minute1 filled OHLC mismatch: O=%d H=%d L=%d C=%d", b1.Open, b1.High, b1.Low, b1.Close)
	}
	if b1.Volume != 0 || b1.Count != 0 {
		t.Fatalf("minute1 should be empty: V=%d Count=%d", b1.Volume, b1.Count)
	}

	// minute2
	b2 := minBars[2]
	if b2.StartMs != 120_000 || b2.EndMs != 180_000 {
		t.Fatalf("minute2 range mismatch: [%d,%d)", b2.StartMs, b2.EndMs)
	}
	if b2.Open != 110*Scale || b2.High != 110*Scale || b2.Low != 110*Scale || b2.Close != 110*Scale {
		t.Fatalf("minute2 OHLC mismatch: O=%d H=%d L=%d C=%d", b2.Open, b2.High, b2.Low, b2.Close)
	}
	if b2.Volume != 1*Scale {
		t.Fatalf("minute2 volume mismatch: got=%d", b2.Volume)
	}
}

func TestShardRouting_SameSymbolSameShard(t *testing.T) {
	// 这个测试不是必须，但它能帮你“心里有底”：同 symbol 永远路由到同 shard
	n := 8
	s1 := shardIndex("BTC-USDT", n)
	s2 := shardIndex("BTC-USDT", n)
	if s1 != s2 {
		t.Fatalf("same symbol should map to same shard: %d vs %d", s1, s2)
	}

	// 不同 symbol 可能撞到同 shard（允许）
	_ = shardIndex("ETH-USDT", n)
}
