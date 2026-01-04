package interfaces

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/shopspring/decimal"
)

type ETHChair struct {
	client *ethclient.Client
	ctx    context.Context
}

func NewETHChair(ctx context.Context, client *ethclient.Client) *ETHChair {
	return &ETHChair{
		ctx:    ctx,
		client: client,
	}
}

// 获取当前的区块高度
func (c *ETHChair) GetHeight(ctx context.Context) (uint64, error) {
	return c.client.BlockNumber(ctx)
}

func (c *ETHChair) GetBlockByHeight(ctx context.Context, height uint64) (*StandardBlock, error) {
	cHeight := big.NewInt(int64(height))
	ethBlock, err := c.client.BlockByNumber(ctx, cHeight)
	if err != nil {
		return nil, err
	}
	standardBlock := &StandardBlock{
		Height:       ethBlock.Number().Int64(),
		Hash:         ethBlock.Hash().String(),
		PrevHash:     ethBlock.ParentHash().String(),
		Time:         int64(ethBlock.Time()),
		Transactions: make([]ChainTransfer, 0, len(ethBlock.Transactions())),
	}
	chainID, _ := c.client.ChainID(ctx)
	signer := types.LatestSignerForChainID(chainID)
	for _, tx := range ethBlock.Transactions() {
		// 精度处理: Wei(18位) -> Decimal
		amount := weiToDecimal(tx.Value(), 18)
		// 获取发送方地址
		fromAddress, _ := types.Sender(signer, tx)
		standardBlock.Transactions = append(standardBlock.Transactions, ChainTransfer{
			TxHash:      tx.Hash().String(),
			LogIndex:    0,
			BlockHeight: ethBlock.Number().Int64(),
			FromAddress: fromAddress.String(),
			ToAddress:   tx.To().String(),
			Chain:       "ETH",
			Symbol:      "ETH",
			Amount:      amount,
			Status:      TransactionStatusPending,
		})
	}
	return standardBlock, nil
}

// 辅助工具
func weiToDecimal(wei *big.Int, decimals int32) decimal.Decimal {
	d := decimal.NewFromBigInt(wei, 0)
	return d.Shift(-decimals)
}
