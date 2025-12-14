package domain

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

type RechargeType uint8

// 充值状态枚举
const (
	RechargeStatusPending   RechargeType = iota //待确认
	RechargeStatusConfirmed                     // 已确认
	RechargeStatusFailed                        // 失败
)

type HashStr []byte
type Recharge struct {
	ID int64 // 主键
	// 核心唯一标识: Chain + TxHash + LogIndex
	TxHash      string          // hash地址
	FromAddress string          // 发送方
	ToAddress   string          // 接收方
	ToUid       int             // 接收方id
	Chain       string          // 链的来源
	Symbol      string          // 币类型
	Amount      decimal.Decimal // 充值金额
	Status      RechargeType    // 充值状态
	ErrorMsg    string          // 充值失败原因
	BlockHeight int64           // 区块的高度
	LogIndex    int             // eth转账的记录
	CreatedAt   time.Time       // 充值时间
}

// TableName 设置 Recharge 结构体对应的数据库表名
func (Recharge) TableName() string {
	return "deposits"
}

type RechargeRepo interface {
	// 🔥 新增：将符合确认数的 Pending 记录更新为 Confirmed
	ConfirmDeposits(ctx context.Context, chain string, currentHeight int64, confirmNum int64) (int64, error)
	// UpdateDepositStatusToConfirmed 将充值记录状态改为 Confirmed
	UpdateDepositStatusToConfirmed(ctx context.Context, id int64) error
	// 根据chain和高度获取充值
	GetPendingDeposits(ctx context.Context, chain string, height int64) ([]*Recharge, error)
	// 根据用户Id获取充值记录
	GetRechargeListById(ctx context.Context, chain string, Symbol string, status RechargeType, page int, limit int) ([]*Recharge, error)
	// CreateDeposit 创建充值记录
	CreateDeposit(ctx context.Context, deposit *Recharge) error
	// GetDeposit 根据ID获取充值记录
	GetDeposit(ctx context.Context, id int64) (*Recharge, error)
}
