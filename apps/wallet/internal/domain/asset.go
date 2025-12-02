package domain

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

type UserAsset struct {
	ID         int64
	UserID     int64           `gorm:"uniqueIndex:idx_user_coin"`
	CoinSymbol string          `gorm:"uniqueIndex:idx_user_coin;size:20"`
	Available  decimal.Decimal `gorm:"type:decimal(36,18);default:0"`
	Frozen     decimal.Decimal `gorm:"type:decimal(36,18);default:0"`
	Version    int64           `gorm:"default:0"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// AssetRepo 接口
type AssetRepo interface {
	// AddBalance 给用户加钱 (支持不存在则创建)
	AddBalance(ctx context.Context, uid int64, symbol string, amount decimal.Decimal) error
}
