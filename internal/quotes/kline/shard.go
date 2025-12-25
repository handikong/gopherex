package kline

import (
	"context"
	"errors"
	"hash/fnv"
	"sync"
	"time"
)

// KlineEvent：你可以直接用 Bar；这里单独包一层方便后续加 src/exchange/seq 等字段
type KlineEvent struct {
	Bar Bar
}

// ShardedAggConfig：聚合参数
type ShardedAggConfig struct {
	Shards        int
	ReorderWindow time.Duration
	TZOffset      time.Duration

	// 是否补空K（建议：1m/1h/1d true，1s false）
	FillGaps1m bool
	FillGaps1h bool
	FillGaps1d bool

	// 背压策略：inbox 满时是阻塞还是丢弃（v0 先给 DropNewest）
	InboxSize    int
	DropWhenFull bool
}

// ShardedAggregator：对外只暴露 OfferTrade / Out
type ShardedAggregator struct {
	cfg ShardedAggConfig
	out chan KlineEvent

	shards []shard
	wg     sync.WaitGroup
}
type shard struct {
	inbox chan Trade
	// 每个 shard 内部拥有自己的聚合链（它们内部的 map 都只在该 goroutine 访问，无锁）
	sAgg *TradeAgg
	mAgg *RollupAgg
	hAgg *RollupAgg
	dAgg *RollupAgg
}

func NewShardedAggregator(cfg ShardedAggConfig) (*ShardedAggregator, error) {
	if cfg.Shards <= 0 {
		return nil, errors.New("Shards must be > 0")
	}
	if cfg.InboxSize <= 0 {
		cfg.InboxSize = 8192
	}
	out := make(chan KlineEvent, 65536)

	a := &ShardedAggregator{
		cfg:    cfg,
		out:    out,
		shards: make([]shard, cfg.Shards),
	}

	for i := 0; i < cfg.Shards; i++ {
		sh := &a.shards[i]
		sh.inbox = make(chan Trade, cfg.InboxSize)

		// 1d emit（最终你可能会写入存储/推送；v0 先往 out 送）
		sh.dAgg = NewRollupAggFill(24*time.Hour, cfg.TZOffset, cfg.FillGaps1d, func(b Bar) {
			a.out <- KlineEvent{Bar: b}
		})
		sh.hAgg = NewRollupAggFill(1*time.Hour, cfg.TZOffset, cfg.FillGaps1h, func(b Bar) {
			a.out <- KlineEvent{Bar: b}
			sh.dAgg.OfferBar(b)
		})
		sh.mAgg = NewRollupAggFill(1*time.Minute, cfg.TZOffset, cfg.FillGaps1m, func(b Bar) {
			a.out <- KlineEvent{Bar: b}
			sh.hAgg.OfferBar(b)
		})

		// 1s：reorderWindow=2s（你已经做了），1s 通常不补空
		sh.sAgg = NewTradeAggReorder(1*time.Second, cfg.TZOffset, cfg.ReorderWindow, func(b Bar) {
			a.out <- KlineEvent{Bar: b}
			sh.mAgg.OfferBar(b)
		})
	}

	return a, nil
}

func (a *ShardedAggregator) Out() <-chan KlineEvent { return a.out }

// Run：启动 shard workers（每 shard 一个 goroutine）
func (a *ShardedAggregator) Run(ctx context.Context) {
	for i := range a.shards {
		sh := &a.shards[i]
		a.wg.Add(1)
		go func(sh *shard) {
			defer a.wg.Done()
			for {
				select {
				case <-ctx.Done():
					// 退出前 flush（把未关闭的 bars 吐出来）
					sh.sAgg.Flush()
					sh.mAgg.Flush()
					sh.hAgg.Flush()
					sh.dAgg.Flush()
					return
				case t := <-sh.inbox:
					sh.sAgg.OfferTrade(t)
				}
			}
		}(sh)
	}
}

// Close：等待 worker 退出，并关闭 out（由外部 cancel ctx 后调用）
func (a *ShardedAggregator) Close() {
	a.wg.Wait()
	close(a.out)
}

// OfferTrade：路由到 shard
func (a *ShardedAggregator) OfferTrade(t Trade) bool {
	// 获取分片 直接传递到shards
	idx := shardIndex(t.Symbol, len(a.shards))
	sh := &a.shards[idx]

	if !a.cfg.DropWhenFull {
		sh.inbox <- t
		return true
	}
	// DropNewest：inbox 满了直接丢（v0 简单做法；后续可按优先级丢 1s 保 1m）
	// 传递到sh.inbox
	select {
	case sh.inbox <- t:
		return true
	default:
		return false
	}
}

func shardIndex(symbol string, shards int) int {
	// v0：fnv1a + mod（足够用、无外部依赖）
	h := fnv.New64a()
	_, _ = h.Write([]byte(symbol))
	return int(h.Sum64() % uint64(shards))
}
