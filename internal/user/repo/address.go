package repo

import (
	"context"
	"errors"
	"fmt"

	"gopherex.com/internal/user/domain"
	"gopherex.com/pkg/xerr"
	"gorm.io/gorm"
)

// Save 保存地址
func (r *Repo) Save(ctx context.Context, userAddress *domain.UserAddress) error {
	// 检查是否已存在相同 user_id 和 chain 的记录
	var existing domain.UserAddress
	err := r.getDb(ctx).WithContext(ctx).
		Where("user_id = ? AND chain = ?", userAddress.UserID, userAddress.Chain).
		First(&existing).Error

	if err == nil {
		// 记录已存在，返回错误
		return xerr.New(xerr.DbError, fmt.Sprintf("address already exists for user_id=%d, chain=%s", userAddress.UserID, userAddress.Chain))
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		// 其他数据库错误
		return xerr.New(xerr.DbError, fmt.Sprintf("check existing address failed: %v", err))
	}

	// 记录不存在，执行插入
	err = r.getDb(ctx).WithContext(ctx).Create(userAddress).Error
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return xerr.New(xerr.DbError, "地址已存在")
		}
		return xerr.New(xerr.DbError, fmt.Sprintf("save address failed: %v", err))
	}
	return nil
}

// GetByUserID 根据用户ID获取所有地址
func (r *Repo) GetByUserID(ctx context.Context, userID int64) ([]*domain.UserAddress, error) {
	var addresses []*domain.UserAddress
	err := r.getDb(ctx).WithContext(ctx).
		Where("user_id = ?", userID).
		Find(&addresses).Error

	if err != nil {
		return nil, xerr.New(xerr.DbError, fmt.Sprintf("get addresses by user id failed: %v", err))
	}
	return addresses, nil
}

// GetByUserIDAndChain 根据用户ID和链获取地址
func (r *Repo) GetByUserIDAndChain(ctx context.Context, userID int64, chain string) (*domain.UserAddress, error) {
	var address domain.UserAddress
	err := r.getDb(ctx).WithContext(ctx).
		Where("user_id = ? AND chain = ?", userID, chain).
		First(&address).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // 没找到，返回 nil
		}
		return nil, xerr.New(xerr.DbError, fmt.Sprintf("get address by user id and chain failed: %v", err))
	}
	return &address, nil
}

// GetUserIDByAddress 根据地址获取用户ID（实现 domain.AddressRepo 接口）
func (r *Repo) GetUserIDByAddress(ctx context.Context, address string) (int64, error) {
	var userAddress domain.UserAddress
	err := r.getDb(ctx).WithContext(ctx).
		Where("address = ?", address).
		First(&userAddress).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, xerr.New(xerr.RequestParamsError, "地址无对应用户")
		}
		return 0, xerr.New(xerr.DbError, fmt.Sprintf("get user id by address failed: %v", err))
	}
	return userAddress.UserID, nil
}

