// internal/quotes/gateway/broker_mem.go
package gateway

import (
	"context"
	"sync"
)

type MemBroker struct {
	mu   sync.RWMutex
	subs map[string][]chan Message
}

func NewMemBroker() *MemBroker {
	return &MemBroker{subs: make(map[string][]chan Message)}
}

func (b *MemBroker) Publish(ctx context.Context, topic string, payload []byte) error {
	b.mu.RLock()
	list := b.subs[topic]
	b.mu.RUnlock()

	// fanout：at-most-once，慢订阅者直接丢
	msg := Message{Topic: topic, Payload: payload}
	for _, ch := range list {
		select {
		case ch <- msg:
		default:
		}
	}
	return nil
}

func (b *MemBroker) Subscribe(ctx context.Context, topics []string) (<-chan Message, error) {
	ch := make(chan Message, 4096)
	b.mu.Lock()
	for _, t := range topics {
		b.subs[t] = append(b.subs[t], ch)
	}
	b.mu.Unlock()

	// 这里省略 unsubscribe/cleanup（v0 够用）。后面可加引用计数和 ctx.Done 清理。
	go func() {
		<-ctx.Done()
		close(ch)
	}()

	return ch, nil
}

func (b *MemBroker) Close() error { return nil }
