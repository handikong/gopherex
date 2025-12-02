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
