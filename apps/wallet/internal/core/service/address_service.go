package service

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"gopherex.com/apps/wallet/internal/domain"
	"gopherex.com/pkg/hdwallet"
	"gopherex.com/pkg/logger"
	"gorm.io/gorm"
)

type AddressService struct {
	db     *gorm.DB // 需要用事务，所以直接依赖 DB
	repo   domain.AddressRepo
	wallet *hdwallet.HDWallet
}

func NewAddressService(db *gorm.DB, repo domain.AddressRepo, wallet *hdwallet.HDWallet) *AddressService {
	return &AddressService{
		db:     db,
		repo:   repo,
		wallet: wallet,
	}
}

// GenerateAddress 为用户生成全套地址 (BTC + ETH)
func (s *AddressService) GenerateAddress(ctx context.Context, uid int64) (string, string, error) {
	// 1. 计算 BTC 地址
	btcAddr, _, err := s.wallet.DeriveAddress(domain.CoinTypeBTC, uint32(uid))
	if err != nil {
		return "", "", fmt.Errorf("derive btc failed: %w", err)
	}

	// 2. 计算 ETH 地址
	ethAddr, _, err := s.wallet.DeriveAddress(domain.CoinTypeETH, uint32(uid))
	if err != nil {
		return "", "", fmt.Errorf("derive eth failed: %w", err)
	}

	// // 3. 开启事务，同时保存
	err = s.repo.Transaction(ctx, func(txCtx context.Context) error {
		// 保存 BTC
		if err := s.repo.Save(txCtx, &domain.UserAddress{
			UserID: uid, Chain: "BTC", Address: btcAddr, PkhIdx: int(uid),
		}); err != nil {
			return err
		}
		// 保存 ETH
		if err := s.repo.Save(txCtx, &domain.UserAddress{
			UserID: uid, Chain: "ETH", Address: ethAddr, PkhIdx: int(uid),
		}); err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		logger.Error(ctx, "Save addresses failed", zap.Error(err))
		return "", "", err
	}

	logger.Info(ctx, "✅ 地址生成成功",
		zap.Int64("uid", uid),
		zap.String("btc", btcAddr),
		zap.String("eth", ethAddr))

	return btcAddr, ethAddr, nil
}
