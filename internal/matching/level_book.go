package matching

type priceLevel struct {
	price int64   // 价格
	head  *lvNode //头部指针
	tail  *lvNode // 尾部指针
	size  int64   // 桶的大小
}

// 实现一个双向链表
type lvNode struct {
	prev  *lvNode     // 前指针
	next  *lvNode     // 后指针
	order *Order      // 订单指针
	lv    *priceLevel // 所属的价格桶
	side  uint8       // 属于卖方还是买方
}

// 入栈 相当于 Add
// Add 新订单时：同价位直接追加到队尾 => 天然满足 FIFO
func (l *priceLevel) pushBack(n *lvNode) {
	// 新进来一个 将目前的指针移动
	n.prev, n.next = l.tail, nil
	// 如果不是空连的话
	if l.tail != nil {
		l.tail.next = n
	} else {
		// 如果是空连的话
		l.head = n
	}
	l.tail = n
	l.size++
}

// 删除节点
func (l *priceLevel) remove(n *lvNode) {
	// 处理前驱连接
	if n.prev != nil {
		n.prev.next = n.next
	} else {
		// n 是 head
		l.head = n.next
	}
	// 处理后继连接
	if n.next != nil {
		n.next.prev = n.prev
	} else {
		// n 是 tail
		l.tail = n.prev
	}
	// 断开节点指针，避免误用
	n.prev, n.next = nil, nil
	l.size--
}
func (l *priceLevel) empty() bool {
	return l.size == 0
}

type LevelOrderBook struct {
	// int是价格 然后价格后面挂上链接
	asks    map[int64]*priceLevel // 卖盘：price -> level
	bids    map[int64]*priceLevel // 买盘：price -> level
	byID    map[uint64]*lvNode    // 订单索引：orderID -> node（撤单 O(1)）
	bestAsk int64                 // 最新卖价
	bestBid int64                 // 最新买价
	hasAsk  bool                  //是否存在
	hasBid  bool                  // 有没有对应盘（避免 0 值歧义） 这个不懂
}

func NewLevelOrderBook() *LevelOrderBook {
	return &LevelOrderBook{
		asks: make(map[int64]*priceLevel, 1024),
		bids: make(map[int64]*priceLevel, 1024),
		byID: make(map[uint64]*lvNode, 1024),
	}
}

func (b *LevelOrderBook) Add(order *Order) {
	if order == nil || order.Qty <= 0 {
		return
	}
	// 重复id直接忽略
	if _, exists := b.byID[order.ID]; exists {
		return
	}
	if order.Side == Sell {
		// 找出卖价格的桶
		lv := b.asks[order.Price]
		if lv == nil {
			lv = &priceLevel{price: order.Price}
			b.asks[order.Price] = lv
		}
		// 2) 追加到 FIFO 队尾（同价时间优先）
		n := &lvNode{order: order, lv: lv, side: Sell}
		lv.pushBack(n)
		// 3) 建立 orderID -> node 索引，供 O(1) 撤单使用
		b.byID[order.ID] = n
		if !b.hasAsk || order.Price < b.bestAsk {
			b.bestAsk = order.Price
			b.hasAsk = true
		}

		return
	}
	if order.Side == Buy {
		lv := b.bids[order.Price]
		if lv == nil {
			lv = &priceLevel{price: order.Price}
			b.bids[order.Price] = lv
		}
		n := &lvNode{order: order, lv: lv, side: Buy}
		lv.pushBack(n)
		b.byID[order.ID] = n
		if !b.hasBid || order.Price > b.bestBid {
			b.bestBid = order.Price
			b.hasBid = true
		}

	}
}

// Cancel：撤单（Step3 的最大收益点）
// - 通过 byID O(1) 定位到 node
// - 通过双向链表 O(1) 摘链
// => 完全避免 Step1/2 的 copy/memmove
func (b *LevelOrderBook) Cancel(orderID uint64) bool {
	n := b.byID[orderID]
	if n == nil {
		return false
	}

	// 1) 从对应价位桶摘链
	lv := n.lv
	lv.remove(n)

	// 2) 删除索引
	if lv.empty() {
		if n.side == Sell {
			delete(b.asks, lv.price)
			// 只有删除的是 bestAsk 桶，才需要重算
			// 删除的桶 需要重新计算best
			if b.hasAsk && lv.price == b.bestAsk {
				b.recomputeBestAsk()
			}
		} else {
			// Buy
			delete(b.bids, lv.price)
			// 删除的桶 需要重新计算best
			if b.hasBid && lv.price == b.bestBid {
				b.recomputeBestBid()
			}
		}
	}

	return true
}

// BestAsk 返回当前最优卖价（最低价）
func (b *LevelOrderBook) BestAsk() (price int64, ok bool) {
	if !b.hasAsk {
		return 0, false
	}
	return b.bestAsk, true
}

// BestBid 返回当前最优买价（最高价）
func (b *LevelOrderBook) BestBid() (price int64, ok bool) {
	if !b.hasBid {
		return 0, false
	}
	return b.bestBid, true
}

// 计算价格的最低价
func (b *LevelOrderBook) recomputeBestAsk() {
	var best int64
	var flag = true
	for p, val := range b.asks {
		if val == nil || val.empty() {
			continue
		}
		if flag || p < best {
			best = p
			flag = false
		}
	}
	// 如果进行到循环里面
	if flag {
		b.hasAsk = false
		b.bestAsk = 0
		return
	}
	b.hasAsk = true
	b.bestAsk = best
}

func (b *LevelOrderBook) recomputeBestBid() {
	var best int64
	first := true
	for p, lv := range b.bids {
		if lv == nil || lv.empty() {
			continue
		}
		if first || p > best {
			best = p
			first = false
		}
	}
	if first {
		b.hasBid = false
		b.bestBid = 0
		return
	}
	b.hasBid = true
	b.bestBid = best
}

func (b *LevelOrderBook) SubmitLimit(order *Order) []Trade {
	if order == nil || order.Qty <= 0 {
		return nil
	}
	if order.Side == Buy {
		return b.matchBuy(order)
	}
	if order.Side == Sell {
		return b.matchSell(order)
	}
	return nil

}

func (b *LevelOrderBook) matchBuy(taker *Order) []Trade {
	// 获取bestAsk
	trade := make([]Trade, 0, 8)
	for taker.Qty > 0 {
		// 没有卖盘结束
		if !b.hasAsk {
			break
		}
		// 如果买盘大于最低卖盘结束
		if taker.Price < b.bestAsk {
			break
		}
		// 3) 拿到 bestAsk 的桶
		lv := b.asks[b.bestAsk]
		// 极端情况下 bestAsk 失效（比如你忘了维护），做一次自愈
		if lv == nil || lv.empty() {
			b.recomputeBestAsk()
			continue
		}
		// 开始吃单
		for taker.Qty > 0 && !lv.empty() {
			// 从头部开始吃单
			mn := lv.head
			maker := mn.order // 单的信息
			// 获取最小吃多少笔
			exec := min64(taker.Qty, maker.Qty)
			trade = append(trade, Trade{
				TakerID: taker.ID,
				MakerID: maker.ID,
				Price:   lv.price,
				Qty:     exec,
			})
			// 两边都减去数量
			taker.Qty -= exec
			maker.Qty -= exec
			// maker 桶被吃完了  摘链 删除索引
			if maker.Qty == 0 {
				lv.remove(mn)
				delete(b.byID, maker.ID)
			}
		}
		// 5) 桶吃空了：删除桶，并重算 bestAsk
		if lv.empty() {
			delete(b.asks, lv.price)
			// 如果删的正好是 bestAsk，则重算；这里必然是 bestAsk 桶
			b.recomputeBestAsk()
		}
	}
	// 6) taker 没吃完：挂单入簿（变成 maker）
	if taker.Qty > 0 {
		b.Add(taker)
	}
	return trade

}

func (b *LevelOrderBook) matchSell(taker *Order) []Trade {
	trades := make([]Trade, 0, 8)

	for taker.Qty > 0 {
		if !b.hasBid {
			break
		}
		if b.bestBid < taker.Price {
			break
		}

		lv := b.bids[b.bestBid]
		if lv == nil || lv.empty() {
			b.recomputeBestBid()
			continue
		}

		for taker.Qty > 0 && !lv.empty() {
			mn := lv.head
			maker := mn.order

			exec := min64(taker.Qty, maker.Qty)
			trades = append(trades, Trade{
				TakerID: taker.ID,
				MakerID: maker.ID,
				Price:   lv.price,
				Qty:     exec,
			})

			taker.Qty -= exec
			maker.Qty -= exec

			if maker.Qty == 0 {
				lv.remove(mn)
				delete(b.byID, maker.ID)
			}
		}

		if lv.empty() {
			delete(b.bids, lv.price)
			b.recomputeBestBid()
		}
	}

	if taker.Qty > 0 {
		b.Add(taker)
	}
	return trades

}
