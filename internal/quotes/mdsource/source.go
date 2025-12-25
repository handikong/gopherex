package mdsource

import (
	"context"

	"gopherex.com/internal/quotes/kline"
)

// Source：一个“可插拔”的市场数据源。
// Run 必须阻塞运行：持续产出 Trade，直到 ctx.Done() 或发生不可恢复错误。
type Source interface {
	Name() string
	Run(ctx context.Context, out chan<- kline.Trade) error
}
