package domain

import (
	"context"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/shopspring/decimal"
)

// ChainTransfer 通用的链上转账模型
type ChainTransfer struct {
	TxHash      string          // 交易hash
	LogIndex    int             // ETH特有
	BlockHeight int64           // 块的高度
	FromAddress string          // 地址来源
	ToAddress   string          // 转账给谁
	Chain       string          // 币的来源
	Symbol      string          // 币的种类
	Amount      decimal.Decimal // 金额
	Contract    string          // ETH的合约地址
	Data        string          // ETH的合约
	Status      TransactionType // 1: 成功, 0: 失败
	GasUsed     decimal.Decimal // 提现时我们需要关注这个，充值时不关心
	MsgId       string          // redis的数据量
}

// 定义区块 屏蔽底层差距
type StandardBlock struct {
	Height       int64           // 区块高度
	Hash         string          // 区块hash
	PrevHash     string          // 前一块hash
	Time         int64           // 区块时间
	Transactions []ChainTransfer //
}

// 充值适配器的接口
type ChainAdapter interface {
	// 获取区块的长度
	GetBlockHeight(ctx context.Context) (int64, error)
	// 获取区块的数据 用于btc和ETH原生
	FetchBlock(ctx context.Context, height int64) (*StandardBlock, error)
	// 获取区块的日志 只用于log
	FetchLog(ctx context.Context, from, to int64, address []string) ([]types.Log, error)

	// 🔥 新增：查询交易状态
	// 输出：通用状态 (Confirmed/Failed/Pending)
	GetTransactionStatus(ctx context.Context, hash string) (TransactionType, error)

	// 🔥 新增：提现发币接口
	// BTC: 只看 Amount 和 ToAddress
	// ETH: 会看 Symbol (ETH 还是 USDT)
	// SendWithdrawal(ctx context.Context, order *Withdraw) (txHash string, err error)
}
