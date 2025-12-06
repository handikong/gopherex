package domain

type TransactionType uint8

// 充值状态枚举
const (
	TransactionStatusPending TransactionType = iota //待确认
	TransactionConfirmed                            // 已确认
	TransactionFailed                               // 失败
)

const (
	StreamRecharegeKey = "stream:recharge"
	GroupName          = "stream:group_workers"
)

const (
	ModeBlock = "block"
	ModeLog   = "log"
)
