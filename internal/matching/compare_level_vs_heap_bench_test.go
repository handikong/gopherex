package matching

import "testing"

// ---------- seed helpers ----------

func seedAsksLevelScan(book *LevelOrderBook, levels int) {
	var id uint64 = 1
	for i := 0; i < levels; i++ {
		book.Add(&Order{ID: id, Side: Sell, Price: int64(100 + i), Qty: 1})
		id++
	}
}

func seedAsksLevelHeap(book *LevelOrderBookHeap, levels int) {
	var id uint64 = 1
	for i := 0; i < levels; i++ {
		book.Add(&Order{ID: id, Side: Sell, Price: int64(100 + i), Qty: 1})
		id++
	}
}

// ---------- Bench: best 前进（通过撮合吃掉 best） ----------

func BenchmarkBestAdvance_ByMatch_LevelScan(b *testing.B) {
	const levels = 4096

	book := NewLevelOrderBook()
	seedAsksLevelScan(book, levels)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = book.SubmitLimit(&Order{
			ID:    uint64(1_000_000 + i),
			Side:  Buy,
			Price: 1 << 60, // 极大，保证能跨价位吃
			Qty:   1,
		})

		// 如果卖盘被吃光了，重置（不把重置时间算进 bench）
		if _, ok := book.BestAsk(); !ok {
			b.StopTimer()
			book = NewLevelOrderBook()
			seedAsksLevelScan(book, levels)
			b.StartTimer()
		}
	}
}

func BenchmarkBestAdvance_ByMatch_LevelHeap(b *testing.B) {
	const levels = 4096

	book := NewLevelOrderHeapBook()
	seedAsksLevelHeap(book, levels)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = book.SubmitLimit(&Order{
			ID:    uint64(1_000_000 + i),
			Side:  Buy,
			Price: 1 << 60,
			Qty:   1,
		})

		if _, ok := book.BestAsk(); !ok {
			b.StopTimer()
			book = NewLevelOrderHeapBook()
			seedAsksLevelHeap(book, levels)
			b.StartTimer()
		}
	}
}

// ---------- Bench: cancel best（best 桶被撤空） + 读取 BestAsk 触发更新 ----------
// 这个 bench 更“结构化”地测 best 更新：scan vs heap lazy pop。

func BenchmarkBestAdvance_ByCancel_LevelScan(b *testing.B) {
	const levels = 4096

	book := NewLevelOrderBook()
	seedAsksLevelScan(book, levels)

	var id uint64 = 1

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = book.Cancel(id)
		_, _ = book.BestAsk() // 让 best 更新逻辑走一遍（scan版通常在 Cancel 内触发 recompute）

		id++
		if id > levels {
			b.StopTimer()
			book = NewLevelOrderBook()
			seedAsksLevelScan(book, levels)
			id = 1
			b.StartTimer()
		}
	}
}

func BenchmarkBestAdvance_ByCancel_LevelHeap(b *testing.B) {
	const levels = 4096

	book := NewLevelOrderHeapBook()
	seedAsksLevelHeap(book, levels)

	var id uint64 = 1

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = book.Cancel(id)
		_, _ = book.BestAsk() // heap 版这里会 lazy pop 过期价位

		id++
		if id > levels {
			b.StopTimer()
			book = NewLevelOrderHeapBook()
			seedAsksLevelHeap(book, levels)
			id = 1
			b.StartTimer()
		}
	}
}
