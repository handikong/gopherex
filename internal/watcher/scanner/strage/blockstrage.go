package strage

import (
	"context"

	"github.com/redis/go-redis/v9"
	"gopherex.com/internal/watcher/domain"
	"gopherex.com/internal/watcher/service"
)

type BlockStrage struct {
	rds     *redis.Client
	adapter domain.ChainAdapter
	srv     *service.ScanService
}

var _ domain.ScanStrageWatcher = (*BlockStrage)(nil)

func newBlockStrage(rds *redis.Client, src *service.ScanService, adapter domain.ChainAdapter) *BlockStrage {
	return &BlockStrage{
		rds:     rds,
		srv:     src,
		adapter: adapter,
	}
}

// 获取步长
func (s *BlockStrage) GetSkip() int64 {
	return 1
}

// 获取数据并且推送到redis
func (s *BlockStrage) GetFetchAndPush(ctx context.Context, from, to int64) (height int64, re []*domain.ChainTransfer, err error) {
	// 调用 log查询
	block, err := s.adapter.FetchBlock(ctx, from)
	if err != nil {
		return from, nil, err
	}
	if len(block.Transactions) == 0 {
		return to, nil, nil
	}
	// 循环组合数据 放入redis
	// 构造redis Pipeline 并保存转账数据（假设SetChainTransfer是一个存储方法）

	var res = []*domain.ChainTransfer{}
	for key, block := range block.Transactions {
		// 判断地址是否存在
		if !s.srv.IsAddress(block.ToAddress) {
			continue
		}
		// 组合数据
		chainTransfer := &domain.ChainTransfer{
			TxHash:      block.TxHash,
			FromAddress: block.FromAddress,
			ToAddress:   block.ToAddress,
			BlockHeight: block.BlockHeight,
			Amount:      block.Amount,
			LogIndex:    key,
			Chain:       block.Chain, // 标记类型
			Symbol:      "native",
		}
		res = append(res, chainTransfer)

	}

	return to, res, err
}
