package md

import (
	"sync"
	"time"
)

type KlineAgg struct {
	mu  sync.Mutex
	cur map[kkey]*KlineEvent

	Intervals []time.Duration
	Emit      func(KlineEvent)
	EmitEvery time.Duration
	nextEmit  map[kkey]time.Time
}
type kkey struct {
	ex string
	sy string
	iv time.Duration
}

func NewKlineAgg(intervals []time.Duration, emit func(KlineEvent)) *KlineAgg {
	return &KlineAgg{
		cur:       make(map[kkey]*KlineEvent, 64),
		Intervals: intervals,
		Emit:      emit,
		EmitEvery: 0, // 默认不节流，你在 main 里再设置成 200ms
		nextEmit:  make(map[kkey]time.Time, 64),
	}
}

func bucketStart(ts time.Time, interval time.Duration) time.Time {
	n := ts.UnixNano()
	i := int64(interval)
	return time.Unix(0, (n/i)*i).UTC()
}

func (a *KlineAgg) OnTrade(t TradeEvent) {
	for _, iv := range a.Intervals {
		a.onTradeInterval(t, iv)
	}
}

func (a *KlineAgg) onTradeInterval(t TradeEvent, iv time.Duration) {
	key := kkey{ex: t.Exchange, sy: t.Symbol, iv: iv}
	openTime := bucketStart(t.Ts, iv)
	now := time.Now()

	a.mu.Lock()
	defer a.mu.Unlock()

	k := a.cur[key]
	if k == nil || !k.OpenTime.Equal(openTime) {
		if k != nil && a.Emit != nil {
			a.Emit(*k) // flush 上一个
		}
		// 新桶开始：重置节流窗口
		delete(a.nextEmit, key)

		nk := &KlineEvent{
			Exchange:  t.Exchange,
			Symbol:    t.Symbol,
			Interval:  iv,
			OpenTime:  openTime,
			Open:      t.Price,
			High:      t.Price,
			Low:       t.Price,
			Close:     t.Price,
			Vol:       t.Size,
			Count:     1,
			UpdatedAt: t.Ts,
		}
		a.cur[key] = nk
		if a.Emit != nil {
			a.Emit(*nk) // 也可以发“更新中的K线”
		}
		return
	}

	if t.Price > k.High {
		k.High = t.Price
	}
	if t.Price < k.Low {
		k.Low = t.Price
	}
	k.Close = t.Price
	k.Vol += t.Size
	k.Count++
	k.UpdatedAt = t.Ts

	if a.Emit != nil {
		a.Emit(*k)
	}
	// 节流：到点才发
	next := a.nextEmit[key]
	if next.IsZero() || !now.Before(next) {
		a.Emit(*k)
		a.nextEmit[key] = now.Add(a.EmitEvery)
	}
}
