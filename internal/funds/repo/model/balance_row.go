package model

import "time"

type BalanceRow struct {
	OwnerType uint64    `gorm:"column:owner_type;primaryKey;not null"`
	OwnerID   uint64    `gorm:"column:owner_id;primaryKey;not null"`
	Asset     string    `gorm:"column:asset;primaryKey;type:varchar(16);not null"`
	Bucket    string    `gorm:"column:bucket;primaryKey;type:varchar(32);not null"`
	Amount    int64     `gorm:"column:amount;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (BalanceRow) TableName() string {
	return "balances"
}
