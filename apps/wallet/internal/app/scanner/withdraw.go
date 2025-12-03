package scanner

import (
	"context"
	"time"

	"go.uber.org/zap"
	"gopherex.com/apps/wallet/internal/core/service"
	"gopherex.com/apps/wallet/internal/domain"
	"gopherex.com/pkg/logger"
	"gopherex.com/pkg/safe"
)

type WithdrawProcessor struct {
	withdrawSvc *service.WithdrawService
	adapter     domain.ChainAdapter
	chain       string
}

func NewWithdrawProcessor(svc *service.WithdrawService, adapter domain.ChainAdapter, chain string) *WithdrawProcessor {
	return &WithdrawProcessor{
		withdrawSvc: svc,
		adapter:     adapter,
		chain:       chain,
	}
}

func (p *WithdrawProcessor) Start(ctx context.Context) {
	logger.Info(ctx, "🚀 提现执行器启动", zap.String("chain", p.chain))
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// 启动第一个抢单携程
	safe.Go(func() {
		p.process(ctx)
	})
	// 启动第二个提现确认
	safe.Go(func() {
		p.confirmWithdraws(ctx)
	})
	// 3. 🔥 阻塞主协程，直到服务被停止
	// 这样 Start 方法不会立即退出，符合 "Service" 的生命周期管理
	<-ctx.Done()
	logger.Info(ctx, "🛑 提现执行器正在停止...", zap.String("chain", p.chain))
}

func (p *WithdrawProcessor) process(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	// 1. 调用 Service 抢单 (Audited -> Processing)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.ProcessBatch(ctx)
		}
	}

}

func (e *WithdrawProcessor) confirmWithdraws(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.confirmWithdraws(ctx)
		}
	}
}

func (p *WithdrawProcessor) ProcessBatch(ctx context.Context) {
	orders, err := p.withdrawSvc.PickAndLockPendingWithdraws(ctx, p.chain, 10)
	if err != nil {
		logger.Error(ctx, "抢单失败", zap.Error(err))
		return
	}
	if len(orders) == 0 {
		return
	}

	logger.Info(ctx, "🔥 抢单成功", zap.Int("count", len(orders)))

	// 2. 逐条处理发币
	for _, order := range orders {
		// A. 发币
		txHash, err := p.adapter.SendWithdrawal(ctx, &order)

		if err != nil {
			logger.Error(ctx, "❌ 广播失败", zap.Int64("id", order.ID), zap.Error(err))
			// B. 登记失败
			_ = p.withdrawSvc.MarkWithdrawFailed(ctx, order.ID, err.Error())
			continue
		}

		// C. 登记广播成功 (状态仍为 Processing, 填入 Hash)
		logger.Info(ctx, "✅ 广播成功", zap.Int64("id", order.ID), zap.String("hash", txHash))
		_ = p.withdrawSvc.MarkWithdrawBroadcasted(ctx, order.ID, txHash, domain.WithdrawStatusProcessing, "s")
	}
}

func (p *WithdrawProcessor) confirmWithdrawsBatch(ctx context.Context) {
	// A. 捞出 Processing 的单子
	tasks, _ := p.withdrawSvc.GetListForStatus(ctx, p.chain, domain.WithdrawStatusProcessing, 10)
	for _, task := range tasks {
		// B. 去链上查这个 Hash 到底怎么样了
		// Adapter 需要实现 GetTransactionStatus
		// 比如返回：StatusSuccess, StatusFailed, StatusPending, StatusNotFound
		status, err := p.adapter.GetTransactionStatus(ctx, task.TxHash)
		if err != nil {
			continue
		}

		if status == domain.WithdrawStatusConfirmed {
			// C. 成功：改状态为 Confirmed (3)
			p.withdrawSvc.MarkWithdrawBroadcasted(ctx, task.ID, task.TxHash, domain.WithdrawStatusConfirmed, "")

		} else if status == domain.WithdrawStatusFailed {
			// D. 失败：改状态为 Failed (4) -> 这通常需要人工介入或自动解冻
			p.withdrawSvc.MarkWithdrawBroadcasted(ctx, task.ID, task.TxHash, domain.WithdrawStatusFailed, "chain execution failed")
		}
		// 如果是 Pending 或 NotFound，就继续等，不做操作
	}
}
