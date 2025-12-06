package repo

import (
	"context"
	"errors"
	"fmt"

	"gopherex.com/internal/user/domain"
	"gopherex.com/pkg/xerr"
	"gorm.io/gorm"
)

// Create 创建用户
func (r *Repo) Create(ctx context.Context, user *domain.User) error {
	err := r.getDb(ctx).WithContext(ctx).Create(user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return xerr.New(xerr.RequestParamsError, "用户名、邮箱或手机号已存在")
		}
		return xerr.New(xerr.DbError, fmt.Sprintf("create user failed: %v", err))
	}
	return nil
}

// GetByID 根据ID获取用户
func (r *Repo) GetByID(ctx context.Context, id int64) (*domain.User, error) {
	var user domain.User
	err := r.getDb(ctx).WithContext(ctx).
		Where("id = ?", id).
		First(&user).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, xerr.New(xerr.RequestParamsError, "用户不存在")
		}
		return nil, xerr.New(xerr.DbError, fmt.Sprintf("get user by id failed: %v", err))
	}
	return &user, nil
}

// GetByUsername 根据用户名获取用户
func (r *Repo) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	var user domain.User
	err := r.getDb(ctx).WithContext(ctx).
		Where("username = ?", username).
		First(&user).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, xerr.New(xerr.RequestParamsError, "用户不存在")
		}
		return nil, xerr.New(xerr.DbError, fmt.Sprintf("get user by username failed: %v", err))
	}
	return &user, nil
}

// GetByEmail 根据邮箱获取用户
func (r *Repo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	var user domain.User
	err := r.getDb(ctx).WithContext(ctx).
		Where("email = ?", email).
		First(&user).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, xerr.New(xerr.RequestParamsError, "用户不存在")
		}
		return nil, xerr.New(xerr.DbError, fmt.Sprintf("get user by email failed: %v", err))
	}
	return &user, nil
}

// GetByPhone 根据手机号获取用户
func (r *Repo) GetByPhone(ctx context.Context, phone string) (*domain.User, error) {
	var user domain.User
	err := r.getDb(ctx).WithContext(ctx).
		Where("phone = ?", phone).
		First(&user).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, xerr.New(xerr.RequestParamsError, "用户不存在")
		}
		return nil, xerr.New(xerr.DbError, fmt.Sprintf("get user by phone failed: %v", err))
	}
	return &user, nil
}

// GetUserIDByAddress 根据地址获取用户ID（用于钱包服务）
// 这个方法在 address.go 中实现，因为 UserRepo 和 AddressRepo 都需要这个方法
// Repo 同时实现两个接口，所以只需要一个实现即可

// Update 更新用户信息
func (r *Repo) Update(ctx context.Context, user *domain.User) error {
	err := r.getDb(ctx).WithContext(ctx).
		Model(&domain.User{}).
		Where("id = ?", user.ID).
		Updates(user).Error

	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return xerr.New(xerr.RequestParamsError, "用户名、邮箱或手机号已存在")
		}
		return xerr.New(xerr.DbError, fmt.Sprintf("update user failed: %v", err))
	}
	return nil
}

// UpdateStatus 更新用户状态
func (r *Repo) UpdateStatus(ctx context.Context, id int64, status domain.UserStatus) error {
	res := r.getDb(ctx).WithContext(ctx).
		Model(&domain.User{}).
		Where("id = ?", id).
		Update("status", status)

	if res.Error != nil {
		return xerr.New(xerr.DbError, fmt.Sprintf("update user status failed: %v", res.Error))
	}
	if res.RowsAffected == 0 {
		return xerr.New(xerr.RequestParamsError, "用户不存在")
	}
	return nil
}

// UpdateLastLogin 更新最后登录信息
func (r *Repo) UpdateLastLogin(ctx context.Context, id int64, ip string) error {
	res := r.getDb(ctx).WithContext(ctx).
		Model(&domain.User{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"last_login_at": gorm.Expr("NOW()"),
			"last_login_ip": ip,
		})

	if res.Error != nil {
		return xerr.New(xerr.DbError, fmt.Sprintf("update last login failed: %v", res.Error))
	}
	if res.RowsAffected == 0 {
		return xerr.New(xerr.RequestParamsError, "用户不存在")
	}
	return nil
}

