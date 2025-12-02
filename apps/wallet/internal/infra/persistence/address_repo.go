package persistence

import (
	"context"
	"fmt"

	"gopherex.com/apps/wallet/internal/domain"
	"gopherex.com/pkg/xerr"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Save 保存地址（实现 domain.AddressRepo 接口）
func (r *Repo) Save(ctx context.Context, addr *domain.UserAddress) error {
	// 使用 Clauses 忽略重复键错误 (INSERT IGNORE)
	// 防止用户重复点击生成导致报错
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		DoNothing: true,
	}).Create(addr).Error

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
