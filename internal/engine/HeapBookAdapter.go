package engine

import (
	"gopherex.com/internal/matching"
)

type HeapBookAdapter struct {
	B *matching.LevelOrderBookHeap
}

func NewHeapBookAdapter(b *matching.LevelOrderBookHeap) *HeapBookAdapter {
	return &HeapBookAdapter{B: b}
}

func (a *HeapBookAdapter) SubmitLimit(reqId, orderID, userID uint64, side uint8, price, qty int64, emit Emitter) {
	// 1) 先给“Accepted”（命令被接收，语义由你决定）
	emit.Accepted(reqId, orderID, userID) // 如果你的 Emitter.Accepted 带 reqID，就传 cmd.ReqID；这里示意

	// 2) 构造 taker（先别纠结 alloc，后面再做 OrderPool 优化）
	taker := &matching.Order{ID: orderID, UserID: userID, Side: side, Price: price, Qty: qty}

	// 3) 撮合：把 Trade 回调翻译成 Emitter.Trade
	rest := a.B.MatchLimitEmit(taker, func(t matching.Trade) {
		emit.Trade(reqId, t.MakerID, t.TakerID, t.Price, t.Qty)
	})

	// 4) 若 rest > 0：挂单入簿，并发 Added
	if rest > 0 {
		taker.Qty = rest
		a.B.Add(taker)
		emit.Added(reqId, orderID, userID)
	}
}

// Cancel：用你现有的 O(1) byID 撤单
func (a *HeapBookAdapter) Cancel(reqId, orderID uint64, emit Emitter) bool {
	ok := a.B.Cancel(orderID)
	if ok {
		emit.Cancelled(reqId, orderID)
	} else {
		emit.Rejected(reqId, orderID, 0, "order not found")
	}
	return ok
}
