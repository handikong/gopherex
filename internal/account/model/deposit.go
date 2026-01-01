package model

import "time"

type DepositStatus int8

const (
	DepositPending   DepositStatus = 0
	DepositConfirmed DepositStatus = 1
)

type Deposit struct {
	ID          int64         `gorm:"column:id;primaryKey"`
	Chain       string        `gorm:"column:chain"`
	Symbol      string        `gorm:"column:symbol"`
	TxHash      string        `gorm:"column:tx_hash"`
	LogIndex    int           `gorm:"column:log_index"`
	ToAddress   string        `gorm:"column:to_address"`
	ToUID       int64         `gorm:"column:to_uid"`
	Amount      string        `gorm:"column:amount"` // decimal string
	BlockHeight int64         `gorm:"column:block_height"`
	Status      DepositStatus `gorm:"column:status"`

	// 入账标记（建议你表里已经有；如果没有，后面我给你“非侵入式替代方案”）
	CreditTxnID string     `gorm:"column:credit_txn_id"`
	CreditedAt  *time.Time `gorm:"column:credited_at"`

	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (Deposit) TableName() string {
	return "account_deposits"
}
