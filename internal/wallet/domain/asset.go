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
	// 无论用户是否存在记录，都应返回一个 UserAsset 实例（或 nil）和 error
	GetBalance(ctx context.Context, uid int64, symbol string) (*UserAsset, error)
	// AddFrozenBalance 充值场景：直接增加冻结金额（不需要从可用余额扣除）
	// 使用乐观锁确保并发安全
	AddFrozenBalance(ctx context.Context, asset *UserAsset, amount decimal.Decimal) error
	// UnfreezeBalanceForDeposit 将冻结金额转为可用金额（充值确认场景）
	// 使用乐观锁确保并发安全
	UnfreezeBalanceForDeposit(ctx context.Context, asset *UserAsset, amount decimal.Decimal) error
}
