package matching

import (
	"fmt"
	"testing"
)

func TestFIFO_SamePrice(t *testing.T) {
	b := NewNaiveOrderBook()
	// 添加两个卖单
	b.Add(&Order{ID: 101, Side: Sell, Price: 100, Qty: 5})
	b.Add(&Order{ID: 102, Side: Sell, Price: 100, Qty: 5})
	// 进行撮合交易
	trades := b.SubmitLimit(&Order{ID: 201, Side: Buy, Price: 100, Qty: 5})
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
	// 成交的必须是101
	if trades[0].MakerID != 101 {
		t.Fatalf("expected maker 101 first (FIFO), got %d", trades[0].MakerID)
	}
	if len(b.asks) != 1 {
		t.Fatalf("expected 1 ask, got %d", len(b.asks))
	}
	if b.BestAsk().ID != 102 {
		t.Fatalf("expected remaining ask is 102, got %d", b.BestAsk().ID)
	}
}

func TestCancel_O1_IndexUpdated(t *testing.T) {
	b := NewNaiveOrderBook()
	// three asks same price FIFO: 1,2,3
	b.Add(&Order{ID: 1, Side: Sell, Price: 100, Qty: 1})
	b.Add(&Order{ID: 2, Side: Sell, Price: 100, Qty: 1})
	b.Add(&Order{ID: 3, Side: Sell, Price: 100, Qty: 1})

	if !b.Cancel(2) {
		t.Fatalf("cancel failed")
	}
	// Now asks should be [1,3]
	if b.AskLen() != 2 || b.BestAsk().ID != 1 || b.asks[1].ID != 3 {
		t.Fatalf("unexpected asks: len=%d best=%d second=%d",
			b.AskLen(), b.BestAsk().ID, b.asks[1].ID)
	}
	// Cancel 3 should succeed (index updated)
	if !b.Cancel(3) {
		t.Fatalf("cancel 3 failed, index likely wrong")
	}
	if b.AskLen() != 1 || b.BestAsk().ID != 1 {
		t.Fatalf("unexpected asks after cancel 3")
	}
}

func TestSlice(t *testing.T) {
	var s = []int{1, 2, 3, 4}
	index := 1
	fmt.Println(s[index:])
	fmt.Println(s[index+1:])
	copy(s[index:], s[index+1:])
	fmt.Println(s)
}
