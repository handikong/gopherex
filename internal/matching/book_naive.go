package matching

import "fmt"

type orderPos struct {
	side  Side
	index int
}

// 交易订单类
type NaiveOrderBook struct {
	bids []*Order // 买方
	asks []*Order // 卖方
	pos  map[uint64]orderPos
}

func NewNaiveOrderBook() *NaiveOrderBook {
	return &NaiveOrderBook{
		// 先预分配好长度
		bids: make([]*Order, 0, 1024),
		asks: make([]*Order, 0, 1024),
		pos:  make(map[uint64]orderPos, 1024),
	}
}

// 获取第一个
func (n *NaiveOrderBook) BestBid() *Order {
	if len(n.bids) <= 0 {
		return nil
	}
	return n.bids[0]
}

// 获取卖方第一个
func (n *NaiveOrderBook) BestAsk() *Order {
	if len(n.asks) <= 0 {
		return nil
	}
	return n.asks[0]
}
func (b *NaiveOrderBook) BidLen() int { return len(b.bids) }
func (b *NaiveOrderBook) AskLen() int { return len(b.asks) }

// 寻找价格插入
func (b *NaiveOrderBook) Add(order *Order) error {
	if order == nil || order.Qty <= 0 {
		return fmt.Errorf("invalid order %v", order)
	}
	if order.Side == Buy {
		return b.insertBid(order)
	}
	return b.insertAsk(order)
}

func (n *NaiveOrderBook) insertBid(order *Order) error {
	// 循环找到位置进行插入 然后其他的进行后移
	lo, hi := 0, len(n.bids)
	// 使用二分查找 查到需要加入的位置
	for lo < hi {
		mid := (lo + hi) >> 1
		if n.bids[mid].Price > order.Price {
			lo = mid + 1
		} else if n.bids[mid].Price < order.Price {
			hi = mid
		} else {
			// equal: move right to place after equals (FIFO)
			lo = mid + 1
		}
	}
	// 切片长度 len 扩大 1 个位置，给后面的“插入”腾出一个空槽位（占位），
	//然后再用 copy 把元素整体右移，完成“在中间插入一个元素”。
	//因为 Go 的切片不能像链表那样“直接在中间插入”，你必须先把长度变长，再移动元素。
	n.bids = append(n.bids, nil)
	// 将后面的复制
	copy(n.bids[lo+1:], n.bids[lo:])
	n.bids[lo] = order
	// 因为改变了位置 从找到位置开始 所有的index都要加1
	for i := lo; i < hi; i++ {
		if n.bids[i] == nil {
			n.pos[n.bids[i].ID] = orderPos{side: Buy, index: i}
		}
	}
	n.pos[order.ID] = orderPos{side: Buy, index: lo}

	return nil
}

func (b *NaiveOrderBook) insertAsk(o *Order) error {
	// Find insertion index:
	// Want all prices < o.Price before it.
	// For equal price, insert AFTER existing equal prices to preserve FIFO.
	lo, hi := 0, len(b.asks)
	for lo < hi {
		mid := (lo + hi) >> 1
		if b.asks[mid].Price < o.Price {
			lo = mid + 1
		} else if b.asks[mid].Price > o.Price {
			hi = mid
		} else {
			// equal: move right to place after equals (FIFO)
			lo = mid + 1
		}
	}
	b.asks = append(b.asks, nil)
	copy(b.asks[lo+1:], b.asks[lo:])
	b.asks[lo] = o

	// 因为改变了位置 从找到位置开始 所有的index都要加1
	for i := lo; i < hi; i++ {
		if b.asks[i] == nil {
			b.pos[b.asks[i].ID] = orderPos{side: Sell, index: i}
		}
	}
	b.pos[o.ID] = orderPos{side: Sell, index: lo}
	return nil
}

// 弹出一个
func (b *NaiveOrderBook) popBestAsk() *Order {
	if len(b.asks) == 0 {
		return nil
	}
	best := b.asks[0]
	copy(b.asks[0:], b.asks[1:])
	b.asks[len(b.asks)-1] = nil
	b.asks = b.asks[:len(b.asks)-1]
	return best
}

func (b *NaiveOrderBook) popBestBid() *Order {
	if len(b.bids) == 0 {
		return nil
	}
	best := b.bids[0]
	copy(b.bids[0:], b.bids[1:])
	b.bids[len(b.bids)-1] = nil
	b.bids = b.bids[:len(b.bids)-1]
	return best
}

func (b *NaiveOrderBook) Cancel(orderID uint64) bool {
	// 先在bid找 再ask找
	for index, order := range b.bids {
		if order != nil && order.ID == orderID {
			b.bids = removeAtOrderPtr(b.bids, index)
			return true
		}
	}
	for index, order := range b.asks {
		if order != nil && order.ID == orderID {
			b.asks = removeAtOrderPtr(b.asks, index)
			return true
		}
	}
	return false
}

func (b *NaiveOrderBook) Cancel2(orderID uint64) bool {
	// 先在bid找 再ask找
	p, ok := b.pos[orderID]
	if !ok {
		return false
	}
	switch p.side {
	case Buy:
		b.bids = removeAtOrderPtr(b.bids, p.index)
		for i := p.index; i < len(b.bids); i++ {
			if b.bids[i] != nil {
				b.pos[b.bids[i].ID] = orderPos{side: Buy, index: i}
			}
		}

	case Sell:
		b.asks = removeAtOrderPtr(b.asks, p.index)
		for i := p.index; i < len(b.asks); i++ {
			if b.asks[i] != nil {
				b.pos[b.asks[i].ID] = orderPos{side: Sell, index: i}
			}
		}
	}
	delete(b.pos, orderID)
	return true
}

func removeAtOrderPtr(s []*Order, i int) []*Order {
	copy(s[i:], s[i+1:]) // 把右边整体左移一格，覆盖掉 s[i]
	s[len(s)-1] = nil    // 把最后一个元素置空，避免内存/引用泄漏
	return s[:len(s)-1]  // 切掉最后一个（长度-1）
}
