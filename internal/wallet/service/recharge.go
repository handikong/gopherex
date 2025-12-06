package service

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"gopherex.com/internal/wallet/domain"
	"gopherex.com/internal/wallet/repo"
	watcherDomain "gopherex.com/internal/watcher/domain"
	"gopherex.com/pkg/xerr"
	"gorm.io/gorm"
)

// UserService 用户服务接口（用于获取用户信息）
// TODO: 后续通过 gRPC 调用用户服务实现
type UserService interface {
	// GetUserIDByAddress 根据地址获取用户ID
	GetUserIDByAddress(ctx context.Context, address string) (int64, error)
}

type RechargeService struct {
	repo        *repo.Repo // 使用 Day 16 优化的聚合接口
	redisClinet *redis.Client
	userService UserService // 用户服务接口（用于后续 gRPC 调用）
}

func NewRechargeService(db *gorm.DB, redisClient *redis.Client, userService UserService) *RechargeService {
	repo := repo.New(db)
	return &RechargeService{
		repo:        repo,
		redisClinet: redisClient,
		userService: userService,
	}
}

// 根据信息获取充值记录
func (r *RechargeService) GetListById(ctx context.Context, uid string,
	chain string, Symbol string, status domain.RechargeType,
	page int, limit int) ([]*domain.Recharge, error) {
	return r.repo.GetRechargeListById(ctx, uid, chain, Symbol, status, page, limit)
}

// CreateDeposit 充值入库
// 参数：ChainTransfer（链上转账信息）
// 1. 创建充值记录，状态为0（Pending）
// 2. 冻结金额为充值金额（frozen += amount）
// 使用事务保证强一致性
func (r *RechargeService) CreateDeposit(ctx context.Context, transfer *watcherDomain.ChainTransfer) (*domain.Recharge, error) {
	// 1. 根据地址获取用户ID（通过用户服务接口，后续通过 gRPC 调用）
	if r.userService == nil {
		return nil, xerr.New(xerr.ServerCommonError, "用户服务未初始化")
	}
	uid, err := r.userService.GetUserIDByAddress(ctx, transfer.ToAddress)
	if err != nil {
		return nil, fmt.Errorf("get user id by address failed: %w", err)
	}
	if uid == 0 {
		return nil, xerr.New(xerr.RequestParamsError, "充值地址无对应用户")
	}

	var deposit *domain.Recharge

	// 2. 开启事务
	err = r.repo.Transaction(ctx, func(txCtx context.Context) error {
		// A. 创建充值记录（状态为0：Pending）
		deposit = &domain.Recharge{
			TxHash:      transfer.TxHash,
			LogIndex:    transfer.LogIndex,
			BlockHeight: transfer.BlockHeight,
			FromAddress: transfer.FromAddress,
			ToAddress:   transfer.ToAddress,
			ToUid:       int(uid),
			Chain:       transfer.Chain,
			Symbol:      transfer.Symbol,
			Amount:      transfer.Amount,
			Status:      domain.RechargeStatusPending, // 状态为0
		}

		if err := r.repo.CreateDeposit(txCtx, deposit); err != nil {
			return fmt.Errorf("create deposit failed: %w", err)
		}

		// B. 获取或创建用户资产记录（用于乐观锁）
		asset, err := r.repo.GetBalance(txCtx, uid, transfer.Symbol)
		if err != nil {
			return fmt.Errorf("get balance failed: %w", err)
		}

		// C. 冻结金额（充值场景：直接增加冻结金额）
		if err := r.repo.AddFrozenBalance(txCtx, asset, transfer.Amount); err != nil {
			return fmt.Errorf("add frozen balance failed: %w", err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return deposit, nil
}

// ConfirmDeposit 修改状态：将冻结金额转为可用金额
// 参数：充值记录ID
// 1. 修改状态：0（Pending）-> 1（Confirmed）
// 2. 冻结金额转为可用金额（frozen -= amount, available += amount）
// 使用事务保证强一致性
func (r *RechargeService) ConfirmDeposit(ctx context.Context, depositID int64) error {
	// 1. 先查询充值记录，获取用户ID和金额
	deposit, err := r.repo.GetDeposit(ctx, depositID)
	if err != nil {
		return err
	}

	// 2. 开启事务
	err = r.repo.Transaction(ctx, func(txCtx context.Context) error {
		// A. 修改充值记录状态：Pending -> Confirmed
		if err := r.repo.UpdateDepositStatusToConfirmed(txCtx, depositID); err != nil {
			return fmt.Errorf("update deposit status failed: %w", err)
		}

		// B. 获取用户资产记录（用于乐观锁）
		asset, err := r.repo.GetBalance(txCtx, int64(deposit.ToUid), deposit.Symbol)
		if err != nil {
			return fmt.Errorf("get balance failed: %w", err)
		}

		// C. 将冻结金额转为可用金额
		if err := r.repo.UnfreezeBalanceForDeposit(txCtx, asset, deposit.Amount); err != nil {
			return fmt.Errorf("unfreeze balance failed: %w", err)
		}

		return nil
	})

	if err != nil {
		return err
	}

	return nil
}
