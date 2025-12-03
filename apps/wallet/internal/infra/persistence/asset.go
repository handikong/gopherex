package persistence

import (
	"context"
	"errors"
	"fmt"

	"github.com/shopspring/decimal"
	"gopherex.com/apps/wallet/internal/domain"
	"gopherex.com/pkg/xerr"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ========== AssetRepo 接口实现 ==========

// AddBalance 实现原子加钱（实现 domain.AssetRepo 接口）
func (r *Repo) AddBalance(ctx context.Context, uid int64, symbol string, amount decimal.Decimal) error {
	// 1. 获取事务 DB (如果 ctx 里有事务，就用事务)
	db := r.db
	if tx, ok := ctx.Value("tx_db").(*gorm.DB); ok {
		db = tx
	}

	asset := domain.UserAsset{
		UserID:     uid,
		CoinSymbol: symbol,
		Available:  amount,
	}

	// 2. 执行 Upsert (存在则更新，不存在则插入)
	err := db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "coin_symbol"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"available": gorm.Expr("available + ?", amount), // 🔥 核心：余额累加
			"version":   gorm.Expr("version + 1"),           // 版本号自增
		}),
	}).Create(&asset).Error

	if err != nil {
		return xerr.New(xerr.DbError, fmt.Sprintf("add balance failed: %v", err))
	}
	return nil
}

// 🔥 新增：GetBalance 实现
func (r *Repo) GetBalance(ctx context.Context, uid int64, symbol string) (*domain.UserAsset, error) {
	// 1. 获取 DB (支持事务传播)
	db := r.db
	if tx, ok := ctx.Value("tx_db").(*gorm.DB); ok {
		db = tx
	}

	var asset domain.UserAsset
	err := db.WithContext(ctx).
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
		return nil, xerr.New(xerr.DbError, fmt.Sprintf("get balance failed: %v", err))
	}

	// 4. 成功找到记录
	return &asset, nil
}
