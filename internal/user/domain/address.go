package domain

import (
	"context"
	"time"
)

// UserAddress 用户地址实体
type UserAddress struct {
	ID        int64
	UserID    int64     `gorm:"uniqueIndex:uniq_user_chain"`
	Chain     string    `gorm:"uniqueIndex:uniq_user_chain;size:10"` // "BTC", "ETH"
	Address   string    `gorm:"uniqueIndex:uniq_address;size:100"`   // 必须唯一
	PkhIdx    int       // HD Path Index (通常等于 UserID)
	CreatedAt time.Time
}

// TableName 指定表名
func (UserAddress) TableName() string {
	return "user_addresses"
}

// AddressRepo 用户地址仓储接口
type AddressRepo interface {
	// Save 保存地址
	Save(ctx context.Context, userAddress *UserAddress) error
	// GetByUserID 根据用户ID获取所有地址
	GetByUserID(ctx context.Context, userID int64) ([]*UserAddress, error)
	// GetByUserIDAndChain 根据用户ID和链获取地址
	GetByUserIDAndChain(ctx context.Context, userID int64, chain string) (*UserAddress, error)
	// GetUserIDByAddress 根据地址获取用户ID
	GetUserIDByAddress(ctx context.Context, address string) (int64, error)
	// Transaction 事务支持
	Transaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// CoinType 币种类型常量
const (
	CoinTypeBTC = 0
	CoinTypeETH = 60
)

