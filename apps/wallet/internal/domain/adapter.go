package domain

import (
	"context"
)

// 定义区块 屏蔽底层差距
type StandardBlock struct {
	Height       int64     // 区块高度
	Hash         string    // 区块hash
	PrevHash     string    // 前一块hash
	Time         int64     // 区块时间
	Transactions []Deposit // 区块交易
}

// 充值适配器的接口
type ChainAdapter interface {
	// 获取区块的长度
	GetBlockHeight(ctx context.Context) (int64, error)
	// 获取区块的数据
	FetchBlock(ctx context.Context, height int64) (*StandardBlock, error)
}

// 充值处理接口
type Handler interface {
	// 处理数据
	HandlerBlock(ctx context.Context, block *StandardBlock) error
}

// 区块数据源
type Repository interface {
	// 获取最后一个游标
	GetLastCursor(ctx context.Context, chain string) (height int64, hash string, err error)
	// UpdateCursor 更新游标 (通常和业务处理在一个事务里，这里单独定义是为了灵活性)
	UpdateCursor(ctx context.Context, chain string, height int64, hash string) error
	// Rollback 回滚：删除 >= height 的所有数据，并将游标重置
	Rollback(ctx context.Context, chain string, height int64) error

	// 🔥 新增：将符合确认数的 Pending 记录更新为 Confirmed
	ConfirmDeposits(ctx context.Context, chain string, currentHeight int64, confirmNum int64) (int64, error)
	// UpdateDepositStatusToConfirmed 将充值记录状态改为 Confirmed
	UpdateDepositStatusToConfirmed(ctx context.Context, id int64) error
}
