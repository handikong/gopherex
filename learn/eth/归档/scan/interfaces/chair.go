package interfaces

import (
	"context"
)

type Chair interface {
	// 获取区块的高度
	GetHeight(ctx context.Context) (uint64, error)
	// 根据区块高度获取数据
	GetBlockByHeight(ctx context.Context, height uint64) (*StandardBlock, error)
}
