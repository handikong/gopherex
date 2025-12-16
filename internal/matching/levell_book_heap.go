package matching

import (
	"container/heap"
	"sync"
)

type priceLevelHeap struct {
	price int64       // 价格
	head  *lvNodeHeap //头部指针
	tail  *lvNodeHeap // 尾部指针
	size  int64       // 桶的大小
}

// 实现一个双向链表
type lvNodeHeap struct {
	prev  *lvNodeHeap     // 前指针
	next  *lvNodeHeap     // 后指针
	order *Order          // 订单指针
	lv    *priceLevelHeap // 所属的价格桶
	side  Side            // 属于卖方还是买方
}

// 使用pool 进行分配
var lvNodePool = sync.Pool{
	New: func() any {
		return new(lvNodeHeap)
	},
}

// 入栈 相当于 Add
// Add 新订单时：同价位直接追加到队尾 => 天然满足 FIFO
func (l *priceLevelHeap) pushBack(n *lvNodeHeap) {
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
func (l *priceLevelHeap) remove(n *lvNodeHeap) {
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
func (l *priceLevelHeap) empty() bool {
	return l.size == 0
}

type LevelOrderBookHeap struct {
	// int是价格 然后价格后面挂上链接
	asks map[int64]*priceLevelHeap // 卖盘：price -> level
	bids map[int64]*priceLevelHeap // 买盘：price -> level
	byID map[uint64]*lvNodeHeap    // 订单索引：orderID -> node（撤单 O(1)）
	askH minPriceHeap              // 最新卖价
	bidH maxPriceHeap              // 最新买价
	//hasAsk bool                      //是否存在
	//hasBid bool                      // 有没有对应盘（避免 0 值歧义） 这个不懂
}

func NewLevelOrderHeapBook() *LevelOrderBookHeap {
	l := &LevelOrderBookHeap{
		asks: make(map[int64]*priceLevelHeap, 1024),
		bids: make(map[int64]*priceLevelHeap, 1024),
		byID: make(map[uint64]*lvNodeHeap, 1024),
	}
	// 构建两个怼
	heap.Init(&l.askH)
	heap.Init(&l.bidH)
	return l

}

func (b *LevelOrderBookHeap) Add(order *Order) {
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
			lv = &priceLevelHeap{price: order.Price}
			b.asks[order.Price] = lv
			heap.Push(&b.askH, order.Price) // 新价位出现：入堆
		}
		// 2) 追加到 FIFO 队尾（同价时间优先）
		n := b.getNode(order, lv, Sell)
		lv.pushBack(n)
		// 3) 建立 orderID -> node 索引，供 O(1) 撤单使用
		b.byID[order.ID] = n

		return
	}
	if order.Side == Buy {
		lv := b.bids[order.Price]
		if lv == nil {
			lv = &priceLevelHeap{price: order.Price}
			b.bids[order.Price] = lv
			heap.Push(&b.bidH, order.Price) // 新价位出现：入堆

		}
		n := b.getNode(order, lv, Buy)
		lv.pushBack(n)
		b.byID[order.ID] = n

	}
}

// Cancel：撤单（Step3 的最大收益点）
// - 通过 byID O(1) 定位到 node
// - 通过双向链表 O(1) 摘链
// => 完全避免 Step1/2 的 copy/memmove
func (b *LevelOrderBookHeap) Cancel(orderID uint64) bool {
	n := b.byID[orderID]
	if n == nil {
		return false
	}

	// 1) 从对应价位桶摘链
	lv := n.lv
	lv.remove(n)

	delete(b.byID, orderID)
	b.putNode(n) // 放回池
	// 2) 删除索引
	if lv.empty() {
		if n.side == Sell {
			delete(b.asks, lv.price)
		} else {
			delete(b.bids, lv.price)
		}
	}
	return true
}

// BestAsk 返回当前最优卖价（最低价）
func (b *LevelOrderBookHeap) BestAsk() (price int64, ok bool) {
	return b.bestAskPrice()
}

// BestBid 返回当前最优买价（最高价）
func (b *LevelOrderBookHeap) BestBid() (price int64, ok bool) {
	return b.bestBidPrice()
}

func (b *LevelOrderBookHeap) SubmitLimitBuff(taker *Order, buf []Trade) []Trade {
	buf = buf[:0]
	b.MatchLimitEmit(taker, func(t Trade) {
		buf = append(buf, t)
	})
	return buf

}

func (b *LevelOrderBookHeap) SubmitLimit(order *Order) []Trade {
	b.SubmitLimitBuff(order, nil)
	return nil

}

func (b *LevelOrderBookHeap) matchBuy(taker *Order, trades []Trade) []Trade {
	// 获取bestAsk
	for taker.Qty > 0 {
		bestP, ok := b.bestAskPrice()
		if !ok || bestP > taker.Price {
			break
		}

		// 3) 拿到 bestAsk 的桶
		lv := b.asks[bestP]

		// 开始吃单
		for taker.Qty > 0 && !lv.empty() {
			// 从头部开始吃单
			mn := lv.head
			maker := mn.order // 单的信息
			// 获取最小吃多少笔
			exec := min64(taker.Qty, maker.Qty)
			trades = append(trades, Trade{
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
				b.putNode(mn) // 关键：归还节点

			}
		}
		// 5) 桶吃空了：删除桶，并重算 bestAsk
		if lv.empty() {
			delete(b.asks, lv.price)

		}
	}
	// 6) taker 没吃完：挂单入簿（变成 maker）
	if taker.Qty > 0 {
		b.Add(taker)
	}
	return trades

}

func (b *LevelOrderBookHeap) matchSell(taker *Order, trades []Trade) []Trade {

	for taker.Qty > 0 {
		bestP, ok := b.bestBidPrice()
		if !ok || bestP < taker.Price {
			break
		}

		lv := b.bids[bestP]
		if lv == nil || lv.empty() {
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
				b.putNode(mn) // 关键：归还节点
			}
		}

		if lv.empty() {
			delete(b.bids, lv.price)
		}
	}

	if taker.Qty > 0 {
		b.Add(taker)
	}
	return trades

}

// 基于事件
// MatchLimitEmit：只撮合，不挂单（热路径：目标 0 alloc）
// 返回剩余数量，由上层决定要不要挂单/撤销（IOC/FOK 都能支持）

func (b *LevelOrderBookHeap) MatchLimitEmit(taker *Order, emit func(Trade)) (restQty int64) {
	if taker == nil || taker.Qty <= 0 {
		return 0
	}
	if emit == nil {
		// 避免 nil 函数调用：给一个空 emitter
		emit = func(Trade) {}
	}
	switch taker.Side {
	case Buy:
		return b.matchBuyEmit(taker, emit)
	case Sell:
		return b.matchSellEmit(taker, emit)
	default:
		return taker.Qty
	}
}

func (b *LevelOrderBookHeap) matchBuyEmit(taker *Order, emit func(Trade)) int64 {
	for taker.Qty > 0 {
		bestP, ok := b.bestAskPrice()
		if !ok || bestP > taker.Price {
			break
		}
		lv := b.asks[bestP]
		// bestAskPrice 已过滤 empty；这里稳妥加一层防御
		if lv == nil || lv.empty() {
			continue
		}

		for taker.Qty > 0 && !lv.empty() {
			mn := lv.head
			maker := mn.order

			exec := min64(taker.Qty, maker.Qty)

			// 不构造 slice，直接输出
			emit(Trade{
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
				// 归还节点（你已实现 nodePool 的话）
				b.putNode(mn)
			}
		}

		if lv.empty() {
			delete(b.asks, lv.price) // heap 不删：lazy deletion
		}
	}
	return taker.Qty
}

func (b *LevelOrderBookHeap) matchSellEmit(taker *Order, emit func(Trade)) int64 {
	for taker.Qty > 0 {
		bestP, ok := b.bestBidPrice()
		if !ok || bestP < taker.Price {
			break
		}
		lv := b.bids[bestP]
		if lv == nil || lv.empty() {
			continue
		}

		for taker.Qty > 0 && !lv.empty() {
			mn := lv.head
			maker := mn.order

			exec := min64(taker.Qty, maker.Qty)

			emit(Trade{
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
				b.putNode(mn)
			}
		}

		if lv.empty() {
			delete(b.bids, lv.price)
		}
	}
	return taker.Qty

}

// bestAskPrice：从最小堆堆顶拿最低卖价；若堆顶对应桶已空/不存在，就弹出继续找
func (b *LevelOrderBookHeap) bestAskPrice() (int64, bool) {
	for b.askH.Len() > 0 {
		p := b.askH[0]
		lv := b.asks[p]
		if lv != nil && !lv.empty() {
			return p, true
		}
		heap.Pop(&b.askH) // lazy：丢掉过期价位
	}
	return 0, false
}

// bestBidPrice：从最大堆堆顶拿最高买价；若桶已空/不存在，就弹出继续找
func (b *LevelOrderBookHeap) bestBidPrice() (int64, bool) {
	for b.bidH.Len() > 0 {
		p := b.bidH[0]
		lv := b.bids[p]
		if lv != nil && !lv.empty() {
			return p, true
		}
		heap.Pop(&b.bidH)
	}
	return 0, false
}

func (b *LevelOrderBookHeap) getNode(order *Order, lv *priceLevelHeap, side Side) *lvNodeHeap {
	n := lvNodePool.Get().(*lvNodeHeap)
	// 必须重置字段：pool 取出来可能带着旧值
	n.prev, n.next = nil, nil
	n.order = order
	n.lv = lv
	n.side = side
	return n
}
func (b *LevelOrderBookHeap) putNode(n *lvNodeHeap) {
	if n == nil {
		return
	}
	// 断引用，避免把旧对象链/订单“粘”在池里造成隐性保留
	n.prev, n.next = nil, nil
	n.order = nil
	n.lv = nil
	n.side = 0
	lvNodePool.Put(n)
}
