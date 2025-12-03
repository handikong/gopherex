package persistence

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"
	"gopherex.com/apps/wallet/internal/domain"
	"gopherex.com/pkg/xerr"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FreezeBalance 原子性冻结余额 (乐观锁)
// SQL: UPDATE user_assets SET available = available - ?, frozen = frozen + ?, version = version + 1
//
//	WHERE user_id = ? AND coin_symbol = ? AND available >= ? AND version = ?
func (r *Repo) FreezeBalance(ctx context.Context, asset *domain.UserAsset, freezeAmount decimal.Decimal) error {
	db := r.db
	if tx, ok := ctx.Value("tx_db").(*gorm.DB); ok {
		db = tx
	}

	// 准备更新的字段
	updates := map[string]interface{}{
		"available": gorm.Expr("available - ?", freezeAmount),
		"frozen":    gorm.Expr("frozen + ?", freezeAmount),
		"version":   gorm.Expr("version + 1"),
	}

	// 执行带乐观锁的更新
	res := db.WithContext(ctx).Model(&domain.UserAsset{}).
		Where("user_id = ? AND coin_symbol = ? AND available >= ? AND version = ?",
			asset.UserID,
			asset.CoinSymbol,
			freezeAmount,
			asset.Version, // 传入当前版本号
		).
		Updates(updates)

	if res.Error != nil {
		return xerr.New(xerr.DbError, fmt.Sprintf("freeze balance failed: %v", res.Error))
	}
	if res.RowsAffected == 0 {
		// 1 行都没更新？说明乐观锁失败（别人抢先改了）或余额不足
		return xerr.New(xerr.ServerCommonError, "并发冲突或余额不足，请重试")
	}

	return nil
}

// CreateWithdrawOrder 创建提现订单
func (r *Repo) CreateWithdrawOrder(ctx context.Context, order *domain.Withdraw) error {
	db := r.db
	if tx, ok := ctx.Value("tx_db").(*gorm.DB); ok {
		db = tx
	}

	if err := db.WithContext(ctx).Create(order).Error; err != nil {
		return xerr.New(xerr.DbError, fmt.Sprintf("create withdraw order failed: %v", err))
	}
	return nil
}

// 1. FindPendingWithdrawsForUpdate 查找并锁定 (FOR UPDATE SKIP LOCKED)
func (r *Repo) FindPendingWithdrawsForUpdate(ctx context.Context, chain string, status domain.WithdrawStatus, limit int) ([]domain.Withdraw, error) {
	var orders []domain.Withdraw
	db := r.db
	if tx, ok := ctx.Value("tx_db").(*gorm.DB); ok {
		db = tx
	}
	// 自动复用 ctx 中的事务
	err := db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("chain = ? AND status = ?", chain, status).
		Limit(limit).
		Find(&orders).Error
	return orders, err
}

// 2. UpdateWithdrawStatusBatch 批量更新状态
func (r *Repo) UpdateWithdrawStatusBatch(ctx context.Context, ids []int64, status domain.WithdrawStatus) error {
	db := r.db
	if tx, ok := ctx.Value("tx_db").(*gorm.DB); ok {
		db = tx
	}
	return db.WithContext(ctx).Model(&domain.Withdraw{}).
		Where("id IN ?", ids).
		Update("status", status).Error
}

// 3. UpdateWithdrawResult 单条更新结果 (成功/失败)
func (r *Repo) UpdateWithdrawResult(ctx context.Context, id int64, txHash string, status domain.WithdrawStatus, errMsg string) error {
	updates := map[string]interface{}{
		"tx_hash":   txHash,
		"status":    status,
		"error_msg": errMsg,
	}
	db := r.db
	if tx, ok := ctx.Value("tx_db").(*gorm.DB); ok {
		db = tx
	}
	return db.WithContext(ctx).Model(&domain.Withdraw{}).
		Where("id = ?", id).
		Updates(updates).Error
}
