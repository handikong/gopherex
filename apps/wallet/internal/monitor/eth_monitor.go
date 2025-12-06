package monitor

import (
	"context"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"
	"gopherex.com/pkg/logger"
)

// USDT 合约地址 (主网)
const USDTAddress = "0xdAC17F958D2ee523a2206206994597C13D831ec7"

// Transfer 事件 Hash
const TransferTopic = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

type EthMonitor struct {
	wssUrl string
}

func NewEthMonitor(wssUrl string) *EthMonitor {
	return &EthMonitor{wssUrl: wssUrl}
}

func (m *EthMonitor) Start(ctx context.Context) {
	// 1. 连接 WebSocket 节点
	client, err := ethclient.Dial(m.wssUrl)
	if err != nil {
		logger.Fatal(ctx, "WSS 连接失败", zap.Error(err))
	}
	logger.Info(ctx, "🎧 已连接以太坊主网 WSS，开始监听 USDT 流...")

	// 2. 构造订阅查询
	contractAddress := common.HexToAddress(USDTAddress)
	query := ethereum.FilterQuery{
		Addresses: []common.Address{contractAddress},
		Topics: [][]common.Hash{
			{common.HexToHash(TransferTopic)},
		},
	}

	// 3. 创建通道接收日志
	logs := make(chan types.Log)
	sub, err := client.SubscribeFilterLogs(ctx, query, logs)
	if err != nil {
		logger.Fatal(ctx, "订阅失败", zap.Error(err))
	}

	// 4. 循环读取
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-sub.Err():
			logger.Error(ctx, "订阅异常断开", zap.Error(err))
			// 真实场景这里需要重连逻辑
			return
		case vLog := <-logs:
			// 简历高光时刻：这里就是高吞吐的入口
			// Day 26 我们会把这里改成：kafkaProducer.Send(vLog)

			// 简单的解析用于展示
			if len(vLog.Topics) < 3 {
				continue
			}
			// Topic[1] 是 From (因为是 indexed)
			from := common.HexToAddress(vLog.Topics[1].Hex())
			// Topic[2] 是 To
			to := common.HexToAddress(vLog.Topics[2].Hex())

			// 只是打印，证明我们连上了真实世界
			// 注意：控制台可能会刷屏非常快！
			logger.Info(ctx, "🔥 [RealTime USDT]",
				zap.String("tx", vLog.TxHash.Hex()),
				zap.String("from", from.Hex()),
				zap.String("to", to.Hex()),
			)
		}
	}
}
