package matching

import "testing"

// 让盘口跨价位撮合，trade 数量足够触发 append
func seedAsksHeap(book *LevelOrderBookHeap, levels, per int) {
	var id uint64 = 1
	for i := 0; i < levels; i++ {
		p := int64(100 + i)
		for j := 0; j < per; j++ {
			book.Add(&Order{ID: id, Side: Sell, Price: p, Qty: 1})
			id++
		}
	}
}

func BenchmarkMatch_Heap_DefaultAlloc(b *testing.B) {
	book := NewLevelOrderHeapBook()
	seedAsksHeap(book, 50, 200)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = book.SubmitLimit(&Order{ID: uint64(1_000_000 + i), Side: Buy, Price: 200, Qty: 500})
	}
}

func BenchmarkMatch_Heap_BufferReuse(b *testing.B) {
	book := NewLevelOrderHeapBook()
	seedAsksHeap(book, 50, 200)

	buf := make([]Trade, 0, 2048) // 复用输出 buffer（容量可按成交量调整）

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf = book.SubmitLimitBuff(&Order{ID: uint64(1_000_000 + i), Side: Buy, Price: 200, Qty: 500}, buf)
	}
}

func BenchmarkMatch_Heap_BufferReuse_NoTakerAlloc(b *testing.B) {
	book := NewLevelOrderHeapBook()
	seedAsksHeap(book, 50, 200)

	buf := make([]Trade, 0, 2048)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		o := Order{ID: uint64(1_000_000 + i), Side: Buy, Price: 200, Qty: 500}
		buf = book.SubmitLimitBuff(&o, buf)
		// 防御：如果你仍然把卖盘吃光了，就重建（不计时）
		if _, ok := book.BestAsk(); !ok {
			b.StopTimer()
			book = NewLevelOrderHeapBook()
			seedAsksHeap(book, 100, 5000)
			b.StartTimer()
		}

	}
}

// 撤单/新增 churn：看 nodePool 是否让 allocs/op 下降
func BenchmarkChurn_Heap_CancelAdd_NodePool(b *testing.B) {
	book := NewLevelOrderHeapBook()
	const N = 20000
	for i := 0; i < N; i++ {
		book.Add(&Order{ID: uint64(i + 1), Side: Sell, Price: 100, Qty: 1})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		target := uint64((i % N) + 1)
		_ = book.Cancel(target)
		// 重新加一笔（新 ID）
		book.Add(&Order{ID: uint64(N + 1 + i), Side: Sell, Price: 100, Qty: 1})
	}
}

func BenchmarkMatch_Heap_BufferReuse_NoRest_NoTakerAlloc(b *testing.B) {
	book := NewLevelOrderHeapBook()
	seedAsksHeap(book, 200, 5000) // 足够深，尽量不被吃空

	buf := make([]Trade, 0, 4096)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 栈上 taker：只有在“不会被挂单入簿”时才不会逃逸
		o := Order{ID: uint64(1_000_000 + i), Side: Buy, Price: 200, Qty: 500}
		buf = book.SubmitLimitBuff(&o, buf)

		// 如果真的被你吃空了：重建簿子，但不计时（避免污染结果）
		if _, ok := book.BestAsk(); !ok {
			b.StopTimer()
			book = NewLevelOrderHeapBook()
			seedAsksHeap(book, 200, 5000)
			b.StartTimer()
		}
	}
}

func nopTrade(Trade) {}

func BenchmarkMatch_Heap_Emit_NoAddPath(b *testing.B) {
	book := NewLevelOrderHeapBook()
	seedAsksHeap(book, 200, 5000) // 深一点，避免撮合过程中走到“剩余挂单”
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		o := Order{ID: uint64(1_000_000 + i), Side: Buy, Price: 200, Qty: 1} // Qty 小，进一步确保不触发 Add(taker)
		_ = book.MatchLimitEmit(&o, nopTrade)                                // 只撮合，不挂单
	}
}
