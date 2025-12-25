package md

import (
	"sync"
	"time"
)

type Topic struct {
	Exchange string
	Symbol   string
	Interval time.Duration
}

type Subscriber struct {
	ch chan KlineEvent
}

type Hub struct {
	mu    sync.RWMutex
	subs  map[Topic]map[*Subscriber]struct{}
	qsize int
}

func NewHub(perSubQueue int) *Hub {
	return &Hub{
		subs:  make(map[Topic]map[*Subscriber]struct{}),
		qsize: perSubQueue,
	}
}
func (h *Hub) Subscribe(t Topic) *Subscriber {
	s := &Subscriber{ch: make(chan KlineEvent, h.qsize)}
	h.mu.Lock()
	m := h.subs[t]
	if m == nil {
		m = make(map[*Subscriber]struct{})
		h.subs[t] = m
	}
	m[s] = struct{}{}
	h.mu.Unlock()
	return s
}
func (h *Hub) Unsubscribe(t Topic, s *Subscriber) {
	h.mu.Lock()
	if m := h.subs[t]; m != nil {
		delete(m, s)
		if len(m) == 0 {
			delete(h.subs, t)
		}
	}
	h.mu.Unlock()
	close(s.ch)
}

// Publish：不阻塞。慢订阅者直接丢（M0策略）
func (h *Hub) Publish(t Topic, k KlineEvent) {
	h.mu.RLock()
	m := h.subs[t]
	h.mu.RUnlock()

	for s := range m {
		select {
		case s.ch <- k:
		default:
			// drop
		}
	}
}

func (s *Subscriber) Chan() <-chan KlineEvent { return s.ch }
