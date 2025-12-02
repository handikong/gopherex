package domain

import (
	"context"
	"time"
)

// 用户地址类型
type UserAddress struct {
	ID        int64
	UserID    int64  `gorm:"uniqueIndex:idx_uid_chain"`
	Chain     string `gorm:"uniqueIndex:idx_uid_chain"` // "BTC", "ETH"
	Address   string `gorm:"uniqueIndex"`               // 必须唯一
	PkhIdx    int    // HD Path Index (通常等于 UserID)
	CreatedAt time.Time
}

// 用户地址接口
type AddressRepo interface {
	// 保存数据
	Save(ctx context.Context, userAddress *UserAddress) error
	// 根据地址获取id
	GetUserIDByAddress(ctx context.Context, address string) (int64, error)
	// 根据UserId获取
	GetUserById(ctx context.Context, id int) (*UserAddress, error)
	// 新增：允许在事务中执行业务逻辑
	Transaction(ctx context.Context, fn func(ctx context.Context) error) error
}
