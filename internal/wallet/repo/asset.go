package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"gopherex.com/internal/wallet/domain"
	"gopherex.com/pkg/xerr"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AddBalance 实现原子加钱（实现 domain.AssetRepo 接口）
func (r *Repo) AddBalance(ctx context.Context, uid int64, symbol string, amount decimal.Decimal) error {
	asset := domain.UserAsset{
		UserID:     uid,
		CoinSymbol: symbol,
		Available:  amount,
	}

	// 执行 Upsert (存在则更新，不存在则插入)
	err := r.getDb(ctx).WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "coin_symbol"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"available": gorm.Expr("available + ?", amount), // 🔥 核心：余额累加
			"version":   gorm.Expr("version + 1"),           // 版本号自增
		}),
	}).Create(&asset).Error

	if err != nil {
		return xerr.New(codes.Internal, fmt.Sprintf("add balance failed: %v", err))
	}
	return nil
}

// GetBalance 实现
func (r *Repo) GetBalance(ctx context.Context, uid int64, symbol string) (*domain.UserAsset, error) {
	var asset domain.UserAsset
	err := r.getDb(ctx).WithContext(ctx).
		Where("user_id = ? AND coin_symbol = ?", uid, symbol).
		First(&asset).Error

	if err != nil {
		// 2. 🔥 核心逻辑：处理“查无此记录”
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 这不是一个错误。返回一个“零值”的资产对象
			// WithdrawService 会拿到 {Available: 0, Version: 0}
			return &domain.UserAsset{
				UserID:     uid,
				CoinSymbol: symbol,
				Available:  decimal.Zero,
				Frozen:     decimal.Zero,
				Version:    0, // 初始版本号为 0
			}, nil
		}
		// 3. 其他数据库错误
		return nil, xerr.New(codes.Internal, fmt.Sprintf("get balance failed: %v", err))
	}

	// 4. 成功找到记录
	return &asset, nil
}

// AddFrozenBalance 充值场景：直接增加冻结金额（不需要从可用余额扣除）
// SQL: UPDATE user_assets SET frozen = frozen + ?, version = version + 1
// WHERE user_id = ? AND coin_symbol = ? AND version = ?
func (r *Repo) AddFrozenBalance(ctx context.Context, asset *domain.UserAsset, amount decimal.Decimal) error {
	updates := map[string]interface{}{
		"frozen":  gorm.Expr("frozen + ?", amount),
		"version": gorm.Expr("version + 1"),
	}

	// 执行带乐观锁的更新
	res := r.getDb(ctx).WithContext(ctx).Model(&domain.UserAsset{}).
		Where("user_id = ? AND coin_symbol = ? AND version = ?",
			asset.UserID,
			asset.CoinSymbol,
			asset.Version,
		).
		Updates(updates)

	if res.Error != nil {
		return xerr.New(codes.Internal, fmt.Sprintf("add frozen balance failed: %v", res.Error))
	}
	if res.RowsAffected == 0 {
		return xerr.New(codes.Internal, "并发冲突，请重试")
	}

	return nil
}

// UnfreezeBalanceForDeposit 将冻结金额转为可用金额（充值确认场景）
// SQL: UPDATE user_assets SET frozen = frozen - ?, available = available + ?, version = version + 1
// WHERE user_id = ? AND coin_symbol = ? AND frozen >= ? AND version = ?
func (r *Repo) UnfreezeBalanceForDeposit(ctx context.Context, asset *domain.UserAsset, amount decimal.Decimal) error {
	updates := map[string]interface{}{
		"frozen":    gorm.Expr("frozen - ?", amount),
		"available": gorm.Expr("available + ?", amount),
		"version":   gorm.Expr("version + 1"),
	}

	// 执行带乐观锁的更新，确保冻结金额足够
	res := r.getDb(ctx).WithContext(ctx).Model(&domain.UserAsset{}).
		Where("user_id = ? AND coin_symbol = ? AND frozen >= ? AND version = ?",
			asset.UserID,
			asset.CoinSymbol,
			amount,
			asset.Version,
		).
		Updates(updates)

	if res.Error != nil {
		return xerr.New(codes.Internal, fmt.Sprintf("unfreeze balance failed: %v", res.Error))
	}
	if res.RowsAffected == 0 {
		return xerr.New(codes.Internal, "并发冲突或冻结金额不足，请重试")
	}

	return nil
}
