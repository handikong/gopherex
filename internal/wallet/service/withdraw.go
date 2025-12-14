package service

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"gopherex.com/internal/wallet/domain"
	"gopherex.com/internal/wallet/repo"
	"gopherex.com/pkg/logger"
	"gopherex.com/pkg/xerr"
	"gorm.io/gorm"
)

type WithdrawService struct {
	repo        *repo.Repo // 使用 Day 16 优化的聚合接口
	redisClinet *redis.Client
}

func NewWithdrawService(db *gorm.DB, redisClient *redis.Client) *WithdrawService {
	repoDb := repo.New(db)
	return &WithdrawService{repo: repoDb, redisClinet: redisClient}
}

// ApplyWithdraw 申请提现
func (s *WithdrawService) ApplyWithdraw(ctx context.Context, uid int64, chain, symbol, toAddress string, amount decimal.Decimal, requestId string) error {
	logger.Info(ctx, "开始申请提现",
		zap.Int64("uid", uid),
		zap.String("symbol", symbol),
		zap.String("amount", amount.String()))
	// === 第一层：Redis 软防重 (为了性能) ===
	// Key 格式: "idempotent:withdraw:{request_id}"
	cacheKey := fmt.Sprintf("idempotent:withdraw:%s", requestId)
	// 使用 SetNX (Set if Not Exists)
	// 过期时间设为 24小时 (足够覆盖网络重试周期)
	isNew, err := s.redisClinet.SetNX(ctx, cacheKey, "processing", 24*time.Hour).Result()
	if err != nil {
		return xerr.New(codes.Internal, "系统繁忙") // Redis 报错，为了安全可以选择降级或报错
	}
	if !isNew {
		// Redis 里已经有了，直接拦截，不走数据库
		return xerr.New(codes.AlreadyExists, "重复的请求，请稍后查询结果")
	}

	// TODO: 1. 计算手续费 (Fee)
	// 暂时 hardcode
	fee := decimal.RequireFromString("0.0001") // 假设手续费 0.0001

	// 2. 开启事务
	err = s.repo.Transaction(ctx, func(txCtx context.Context) error {
		// A. 查询余额并获取版本号 (为了乐观锁)
		// 注意：GetBalance 也需要用 txCtx 查 (如果 Repo 没实现，需要补充)
		// 假设 AssetRepo 里的 GetBalance 会自动使用 txCtx
		asset, err := s.repo.GetBalance(txCtx, uid, symbol)
		if err != nil {
			return err
		}

		// B. 检查余额
		totalCost := amount.Add(fee)
		if asset.Available.LessThan(totalCost) {
			return xerr.New(codes.InvalidArgument, "可用余额不足")
		}

		// C. 冻结余额 (核心步骤)
		if err := s.repo.FreezeBalance(txCtx, asset, totalCost); err != nil {
			// 这里会捕获乐观锁冲突
			return err
		}

		// D. 创建提现订单
		order := &domain.Withdraw{
			UserID:    uid,
			Chain:     chain,
			Symbol:    symbol,
			Amount:    amount,
			Fee:       fee,
			ToAddress: toAddress,
			RequestID: requestId,
			Status:    domain.WithdrawStatusAudited, // 初始状态：申请中
		}
		if err := s.repo.CreateWithdrawOrder(txCtx, order); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		s.redisClinet.Del(ctx, cacheKey)
	}
	return nil

}

// PickAndLockPendingWithdraws 抢单
func (s *WithdrawService) PickAndLockPendingWithdraws(ctx context.Context, chain string, batchSize int) ([]domain.Withdraw, error) {
	var orders []domain.Withdraw

	// 🔥 开启事务
	err := s.repo.Transaction(ctx, func(txCtx context.Context) error {
		// 1. 抢锁
		var err error
		orders, err = s.GetListForStatus(txCtx, chain, domain.WithdrawStatusAudited, batchSize)
		if err != nil {
			return err
		}
		if len(orders) == 0 {
			return nil
		}

		// 2. 收集 ID
		var ids []int64
		for _, o := range orders {
			ids = append(ids, o.ID)
		}

		// 3. 标记为处理中 (防止被其他人抢)
		if err := s.repo.UpdateWithdrawStatusBatch(txCtx, ids, domain.WithdrawStatusProcessing); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return orders, nil
}

func (s *WithdrawService) GetListForStatus(ctx context.Context, chain string, status domain.WithdrawStatus, batchSize int) ([]domain.Withdraw, error) {
	return s.repo.FindPendingWithdrawsForUpdate(ctx, chain, status, batchSize)
}

// MarkWithdrawBroadcasted 标记广播成功 (依然是 Processing，但有了 Hash)
func (s *WithdrawService) MarkWithdrawBroadcasted(ctx context.Context, id int64, txHash string, status domain.WithdrawStatus, ErrorMsg string) error {
	// 状态保持 Processing，等待 Scanner 确认为 Confirmed
	return s.repo.UpdateWithdrawResult(ctx, id, txHash, status, "")
}

// MarkWithdrawFailed 标记失败
func (s *WithdrawService) MarkWithdrawFailed(ctx context.Context, id int64, errMsg string) error {
	// TODO: 这里未来可能需要关联“解冻资产”的操作，所以放在 Service 层非常合适
	logger.Error(ctx, "提现失败标记", zap.Int64("id", id), zap.String("err", errMsg))
	return s.repo.UpdateWithdrawResult(ctx, id, "", domain.WithdrawStatusFailed, errMsg)
}
