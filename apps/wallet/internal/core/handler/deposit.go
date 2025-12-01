package handler

import (
	"context"

	"go.uber.org/zap"
	"gopherex.com/apps/wallet/internal/domain"
	"gopherex.com/pkg/logger"
	"gopherex.com/pkg/xerr"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DepositHandler struct {
	db *gorm.DB
	// 内存关注列表 (Key: Address)，用于快速判断是否是交易所用户
	// 生产环境这个列表应该很大，初始化时从数据库加载
	watchlist map[string]bool
}

func NewDepositHandler(db *gorm.DB) *DepositHandler {
	return &DepositHandler{
		db:        db,
		watchlist: make(map[string]bool),
	}
}

// AddWatchAddress 添加要监控的地址 (给 main.go 调用)
func (h *DepositHandler) AddWatchAddress(address string) {
	h.watchlist[address] = true
}

// HandlerBlock 处理区块业务逻辑
func (h *DepositHandler) HandlerBlock(ctx context.Context, block *domain.StandardBlock) error {
	// 1. 筛选出属于我们的充值
	var myDeposits []domain.Deposit

	for _, tx := range block.Transactions {
		// 判断接收地址是否在关注列表中
		// 注意：实际业务中可能需要把地址转为小写比较
		// if h.watchlist[tx.ToAddress] {
		myDeposits = append(myDeposits, tx)

		// 打印日志，方便调试
		logger.Info(ctx, "💰 捕获充值",
			zap.String("chain", tx.Chain),
			zap.String("tx", tx.TxHash),
			zap.String("amount", tx.Amount.String()),
			zap.String("user", tx.ToAddress),
		)
		// }
	}

	// 如果没有感兴趣的交易，直接返回
	if len(myDeposits) == 0 {
		return nil
	}

	// 2. 批量写入数据库 (幂等性核心)
	// 使用 INSERT IGNORE 或者 ON DUPLICATE KEY UPDATE
	// 依赖数据库的唯一索引 uniq_tx (chain, tx_hash, log_index)
	err := h.db.WithContext(ctx).Table("deposits").
		Clauses(clause.OnConflict{
			DoNothing: true, // 如果存在，说明已经处理过，直接忽略 (幂等)
		}).
		Create(&myDeposits).Error

	if err != nil {
		logger.Error(ctx, "保存充值记录失败", zap.Error(err))
		return xerr.New(xerr.DbError, "batch insert deposits failed")
	}

	return nil
}
