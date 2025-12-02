package persistence

import (
	"context"
	"fmt"

	"gopherex.com/apps/wallet/internal/domain"
	"gopherex.com/pkg/xerr"
	"gorm.io/gorm"
)

// ========== AddressRepo 接口实现 ==========

// Save 保存地址（实现 domain.AddressRepo 接口）
func (r *Repo) Save(ctx context.Context, addr *domain.UserAddress) error {
	db := r.db
	// 如果 context 里有事务对象，就用事务对象
	if tx, ok := ctx.Value("tx_db").(*gorm.DB); ok {
		db = tx
	}

	// 检查是否已存在相同 user_id 和 chain 的记录
	var existing domain.UserAddress
	err := db.WithContext(ctx).
		Where("user_id = ? AND chain = ?", addr.UserID, addr.Chain).
		First(&existing).Error

	if err == nil {
		// 记录已存在，返回错误
		return xerr.New(xerr.DbError, fmt.Sprintf("address already exists for user_id=%d, chain=%s", addr.UserID, addr.Chain))
	}
	if err != gorm.ErrRecordNotFound {
		// 其他数据库错误
		return xerr.New(xerr.DbError, fmt.Sprintf("check existing address failed: %v", err))
	}

	// 记录不存在，执行插入
	err = db.WithContext(ctx).Create(addr).Error
	if err != nil {
		return xerr.New(xerr.DbError, fmt.Sprintf("save address failed: %v", err))
	}
	return nil
}

// GetUserIDByAddress 根据地址查用户（实现 domain.AddressRepo 接口）
func (r *Repo) GetUserIDByAddress(ctx context.Context, address string) (int64, error) {
	var userAddr domain.UserAddress

	err := r.db.WithContext(ctx).
		Where("address = ?", address).
		First(&userAddr).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return 0, nil // 没找到
		}
		return 0, err
	}
	return userAddr.UserID, nil
}

// GetUserById 根据用户ID获取地址（实现 domain.AddressRepo 接口）
func (r *Repo) GetUserById(ctx context.Context, id int) (*domain.UserAddress, error) {
	var userAddr domain.UserAddress

	err := r.db.WithContext(ctx).
		Where("id = ?", id).
		First(&userAddr).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil // 没找到
		}
		return nil, err
	}
	return &userAddr, nil
}
