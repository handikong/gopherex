package service

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"gopherex.com/apps/wallet/internal/domain"
	"gopherex.com/pkg/logger"
)

// AssetService 资产服务
type AssetService struct {
	addressRepo domain.AddressRepo // 地址相关操作
	assetRepo   domain.AssetRepo   // 资产相关操作
	depositRepo domain.Repository  // 充值记录相关操作
}

func NewAssetService(addressRepo domain.AddressRepo, assetRepo domain.AssetRepo, depositRepo domain.Repository) *AssetService {
	return &AssetService{
		addressRepo: addressRepo,
		assetRepo:   assetRepo,
		depositRepo: depositRepo,
	}
}

// SettleDeposit 充值入账结算 (事务原子性)
func (s *AssetService) SettleDeposit(ctx context.Context, deposit *domain.Deposit) error {
	logger.Info(ctx, "💰 开始处理入账",
		zap.String("tx", deposit.TxHash),
		zap.String("amount", deposit.Amount.String()),
		zap.String("symbol", deposit.Symbol),
	)

	// 1. 查找用户 ID
	// 这一步是只读的，可以不在事务里，减少锁时间
	uid, err := s.addressRepo.GetUserIDByAddress(ctx, deposit.ToAddress)
	if err != nil {
		return fmt.Errorf("check user failed: %w", err)
	}
	if uid == 0 {
		// 严重异常：充值地址找不到对应用户（可能是系统测试数据或黑客攻击）
		// 可以选择标记为异常状态，或者记录日志后忽略
		logger.Error(ctx, "❌ 充值地址无对应用户", zap.String("addr", deposit.ToAddress))
		return nil
	}

	// 2. 开启事务 (Transaction)
	// 注意：由于所有接口都由同一个 Repo 实现，Transaction 方法在 AddressRepo 中
	err = s.addressRepo.Transaction(ctx, func(txCtx context.Context) error {
		// A. 修改充值记录状态 (Pending -> Confirmed)
		// 如果这一步失败（比如已经被别人处理了），整个事务回滚
		if err := s.depositRepo.UpdateDepositStatusToConfirmed(txCtx, deposit.ID); err != nil {
			return err
		}

		// B. 给用户加钱
		if err := s.assetRepo.AddBalance(txCtx, uid, deposit.Symbol, deposit.Amount); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		logger.Error(ctx, "❌ 入账事务失败", zap.Error(err))
		return err
	}

	logger.Info(ctx, "✅ 入账成功", zap.Int64("uid", uid), zap.String("tx", deposit.TxHash))
	return nil
}
