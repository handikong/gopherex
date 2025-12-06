package ethereum

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
	"gopherex.com/internal/watcher/domain"
	"gopherex.com/pkg/logger"
)

var TransferTopic = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))

type Adapter struct {
	client *ethclient.Client
	// 关注的合约列表 (Key: ContractAddress, Value: Symbol)
	// 生产环境应从数据库加载
	watchedContracts map[string]string
	chainID          *big.Int
}

// 确保实现接口
var _ domain.ChainAdapter = (*Adapter)(nil)

func New(nodeUrl string) (*Adapter, error) {
	client, err := ethclient.Dial(nodeUrl)
	if err != nil {
		return nil, err
	}
	// 获取 ChainID (防止重放攻击)
	chainID, err := client.ChainID(context.Background())
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
		chainID:          chainID,
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
		Transactions: make([]domain.ChainTransfer, 0),
	}
	// 创建签名器用于提取发送方地址
	signer := types.LatestSignerForChainID(a.chainID)

	for _, tx := range block.Transactions() {
		// 获取发送方地址
		fromAddress, err := types.Sender(signer, tx)
		if err != nil {
			// 如果无法获取发送方地址（例如交易格式异常），跳过该交易
			logger.Warn(ctx, "failed to get sender address from transaction",
				zap.String("txHash", tx.Hash().Hex()),
				zap.Error(err))
			continue
		}

		// 处理 ETH 转账
		if tx.Value().Cmp(big.NewInt(0)) > 0 && tx.To() != nil {
			// 精度处理: Wei(18位) -> Decimal
			amount := weiToDecimal(tx.Value(), 18)

			stdBlock.Transactions = append(stdBlock.Transactions, domain.ChainTransfer{
				TxHash:      tx.Hash().Hex(),
				LogIndex:    0, // 原生交易默认为 0
				Chain:       "ETH",
				Symbol:      "ETH",
				ToAddress:   strings.ToLower(tx.To().Hex()),
				FromAddress: strings.ToLower(fromAddress.Hex()),
				Amount:      amount,
				BlockHeight: height,
				Status:      domain.TransactionStatusPending,
			})
		}

	}
	return stdBlock, nil
}

func (a *Adapter) FetchLog(ctx context.Context, from, to int64, addresses []string) ([]types.Log, error) {
	query := ethereum.FilterQuery{
		FromBlock: big.NewInt(from),
		ToBlock:   big.NewInt(to),
		// Topics[0] 固定为 Transfer 方法签名
		// Topics[1] 是 from (我们不关心)
		// Topics[2] 是 to (充值目标地址) -> 这里可以用 nil 抓全量，也可以传入你的热钱包地址列表进行过滤
		Topics: [][]common.Hash{
			{TransferTopic},
		},
	}
	if len(addresses) > 0 {
		addrList := make([]common.Address, len(addresses))
		for i, a := range addresses {
			addrList[i] = common.HexToAddress(a)
		}
		query.Addresses = addrList
	}

	return a.client.FilterLogs(ctx, query)
}

func (a *Adapter) GetTransactionStatus(ctx context.Context, hash string) (domain.TransactionType, error) {
	txHash := common.HexToHash(hash)

	// 获取收据
	receipt, err := a.client.TransactionReceipt(ctx, txHash)
	if err != nil {
		// 如果是 ethereum.NotFound，说明可能还在 Pending 或者丢了
		return 0, nil
	}

	// Status: 1 = Success, 0 = Failed
	if receipt.Status == 1 {
		// 还要检查确认数
		latest, _ := a.client.BlockNumber(ctx)
		if int64(latest)-receipt.BlockNumber.Int64() >= 12 { // 12个确认才算稳
			return domain.TransactionConfirmed, nil
		}
		return domain.TransactionStatusPending, nil
	}

	return domain.TransactionFailed, nil
}

// 辅助工具
func weiToDecimal(wei *big.Int, decimals int32) decimal.Decimal {
	d := decimal.NewFromBigInt(wei, 0)
	return d.Shift(-decimals)
}
