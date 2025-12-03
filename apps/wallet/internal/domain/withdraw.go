package domain

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

type WithdrawStatus uint8

const (
	WithdrawStatusApplying   WithdrawStatus = iota // 0: 申请中
	WithdrawStatusAudited                          // 1: 审核通过 (待广播)
	WithdrawStatusProcessing                       // 2: 广播中 (已上链，等待确认)
	WithdrawStatusConfirmed                        // 3: 已确认 (成功)
	WithdrawStatusFailed                           // 4: 失败 (链上失败)
	WithdrawStatusRejected                         // 5: 驳回 (风控/人工)
)

// Withdraw 提现实体
type Withdraw struct {
	ID        int64
	UserID    int64
	Chain     string
	Symbol    string
	Amount    decimal.Decimal // 提现金额
	Fee       decimal.Decimal // 手续费
	ToAddress string
	TxHash    string
	Status    WithdrawStatus
	ErrorMsg  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// WithdrawRepo 提现仓储接口
type WithdrawRepo interface {
	// FreezeBalance 原子性冻结余额（核心）
	// 使用乐观锁 (version) 确保数据一致性
	FreezeBalance(ctx context.Context, asset *UserAsset, freezeAmount decimal.Decimal) error

	// CreateWithdrawOrder 创建提现订单
	CreateWithdrawOrder(ctx context.Context, order *Withdraw) error

	// 核心：抢单 (原子操作)
	// 查找、锁定并更新状态为 Processing
	FindPendingWithdrawsForUpdate(ctx context.Context, chain string, status WithdrawStatus, limit int) ([]Withdraw, error)

	// 多条更新
	UpdateWithdrawStatusBatch(ctx context.Context, ids []int64, status WithdrawStatus) error

	// 标记状态
	UpdateWithdrawResult(ctx context.Context, id int64, txHash string, status WithdrawStatus, errMsg string) error
}
