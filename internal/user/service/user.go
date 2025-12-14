package service

import (
	"context"
	"fmt"

	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"gopherex.com/internal/user/domain"
	"gopherex.com/internal/user/repo"
	"gopherex.com/pkg/hdwallet"
	"gopherex.com/pkg/xerr"
	"gorm.io/gorm"
)

type UserService struct {
	repo   *repo.Repo
	wallet *hdwallet.HDWallet
}

func NewUserService(db *gorm.DB, wallet *hdwallet.HDWallet) *UserService {
	repo := repo.New(db)
	return &UserService{
		repo:   repo,
		wallet: wallet,
	}
}

// UserWithAddresses 包含地址的用户信息
type UserWithAddresses struct {
	*domain.User
	Addresses []*domain.UserAddress `json:"addresses"`
}

// CreateUser 创建用户
// 参数：用户名、邮箱、手机号、密码
func (s *UserService) CreateUser(ctx context.Context, username, email, phone, password string) (*domain.User, error) {
	// 1. 验证参数
	if username == "" {
		return nil, xerr.New(codes.Internal, "用户名不能为空")
	}
	if email == "" {
		return nil, xerr.New(codes.Internal, "邮箱不能为空")
	}
	if password == "" {
		return nil, xerr.New(codes.Internal, "密码不能为空")
	}

	// 2. 检查用户名是否已存在
	_, err := s.repo.GetByUsername(ctx, username)
	if err == nil {
		return nil, xerr.New(codes.Internal, "用户名已存在")
	}
	// 如果错误不是"用户不存在"，说明是其他错误，需要返回
	if err != nil && !isNotFoundError(err) {
		return nil, err
	}

	// 3. 检查邮箱是否已存在
	_, err = s.repo.GetByEmail(ctx, email)
	if err == nil {
		return nil, xerr.New(codes.Internal, "邮箱已存在")
	}
	if err != nil && !isNotFoundError(err) {
		return nil, err
	}

	// 4. 如果提供了手机号，检查是否已存在
	if phone != "" {
		_, err = s.repo.GetByPhone(ctx, phone)
		if err == nil {
			return nil, xerr.New(codes.Internal, "手机号已存在")
		}
		if err != nil && !isNotFoundError(err) {
			return nil, err
		}
	}

	// 5. 加密密码
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password failed: %w", err)
	}

	// 6. 创建用户（使用事务，同时创建用户和地址）
	var user *domain.User
	err = s.repo.Transaction(ctx, func(txCtx context.Context) error {
		// 6.1 创建用户
		user = &domain.User{
			Username:     username,
			Email:        email,
			Phone:        phone,
			PasswordHash: string(passwordHash),
			Status:       domain.UserStatusEnabled,
		}

		if err := s.repo.Create(txCtx, user); err != nil {
			return err
		}

		// 6.2 生成 BTC 和 ETH 地址
		if s.wallet == nil {
			return xerr.New(codes.Internal, "钱包服务未初始化")
		}

		// 生成 BTC 地址
		btcAddr, _, err := s.wallet.DeriveAddress(domain.CoinTypeBTC, uint32(user.ID))
		if err != nil {
			return fmt.Errorf("derive btc address failed: %w", err)
		}

		// 生成 ETH 地址
		ethAddr, _, err := s.wallet.DeriveAddress(domain.CoinTypeETH, uint32(user.ID))
		if err != nil {
			return fmt.Errorf("derive eth address failed: %w", err)
		}

		// 6.3 保存地址
		if err := s.repo.Save(txCtx, &domain.UserAddress{
			UserID:  user.ID,
			Chain:   "BTC",
			Address: btcAddr,
			PkhIdx:  int(user.ID),
		}); err != nil {
			return fmt.Errorf("save btc address failed: %w", err)
		}

		if err := s.repo.Save(txCtx, &domain.UserAddress{
			UserID:  user.ID,
			Chain:   "ETH",
			Address: ethAddr,
			PkhIdx:  int(user.ID),
		}); err != nil {
			return fmt.Errorf("save eth address failed: %w", err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return user, nil
}

// GetUserByID 根据ID获取用户（包含地址）
func (s *UserService) GetUserByID(ctx context.Context, id int64) (*UserWithAddresses, error) {
	user, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	addresses, err := s.repo.GetByUserID(ctx, id)
	if err != nil {
		return nil, err
	}

	return &UserWithAddresses{
		User:      user,
		Addresses: addresses,
	}, nil
}

// GetUserByUsername 根据用户名获取用户（包含地址）
func (s *UserService) GetUserByUsername(ctx context.Context, username string) (*UserWithAddresses, error) {
	user, err := s.repo.GetByUsername(ctx, username)
	if err != nil {
		return nil, err
	}

	addresses, err := s.repo.GetByUserID(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	return &UserWithAddresses{
		User:      user,
		Addresses: addresses,
	}, nil
}

// GetUserByEmail 根据邮箱获取用户（包含地址）
func (s *UserService) GetUserByEmail(ctx context.Context, email string) (*UserWithAddresses, error) {
	user, err := s.repo.GetByEmail(ctx, email)
	if err != nil {
		return nil, err
	}

	addresses, err := s.repo.GetByUserID(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	return &UserWithAddresses{
		User:      user,
		Addresses: addresses,
	}, nil
}

// GetUserByPhone 根据手机号获取用户（包含地址）
func (s *UserService) GetUserByPhone(ctx context.Context, phone string) (*UserWithAddresses, error) {
	user, err := s.repo.GetByPhone(ctx, phone)
	if err != nil {
		return nil, err
	}

	addresses, err := s.repo.GetByUserID(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	return &UserWithAddresses{
		User:      user,
		Addresses: addresses,
	}, nil
}

// GetUserIDByAddress 根据地址获取用户ID（用于钱包服务）
func (s *UserService) GetUserIDByAddress(ctx context.Context, address string) (int64, error) {
	return s.repo.GetUserIDByAddress(ctx, address)
}

// Login 用户登录（返回包含地址的用户信息）
// 参数：用户名/邮箱/手机号、密码、登录IP
func (s *UserService) Login(ctx context.Context, account, password, ip string) (*UserWithAddresses, error) {
	// 1. 根据账号查找用户（支持用户名、邮箱、手机号）
	var user *domain.User
	var err error

	// 尝试用户名
	user, err = s.repo.GetByUsername(ctx, account)
	if err == nil {
		goto verifyPassword
	}

	// 尝试邮箱
	user, err = s.repo.GetByEmail(ctx, account)
	if err == nil {
		goto verifyPassword
	}

	// 尝试手机号
	user, err = s.repo.GetByPhone(ctx, account)
	if err != nil {
		return nil, xerr.New(codes.Internal, "账号或密码错误")
	}

verifyPassword:
	// 2. 验证密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, xerr.New(codes.Internal, "账号或密码错误")
	}

	// 3. 检查用户状态
	if user.Status != domain.UserStatusEnabled {
		return nil, xerr.New(codes.Internal, "用户已被禁用")
	}

	// 4. 更新最后登录信息
	if err := s.repo.UpdateLastLogin(ctx, user.ID, ip); err != nil {
		// 登录信息更新失败不影响登录流程，只记录错误
		// logger.Error(ctx, "update last login failed", zap.Error(err))
	}

	// 5. 获取用户地址
	addresses, err := s.repo.GetByUserID(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	return &UserWithAddresses{
		User:      user,
		Addresses: addresses,
	}, nil
}

// UpdateUser 更新用户信息
func (s *UserService) UpdateUser(ctx context.Context, user *domain.User) error {
	// 1. 检查用户是否存在
	_, err := s.repo.GetByID(ctx, user.ID)
	if err != nil {
		return err
	}

	// 2. 如果更新了邮箱，检查是否重复
	if user.Email != "" {
		existingUser, err := s.repo.GetByEmail(ctx, user.Email)
		if err == nil && existingUser.ID != user.ID {
			return xerr.New(codes.Internal, "邮箱已被使用")
		}
	}

	// 3. 如果更新了手机号，检查是否重复
	if user.Phone != "" {
		existingUser, err := s.repo.GetByPhone(ctx, user.Phone)
		if err == nil && existingUser.ID != user.ID {
			return xerr.New(codes.Internal, "手机号已被使用")
		}
	}

	// 4. 更新用户信息
	return s.repo.Update(ctx, user)
}

// UpdatePassword 更新密码
func (s *UserService) UpdatePassword(ctx context.Context, userID int64, oldPassword, newPassword string) error {
	// 1. 获取用户
	user, err := s.repo.GetByID(ctx, userID)
	if err != nil {
		return err
	}

	// 2. 验证旧密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword)); err != nil {
		return xerr.New(codes.Internal, "原密码错误")
	}

	// 3. 加密新密码
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password failed: %w", err)
	}

	// 4. 更新密码
	user.PasswordHash = string(passwordHash)
	return s.repo.Update(ctx, user)
}

// EnableUser 启用用户
func (s *UserService) EnableUser(ctx context.Context, id int64) error {
	return s.repo.UpdateStatus(ctx, id, domain.UserStatusEnabled)
}

// DisableUser 禁用用户
func (s *UserService) DisableUser(ctx context.Context, id int64) error {
	return s.repo.UpdateStatus(ctx, id, domain.UserStatusDisabled)
}

// isNotFoundError 判断是否为"记录不存在"的错误
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	// 检查错误消息是否包含"不存在"或"无对应"
	errMsg := err.Error()
	return contains(errMsg, "不存在") || contains(errMsg, "无对应")
}

// contains 简单的字符串包含检查
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(len(substr) == 0 || indexOf(s, substr) >= 0)
}

// indexOf 查找子字符串位置
func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
