package matching

import (
	"fmt"
	"testing"
)

func TestLevelBook_BestAsk(t *testing.T) {
	b := NewLevelOrderBook()
	b.Add(&Order{ID: 1, Side: Sell, Price: 101, Qty: 1})
	b.Add(&Order{ID: 2, Side: Sell, Price: 100, Qty: 1})

	p, ok := b.BestAsk()
	if !ok || p != 100 {
		t.Fatalf("best ask expected 100, got %v %v", p, ok)
	}

	// 撤掉 best 桶里的订单，best 应该变为 101（触发 recompute）
	if !b.Cancel(2) {
		t.Fatalf("cancel failed")
	}
	p, ok = b.BestAsk()
	if !ok || p != 101 {
		t.Fatalf("best ask expected 101, got %v %v", p, ok)
	}
}

func TestLevelBook_BestBid(t *testing.T) {
	b := NewLevelOrderBook()
	b.Add(&Order{ID: 1, Side: Buy, Price: 99, Qty: 1})
	b.Add(&Order{ID: 2, Side: Buy, Price: 100, Qty: 1})

	p, ok := b.BestBid()
	if !ok || p != 100 {
		t.Fatalf("best bid expected 100, got %v %v", p, ok)
	}

	if !b.Cancel(2) {
		t.Fatalf("cancel failed")
	}
	p, ok = b.BestBid()
	if !ok || p != 99 {
		t.Fatalf("best bid expected 99, got %v %v", p, ok)
	}
}

func TestMatchBuy_FIFO(t *testing.T) {
	b := NewLevelOrderBook()
	// 两个同价卖单，FIFO：1 先被吃
	b.Add(&Order{ID: 1, Side: Sell, Price: 100, Qty: 2})
	b.Add(&Order{ID: 2, Side: Sell, Price: 100, Qty: 2})

	tr := b.SubmitLimit(&Order{ID: 10, Side: Buy, Price: 100, Qty: 3})
	if len(tr) != 2 {
		t.Fatalf("expected 2 trades, got %d", len(tr))
	}
	if tr[0].MakerID != 1 || tr[0].Qty != 2 {
		t.Fatalf("expected first fill maker=1 qty=2, got %+v", tr[0])
	}
	if tr[1].MakerID != 2 || tr[1].Qty != 1 {
		t.Fatalf("expected second fill maker=2 qty=1, got %+v", tr[1])
	}
}

func TestMatchBuy_CrossLevels(t *testing.T) {
	b := NewLevelOrderBook()
	b.Add(&Order{ID: 1, Side: Sell, Price: 100, Qty: 1})
	b.Add(&Order{ID: 2, Side: Sell, Price: 101, Qty: 1})

	tr := b.SubmitLimit(&Order{ID: 10, Side: Buy, Price: 101, Qty: 2})
	fmt.Println(tr)
	if len(tr) != 2 || tr[0].Price != 100 || tr[1].Price != 101 {
		t.Fatalf("unexpected trades: %+v", tr)
	}
}

func TestMatchLeavesRestingOrder(t *testing.T) {
	b := NewLevelOrderBook()
	// 卖盘只有 1
	b.Add(&Order{ID: 1, Side: Sell, Price: 100, Qty: 1})

	// 买 3，最多成交 1，剩余 2 应该挂到 bids@100
	tr := b.SubmitLimit(&Order{ID: 10, Side: Buy, Price: 100, Qty: 3})
	if len(tr) != 1 || tr[0].Qty != 1 {
		t.Fatalf("unexpected trades: %+v", tr)
	}

}
