package domain

import (
	"context"
	"time"
)

// UserStatus 用户状态枚举
type UserStatus uint8

const (
	UserStatusDisabled UserStatus = iota // 禁用
	UserStatusEnabled                    // 启用
)

// User 用户实体
type User struct {
	ID           int64
	Username     string     `gorm:"uniqueIndex:uniq_username;size:50"`
	Email        string     `gorm:"uniqueIndex:uniq_email;size:100"`
	Phone        string     `gorm:"uniqueIndex:uniq_phone;size:20"`
	PasswordHash string     `gorm:"column:password_hash;size:255"`
	Status       UserStatus `gorm:"default:1"`
	LastLoginAt  *time.Time `gorm:"column:last_login_at"`
	LastLoginIP  string     `gorm:"column:last_login_ip;size:50"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// TableName 指定表名
func (User) TableName() string {
	return "users"
}

// UserRepo 用户仓储接口
type UserRepo interface {
	// Create 创建用户
	Create(ctx context.Context, user *User) error
	// GetByID 根据ID获取用户
	GetByID(ctx context.Context, id int64) (*User, error)
	// GetByUsername 根据用户名获取用户
	GetByUsername(ctx context.Context, username string) (*User, error)
	// GetByEmail 根据邮箱获取用户
	GetByEmail(ctx context.Context, email string) (*User, error)
	// GetByPhone 根据手机号获取用户
	GetByPhone(ctx context.Context, phone string) (*User, error)
	// GetByAddress 根据地址获取用户ID（用于钱包服务）
	GetUserIDByAddress(ctx context.Context, address string) (int64, error)
	// Update 更新用户信息
	Update(ctx context.Context, user *User) error
	// UpdateStatus 更新用户状态
	UpdateStatus(ctx context.Context, id int64, status UserStatus) error
	// UpdateLastLogin 更新最后登录信息
	UpdateLastLogin(ctx context.Context, id int64, ip string) error
	// Transaction 事务支持
	Transaction(ctx context.Context, fn func(ctx context.Context) error) error
}

