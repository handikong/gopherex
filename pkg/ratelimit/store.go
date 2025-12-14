package ratelimit

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

type entry struct {
	limiter  *rate.Limiter
	lastSeen int64 // unix nano  最后的时间
}

type Store struct {
	mu      sync.Mutex
	entries map[string]*entry
	rate    rate.Limit
	burst   int
	ttl     time.Duration
}

func NewStore(r rate.Limit, burst int, ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &Store{
		entries: make(map[string]*entry, 1024),
		rate:    r,
		burst:   burst,
		ttl:     ttl,
	}

}

// Allow 判断是否允许通过。允许则返回 true。
func (s *Store) Allow(key string) bool {
	now := time.Now().UnixNano()

	s.mu.Lock()
	e, ok := s.entries[key]
	if !ok {
		e = &entry{limiter: rate.NewLimiter(s.rate, s.burst), lastSeen: now}
		s.entries[key] = e
		s.mu.Unlock()
		return e.limiter.Allow()
	}
	atomic.StoreInt64(&e.lastSeen, now)
	s.mu.Unlock()

	return e.limiter.Allow()
}
func (s *Store) Wait(ctx context.Context, key string) error {
	now := time.Now().UnixNano()

	s.mu.Lock()
	e, ok := s.entries[key]
	if !ok {
		e = &entry{limiter: rate.NewLimiter(s.rate, s.burst), lastSeen: now}
		s.entries[key] = e
		s.mu.Unlock()
		return e.limiter.Wait(ctx)
	}
	atomic.StoreInt64(&e.lastSeen, now)
	s.mu.Unlock()

	return e.limiter.Wait(ctx)
}

func (s *Store) StartJanitor(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = time.Minute
	}
	ticker := time.NewTicker(every)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.cleanup()
			}
		}
	}()
}

func (s *Store) cleanup() {
	cut := time.Now().Add(-s.ttl).UnixNano()

	s.mu.Lock()
	for k, e := range s.entries {
		if atomic.LoadInt64(&e.lastSeen) < cut {
			delete(s.entries, k)
		}
	}
	s.mu.Unlock()
}
