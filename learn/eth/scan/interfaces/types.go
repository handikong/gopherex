package interfaces

import "github.com/shopspring/decimal"

type TransactionType int

// 充值状态枚举
const (
	TransactionStatusPending TransactionType = iota //待确认
	TransactionConfirmed                            // 已确认
	TransactionFailed                               // 失败
)

type StandardBlock struct {
	Height       int64           `json:"height"`    // 区块高度
	Hash         string          `json:"hash"`      // 区块hash
	PrevHash     string          `json:"prev_hash"` // 前一块hash
	Time         int64           `json:"time"`      // 区块时间
	Transactions []ChainTransfer //
}

// ChainTransfer 通用的链上转账模型
type ChainTransfer struct {
	TxHash      string          `json:"tx_hash"`      // 交易hash
	LogIndex    int             `json:"log_index"`    // ETH特有
	BlockHeight int64           `json:"block_height"` // 块的高度
	FromAddress string          `json:"from_address"` // 地址来源
	ToAddress   string          `json:"to_address"`   // 转账给谁
	Chain       string          `json:"chain"`        // 币的来源
	Symbol      string          `json:"symbol"`       // 币的种类
	Amount      decimal.Decimal `json:"amount"`       // 金额
	Contract    string          `json:"contract"`     // ETH的合约地址
	Data        string          `json:"data"`         // ETH的合约
	Status      TransactionType `json:"status"`       // 1: 成功, 0: 失败
	GasUsed     decimal.Decimal `json:"gas_used"`     // 提现时我们需要关注这个，充值时不关心
	MsgId       string          `json:"msg_id"`       // redis的数据量
}
