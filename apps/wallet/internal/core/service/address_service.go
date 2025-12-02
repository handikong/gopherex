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
	btcAddr, _, err := s.wallet.DeriveAddress(0, uint32(uid))
	if err != nil {
		return "", "", fmt.Errorf("derive btc failed: %w", err)
	}

	// 2. 计算 ETH 地址
	ethAddr, _, err := s.wallet.DeriveAddress(60, uint32(uid))
	if err != nil {
		return "", "", fmt.Errorf("derive eth failed: %w", err)
	}

	// 3. 开启事务，同时保存
	err = s.db.Transaction(func(tx *gorm.DB) error {
		// 这里比较 tricky：因为 persistence.Repo 绑定了 db 实例
		// 在事务中，我们应该使用 tx 及其关联的 repo。
		// 简单起见，我们直接用 gorm 操作，或者你也给 Repo 加一个 WithTx 方法。
		// 这里为了演示清晰，直接用 tx Create。

		// 保存 BTC
		if err := tx.Create(&domain.UserAddress{
			UserID: uid, Chain: "BTC", Address: btcAddr, PkhIdx: int(uid),
		}).Error; err != nil {
			return err
		}

		// 保存 ETH
		if err := tx.Create(&domain.UserAddress{
			UserID: uid, Chain: "ETH", Address: ethAddr, PkhIdx: int(uid),
		}).Error; err != nil {
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
