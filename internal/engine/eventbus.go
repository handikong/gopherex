package engine

import (
	"context"
	"sync/atomic"
)

// 这个目前还不知道是干什么的
type ChanBus struct {
	ch      chan Event
	dropped uint64
}

func NewChanBus(size int) *ChanBus {
	if size <= 0 {
		size = 1 << 16
	}
	return &ChanBus{ch: make(chan Event, size)}
}

func (b *ChanBus) TryPublish(ev Event) bool {
	select {
	case b.ch <- ev:
		return true
	default:
		atomic.AddUint64(&b.dropped, 1)
		return false
	}
}

func (b *ChanBus) C() <-chan Event { return b.ch }
func (b *ChanBus) Dropped() uint64 { return atomic.LoadUint64(&b.dropped) }

func (b *ChanBus) Publish(ctx context.Context, ev Event) error {
	select {
	case b.ch <- ev:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
