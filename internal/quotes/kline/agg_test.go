package kline

import (
	"testing"
	"time"
)

func TestKline_ParseFixedAndFormatFixed(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		got, ok := ParseFixed("3052.18000000")
		if !ok {
			t.Fatalf("ParseFixed failed")
		}
		// 3052.18 * 1e8 = 305218000000
		want := int64(3052)*Scale + int64(18000000)*10 // 0.18 => 18,000,000 then pad to 8 digits -> 18,000,000
		// 上面这种写法容易看错：直接写死更清晰
		want = int64(305218000000)
		if got != want {
			t.Fatalf("want=%d got=%d", want, got)
		}
		if s := FormatFixed(got); s != "3052.18000000" {
			t.Fatalf("FormatFixed want=%q got=%q", "3052.18000000", s)
		}
	})

	t.Run("truncate_to_8_digits", func(t *testing.T) {
		got, ok := ParseFixed("1.123456789") // 9 位小数，v0实现截断到 8 位
		if !ok {
			t.Fatalf("ParseFixed failed")
		}
		// 1.12345678
		want := int64(1)*Scale + int64(12345678)
		if got != want {
			t.Fatalf("want=%d got=%d", want, got)
		}
		if s := FormatFixed(got); s != "1.12345678" {
			t.Fatalf("FormatFixed want=%q got=%q", "1.12345678", s)
		}
	})

	t.Run("pad_to_8_digits", func(t *testing.T) {
		got, ok := ParseFixed("0.31") // => 0.31000000
		if !ok {
			t.Fatalf("ParseFixed failed")
		}
		want := int64(31000000)
		if got != want {
			t.Fatalf("want=%d got=%d", want, got)
		}
		if s := FormatFixed(got); s != "0.31000000" {
			t.Fatalf("FormatFixed want=%q got=%q", "0.31000000", s)
		}
	})
}

func TestKline_BucketStartMs(t *testing.T) {
	intervalMs := int64((1 * time.Hour) / time.Millisecond)
	offsetMs := int64((30 * time.Minute) / time.Millisecond)

	t.Run("offset_alignment", func(t *testing.T) {
		// ts=0 时，按 offset=+30min 对齐，桶起点会是 -30min
		got := bucketStartMs(0, intervalMs, offsetMs)
		want := -offsetMs
		if got != want {
			t.Fatalf("want=%d got=%d", want, got)
		}
	})

	t.Run("next_bucket", func(t *testing.T) {
		// ts=31min -> 应落在起点=30min 的桶（因为 offset=30min）
		ts := int64((31 * time.Minute) / time.Millisecond)
		got := bucketStartMs(ts, intervalMs, offsetMs)
		want := int64((30 * time.Minute) / time.Millisecond)
		if got != want {
			t.Fatalf("want=%d got=%d", want, got)
		}
	})
}

func TestKline_TradeAgg_SameBucket(t *testing.T) {
	var emitted []Bar
	emit := func(b Bar) { emitted = append(emitted, b) }

	agg := NewTradeAgg(1*time.Second, 0, emit)

	// 两笔成交都在 [1000,2000) 这一秒桶内
	agg.OfferTrade(Trade{
		Symbol:   "BTC-USDT",
		PriceStr: "100.00000000",
		SizeStr:  "1.00000000",
		TsUnixMs: 1500,
	})
	agg.OfferTrade(Trade{
		Symbol:   "BTC-USDT",
		PriceStr: "101.00000000",
		SizeStr:  "2.00000000",
		TsUnixMs: 1999,
	})

	if len(emitted) != 0 {
		t.Fatalf("should not emit until bucket switches/flush, got=%d", len(emitted))
	}

	agg.Flush()
	if len(emitted) != 1 {
		t.Fatalf("flush should emit 1 bar, got=%d", len(emitted))
	}
	b := emitted[0]

	if b.StartMs != 1000 || b.EndMs != 2000 {
		t.Fatalf("bucket range want [1000,2000) got [%d,%d)", b.StartMs, b.EndMs)
	}

	// 100 * 1e8, 101 * 1e8
	wantOpen := int64(100) * Scale
	wantClose := int64(101) * Scale
	wantHigh := wantClose
	wantLow := wantOpen
	// volume = 1 + 2 = 3
	wantVol := int64(3) * Scale

	if b.Open != wantOpen || b.Close != wantClose || b.High != wantHigh || b.Low != wantLow {
		t.Fatalf("OHLC mismatch: got O=%d H=%d L=%d C=%d", b.Open, b.High, b.Low, b.Close)
	}
	if b.Volume != wantVol {
		t.Fatalf("Volume mismatch: want=%d got=%d", wantVol, b.Volume)
	}
	if b.Count != 2 {
		t.Fatalf("Count mismatch: want=2 got=%d", b.Count)
	}
}

func TestKline_TradeAgg_NewBucket_EmitsPrevious(t *testing.T) {
	var emitted []Bar
	emit := func(b Bar) { emitted = append(emitted, b) }

	agg := NewTradeAgg(1*time.Second, 0, emit)

	// 第一笔：桶 [1000,2000)
	agg.OfferTrade(Trade{
		Symbol:   "ETH-USDT",
		PriceStr: "10.00000000",
		SizeStr:  "1.00000000",
		TsUnixMs: 1500,
	})
	// 第二笔：桶 [2000,3000) -> 应触发 emit 上一根 bar
	agg.OfferTrade(Trade{
		Symbol:   "ETH-USDT",
		PriceStr: "11.00000000",
		SizeStr:  "1.00000000",
		TsUnixMs: 2500,
	})

	if len(emitted) != 1 {
		t.Fatalf("should emit previous bar on bucket switch, got=%d", len(emitted))
	}
	if emitted[0].StartMs != 1000 || emitted[0].EndMs != 2000 {
		t.Fatalf("first emitted bar range mismatch: [%d,%d)", emitted[0].StartMs, emitted[0].EndMs)
	}

	// Flush 再吐出当前桶 [2000,3000)
	agg.Flush()
	if len(emitted) != 2 {
		t.Fatalf("flush should emit current bar too, got=%d", len(emitted))
	}
	if emitted[1].StartMs != 2000 || emitted[1].EndMs != 3000 {
		t.Fatalf("second emitted bar range mismatch: [%d,%d)", emitted[1].StartMs, emitted[1].EndMs)
	}
}

func TestKline_TradeAgg_LateTrade_Dropped(t *testing.T) {
	var emitted []Bar
	emit := func(b Bar) { emitted = append(emitted, b) }

	agg := NewTradeAgg(1*time.Second, 0, emit)

	// 先进入桶 [2000,3000)
	agg.OfferTrade(Trade{
		Symbol:   "BTC-USDT",
		PriceStr: "100.00000000",
		SizeStr:  "1.00000000",
		TsUnixMs: 2500,
	})
	// 来一笔“更早桶”的 trade（[1000,2000)），如果被错误合并会污染当前桶
	agg.OfferTrade(Trade{
		Symbol:   "BTC-USDT",
		PriceStr: "1.00000000", // 极端低价，用来检测是否被错误合并
		SizeStr:  "1.00000000",
		TsUnixMs: 1500,
	})

	agg.Flush()
	if len(emitted) != 1 {
		t.Fatalf("expected 1 bar, got=%d", len(emitted))
	}
	b := emitted[0]
	if b.StartMs != 2000 || b.EndMs != 3000 {
		t.Fatalf("range mismatch: [%d,%d)", b.StartMs, b.EndMs)
	}
	// 如果晚到 trade 被错误合并，Low 会变成 1*Scale
	if b.Low != int64(100)*Scale {
		t.Fatalf("late trade should be dropped; low polluted: got=%d", b.Low)
	}
}

func TestKline_RollupAgg_MinuteFromSeconds(t *testing.T) {
	var emitted []Bar
	emit := func(b Bar) { emitted = append(emitted, b) }

	agg := NewRollupAgg(1*time.Minute, 0, emit)

	// 两根 1s bar，都属于分钟桶 [0,60000)
	child1 := Bar{
		Symbol:   "BTC-USDT",
		Interval: 1 * time.Second,
		StartMs:  0,
		EndMs:    1000,
		Open:     int64(100) * Scale,
		High:     int64(101) * Scale,
		Low:      int64(99) * Scale,
		Close:    int64(100) * Scale,
		Volume:   int64(1) * Scale,
		Count:    10,
	}
	child2 := Bar{
		Symbol:   "BTC-USDT",
		Interval: 1 * time.Second,
		StartMs:  1000,
		EndMs:    2000,
		Open:     int64(100) * Scale,
		High:     int64(105) * Scale,
		Low:      int64(98) * Scale,
		Close:    int64(104) * Scale,
		Volume:   int64(2) * Scale,
		Count:    12,
	}

	agg.OfferBar(child1)
	agg.OfferBar(child2)

	if len(emitted) != 0 {
		t.Fatalf("should not emit until bucket switches/flush, got=%d", len(emitted))
	}

	// 来一个新分钟的 child，触发 emit 旧分钟 bar
	child3 := Bar{
		Symbol:   "BTC-USDT",
		Interval: 1 * time.Second,
		StartMs:  60000,
		EndMs:    61000,
		Open:     int64(200) * Scale,
		High:     int64(200) * Scale,
		Low:      int64(200) * Scale,
		Close:    int64(200) * Scale,
		Volume:   int64(1) * Scale,
		Count:    1,
	}
	agg.OfferBar(child3)

	if len(emitted) != 1 {
		t.Fatalf("should emit 1 minute bar, got=%d", len(emitted))
	}
	b := emitted[0]
	if b.StartMs != 0 || b.EndMs != 60000 {
		t.Fatalf("minute range mismatch: [%d,%d)", b.StartMs, b.EndMs)
	}
	// 合并规则验证
	if b.Open != child1.Open {
		t.Fatalf("open mismatch: want=%d got=%d", child1.Open, b.Open)
	}
	if b.Close != child2.Close {
		t.Fatalf("close mismatch: want=%d got=%d", child2.Close, b.Close)
	}
	// high = max(101,105) -> 105
	if b.High != int64(105)*Scale {
		t.Fatalf("high mismatch: got=%d", b.High)
	}
	// low = min(99,98) -> 98
	if b.Low != int64(98)*Scale {
		t.Fatalf("low mismatch: got=%d", b.Low)
	}
	// volume = 1 + 2 = 3
	if b.Volume != int64(3)*Scale {
		t.Fatalf("volume mismatch: got=%d", b.Volume)
	}
	// Count 在 RollupAgg 中是“子 bar 个数”
	if b.Count != 2 {
		t.Fatalf("count mismatch: want=2 got=%d", b.Count)
	}

	// Flush 会吐出当前分钟（child3 所在分钟）
	agg.Flush()
	if len(emitted) != 2 {
		t.Fatalf("flush should emit current minute too, got=%d", len(emitted))
	}
	if emitted[1].StartMs != 60000 || emitted[1].EndMs != 120000 {
		t.Fatalf("second minute range mismatch: [%d,%d)", emitted[1].StartMs, emitted[1].EndMs)
	}
}
