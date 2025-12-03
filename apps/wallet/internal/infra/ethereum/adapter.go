package ethereum

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
	"gopherex.com/apps/wallet/internal/domain"
	"gopherex.com/pkg/logger"
)

// ERC-20 Transfer 事件哈希: Keccak256("Transfer(address,address,uint256)")
const TransferEventHash = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

type Adapter struct {
	client *ethclient.Client
	// 关注的合约列表 (Key: ContractAddress, Value: Symbol)
	// 生产环境应从数据库加载
	watchedContracts map[string]string
}

// 确保实现接口
var _ domain.ChainAdapter = (*Adapter)(nil)

func New(nodeUrl string) (*Adapter, error) {
	client, err := ethclient.Dial(nodeUrl)
	if err != nil {
		return nil, err
	}

	// 初始化关注的合约 (这里先硬编码测试)
	// 请把这里的地址换成你 Day 10 部署的 MockToken 地址
	contracts := map[string]string{
		strings.ToLower("0x5FC8d32690cc91D4c39d9d3abcBD16989F875707"): "USDT",
	}

	return &Adapter{
		client:           client,
		watchedContracts: contracts,
	}, nil
}

func (a *Adapter) GetBlockHeight(ctx context.Context) (int64, error) {
	height, err := a.client.BlockNumber(ctx)
	if err != nil {
		return 0, err
	}
	return int64(height), nil
}

func (a *Adapter) FetchBlock(ctx context.Context, height int64) (*domain.StandardBlock, error) {
	blockNum := big.NewInt(height)

	// 1. 获取区块详情
	block, err := a.client.BlockByNumber(ctx, blockNum)
	if err != nil {
		return nil, fmt.Errorf("eth get block failed: %w", err)
	}

	stdBlock := &domain.StandardBlock{
		Height:       height,
		Hash:         block.Hash().Hex(),
		PrevHash:     block.ParentHash().Hex(),
		Time:         int64(block.Time()),
		Transactions: make([]domain.Deposit, 0),
	}
	for _, tx := range block.Transactions() {
		// 处理 ETH 转账
		if tx.Value().Cmp(big.NewInt(0)) > 0 && tx.To() != nil {
			// 精度处理: Wei(18位) -> Decimal
			amount := weiToDecimal(tx.Value(), 18)

			stdBlock.Transactions = append(stdBlock.Transactions, domain.Deposit{
				TxHash:      tx.Hash().Hex(),
				LogIndex:    0, // 原生交易默认为 0
				Chain:       "ETH",
				Symbol:      "ETH",
				ToAddress:   strings.ToLower(tx.To().Hex()),
				Amount:      amount,
				BlockHeight: height,
				Status:      domain.DepositStatusPending,
			})
		}
		// 3. 处理合约交易 (Logs)
		// 性能优化：生产环境建议使用 FilterLogs 批量拉取整个块的日志，而不是逐笔查 Receipt
		// 这里为了逻辑清晰，先演示逐笔查 Receipt
		receipt, err := a.client.TransactionReceipt(ctx, tx.Hash())
		if err != nil {
			continue
		}
		if receipt.Status != types.ReceiptStatusSuccessful {
			continue
		}
		for _, log := range receipt.Logs {
			// 过滤 1: 是否是 Transfer 事件?
			if len(log.Topics) == 3 && log.Topics[0].Hex() == TransferEventHash {

				// 过滤 2: 是否是我们关注的币种?
				contractAddr := strings.ToLower(log.Address.Hex())
				symbol, exists := a.watchedContracts[contractAddr]
				if !exists {
					continue
				}

				// 解析: Topic[2] 是接收方
				toAddress := common.HexToAddress(log.Topics[2].Hex()).Hex()

				// 解析: Data 是金额
				amountBig := new(big.Int).SetBytes(log.Data)
				// 假设 USDT 是 18 位 (Mock合约)，真实 USDT 是 6 位
				amount := weiToDecimal(amountBig, 18)

				stdBlock.Transactions = append(stdBlock.Transactions, domain.Deposit{
					TxHash:      log.TxHash.Hex(),
					LogIndex:    int(log.Index), // 使用 Log 的全局索引
					Chain:       "ETH",
					Symbol:      symbol, // "USDT"
					ToAddress:   strings.ToLower(toAddress),
					Amount:      amount,
					BlockHeight: height,
					Status:      domain.DepositStatusPending,
				})

				logger.Info(ctx, "🔍 发现合约充值",
					zap.String("symbol", symbol),
					zap.String("to", toAddress),
					zap.String("amount", amount.String()))
			}
		}
	}
	return stdBlock, nil
}

func (a *Adapter) SendWithdrawal(ctx context.Context, order *domain.Withdraw) (string, error) {
	return "", nil
}

func (a *Adapter) GetTransactionStatus(ctx context.Context, hash string) (domain.WithdrawStatus, error)

// 辅助工具
func weiToDecimal(wei *big.Int, decimals int32) decimal.Decimal {
	d := decimal.NewFromBigInt(wei, 0)
	return d.Shift(-decimals)
}
