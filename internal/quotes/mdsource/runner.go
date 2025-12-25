package mdsource

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"

	"gopherex.com/internal/quotes/kline"
)

type Runner struct {
	sources []Source

	// Out 是统一 trade 流出口（上层只消费这个）
	Out chan kline.Trade

	// Err 输出源错误（可选，你现在先打印也行）
	Err chan error

	// Backoff 参数（v0 简单版）
	BaseBackoff time.Duration // e.g. 300ms
	MaxBackoff  time.Duration // e.g. 5s
}

func NewRunner(sources ...Source) *Runner {
	return &Runner{
		sources:     sources,
		Out:         make(chan kline.Trade, 64_000),
		Err:         make(chan error, 128),
		BaseBackoff: 300 * time.Millisecond,
		MaxBackoff:  5 * time.Second,
	}
}

func (r *Runner) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, s := range r.sources {
		src := s
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.runOne(ctx, src)
		}()
	}

	go func() {
		wg.Wait()
		close(r.Out)
		close(r.Err)
	}()
}
func (r *Runner) Get() chan kline.Trade {
	return r.Out
}

func (r *Runner) runOne(ctx context.Context, src Source) {
	backoff := r.BaseBackoff
	for {
		if ctx.Err() != nil {
			return
		}

		err := src.Run(ctx, r.Out) // 阻塞直到断线/错误/ctx cancel
		if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}

		// 记录错误（v0：先发到 Err；你暂时不做日志体系也行）
		select {
		case r.Err <- wrapErr(src.Name(), err):
		default:
		}

		// 指数退避 + jitter（避免所有源同时重连造成尖峰）
		sleep := backoff + time.Duration(rand.Int63n(int64(backoff/2+1)))
		if sleep > r.MaxBackoff {
			sleep = r.MaxBackoff
		}

		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		backoff *= 2
		if backoff > r.MaxBackoff {
			backoff = r.MaxBackoff
		}
	}
}

type namedErr struct {
	src string
	err error
}

func (e namedErr) Error() string          { return e.src + ": " + e.err.Error() }
func wrapErr(src string, err error) error { return namedErr{src: src, err: err} }
