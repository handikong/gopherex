package strage

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
	"github.com/redis/go-redis/v9"
	"gopherex.com/internal/watcher/domain"
	"gopherex.com/internal/watcher/service"
	"gopherex.com/pkg/logger"
)

type LogStrage struct {
	rds     *redis.Client
	adapter domain.ChainAdapter
	srv     *service.ScanService
}

var _ domain.ScanStrageWatcher = (*LogStrage)(nil)

func newLogStrage(rds *redis.Client, src *service.ScanService, adapter domain.ChainAdapter) *LogStrage {
	return &LogStrage{
		rds:     rds,
		srv:     src,
		adapter: adapter,
	}
}

// 获取步长
func (s *LogStrage) GetSkip() int64 {
	return 1000
}

// 获取数据并且推送到redis
func (s *LogStrage) GetFetchAndPush(ctx context.Context, from, to int64) (height int64, res []*domain.ChainTransfer, err error) {
	// 调用 log查询
	logger.Info(ctx, "GetFetchAndPush开始")
	txlogs, err := s.adapter.FetchLog(ctx, from, to, []string{})
	if err != nil {
		return from, nil, err
	}
	if len(txlogs) == 0 {
		return to, nil, nil
	}
	// 循环组合数据 放入redis
	// 构造redis Pipeline 并保存转账数据（假设SetChainTransfer是一个存储方法）
	// pipe := s.rds.Pipeline()
	var ChainTransfers = []*domain.ChainTransfer{}
	for _, lg := range txlogs {
		// 如果参数小于3就是假的
		if len(lg.Topics) < 3 {
			continue
		}
		//解析数据
		toAddress := common.HexToAddress(lg.Topics[2].Hex()).String()
		// 判断地址是否存在
		if !s.srv.IsAddress(toAddress) {
			continue
		}
		// 组合数据
		chainTransfer := domain.ChainTransfer{
			TxHash:      lg.TxHash.Hex(),
			FromAddress: common.HexToAddress(lg.Topics[1].Hex()).String(),
			ToAddress:   toAddress,
			BlockHeight: int64(lg.BlockNumber),
			Data:        common.Bytes2Hex(lg.Data),
			Contract:    lg.Address.String(),
			LogIndex:    int(lg.Index),
			Chain:       "ETH", // 标记类型
			Symbol:      "erc20",
		}
		ChainTransfers = append(ChainTransfers, &chainTransfer)

	}
	return to, ChainTransfers, err
}
