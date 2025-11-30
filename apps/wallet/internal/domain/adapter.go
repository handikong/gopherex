package domain

import (
	"context"
)

// 定义区块 屏蔽底层差距
type StandarBlock struct {
	Hight        int64     // 区块高度
	Hash         string    // 区块hash
	PerHash      string    // 前一块hash
	Time         int64     // 区块时间
	Transactions []Deposit // 区块交易
}

// 适配器的接口
type ChainAdapter interface {
	// 获取区块的长度
	GetBlockHeight(ctx context.Context) (int64, error)
	// 获取区块的数据
	FetchBlock(ctx context.Context, height int64) (*StandarBlock, error)
}
