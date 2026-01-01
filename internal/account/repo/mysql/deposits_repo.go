package mysql

import (
	"context"
	"time"

	"gorm.io/gorm/clause"

	"gopherex.com/internal/account/model"
)

func (r *Repo) AcquireConfirmedDepositsForUpdate(ctx context.Context, limit int) ([]*model.Deposit, error) {
	var rows []*model.Deposit

	// 关键：FOR UPDATE SKIP LOCKED，多个 worker 并发抢任务不会互相阻塞 :contentReference[oaicite:4]{index=4}
	err := r.getDb(ctx).
		Model(&model.Deposit{}).
		Where("status = ? AND credited_at IS NULL", model.DepositConfirmed).
		Order("id ASC").
		Limit(limit).
		Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Find(&rows).Error

	return rows, err
}

func (r *Repo) MarkDepositCredited(ctx context.Context, depositID int64, txnID string) error {
	now := time.Now().UTC()
	// 幂等：只允许从未 credited -> credited
	res := r.getDb(ctx).Model(&model.Deposit{}).
		Where("id = ? AND credited_at IS NULL", depositID).
		Updates(map[string]any{
			"credit_txn_id": txnID,
			"credited_at":   now,
		})
	return res.Error
}
