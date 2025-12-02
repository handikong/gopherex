package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

type DepositType uint8

// 充值状态枚举
const (
	DepositStatusPending   DepositType = iota //待确认
	DepositStatusConfirmed                    // 已确认
	DepositStatusFailed                       // 失败
)

type HashStr []byte
type Deposit struct {
	ID int64 // 主键
	// 核心唯一标识: Chain + TxHash + LogIndex
	TxHash      string          // hash地址
	FromAddress string          // 发送方
	ToAddress   string          // 接收方
	Chain       string          // 链的来源
	Symbol      string          // 币类型
	Amount      decimal.Decimal // 充值金额
	Status      DepositType     // 充值状态
	ErrorMsg    string          // 充值失败原因
	BlockHeight int64           // 区块的高度
	LogIndex    int             // eth转账的记录
	CreatedAt   time.Time       // 充值时间
}
