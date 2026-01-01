package app

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

type Registry interface {
	Get(symbol string) (Currency, bool)
	Must(symbol string) Currency
	Parse(symbol string, s string) (int64, error)  // string -> smallest int64
	Format(symbol string, v int64) (string, error) // smallest int64 -> string
}
type Loader func(ctx context.Context) (map[string]Currency, error)

// CachedRegistry：DB -> 内存缓存，支持定时刷新；读路径无锁争用（RWMutex）
type CachedRegistry struct {
	mu     sync.RWMutex
	cache  map[string]Currency
	loader Loader
	ttl    time.Duration
	sf     singleflight.Group
	lastAt time.Time
}

func NewCachedRegistry(loader Loader, ttl time.Duration) *CachedRegistry {
	return &CachedRegistry{
		cache:  make(map[string]Currency),
		loader: loader,
		ttl:    ttl,
	}
}

func (r *CachedRegistry) Get(symbol string) (Currency, bool) {
	r.mu.RLock()
	c, ok := r.cache[symbol]
	r.mu.RUnlock()
	return c, ok
}

func (r *CachedRegistry) Must(symbol string) Currency {
	if c, ok := r.Get(symbol); ok {
		return c
	}
	panic("unknown symbol: " + symbol)
}
func (r *CachedRegistry) EnsureFresh(ctx context.Context) error {
	r.mu.RLock()
	need := r.ttl > 0 && time.Since(r.lastAt) > r.ttl
	r.mu.RUnlock()
	if !need {
		return nil
	}
	_, err, _ := r.sf.Do("reload", func() (any, error) {
		m, err := r.loader(ctx)
		if err != nil {
			return nil, err
		}
		r.mu.Lock()
		r.cache = m
		r.lastAt = time.Now()
		r.mu.Unlock()
		return nil, nil
	})
	return err
}
func (r *CachedRegistry) StartAutoRefresh(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	tk := time.NewTicker(interval)
	go func() {
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				_ = r.EnsureFresh(ctx)
			}
		}
	}()
}
