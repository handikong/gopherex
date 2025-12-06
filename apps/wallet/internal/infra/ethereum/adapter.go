package ethereum

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
	"gopherex.com/apps/wallet/internal/domain"
	"gopherex.com/pkg/logger"
)

// ERC-20 Transfer 事件哈希: Keccak256("Transfer(address,address,uint256)")
const TransferEventHash = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
const erc20ABI = `[{"constant":false,"inputs":[{"name":"_to","type":"address"},{"name":"_value","type":"uint256"}],"name":"transfer","outputs":[{"name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"}]`

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
	// 根据私钥进行签名
	privateKeyHex := "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	// 用私钥推导出公钥
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return "", err
	}
	// 公钥地址
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return "", fmt.Errorf("error casting public key to ECDSA")
	}
	// 1. 准备基础参数
	var (
		toAddress common.Address
		amountWei *big.Int
		txData    []byte // 交易附带的数据
	)
	// 处理多个地址请求
	if order.Symbol == "ETH" {
		// === 原生币转账 ===
		toAddress = common.HexToAddress(order.ToAddress)
		// ETH 转账: Amount 就是转账金额
		amountWei = order.Amount.Mul(decimal.NewFromInt(1e18)).BigInt()
		txData = nil // 原生转账没有 Data
	} else {
		// === ERC20 代币转账 (例如 USDT) ===
		// 这里的 To 是合约地址！(需要你维护一个 map 或从 order 里传进来)
		// 假设我们在 NewAdapter 里已经把 USDT 合约地址存进去了，或者通过 order.ContractAddress 传进来
		// 这里为了演示，假设 order.Symbol 直接对应合约地址 (生产环境需查表)
		contractAddrStr := "0x你的USDT测试合约地址" // 记得替换！
		toAddress = common.HexToAddress(contractAddrStr)

		// ERC20 转账: 交易金额 Value 必须为 0 (因为我们只付 Gas)
		amountWei = big.NewInt(0)

		// 真正的转账金额和接收方放在 Data 里
		realTo := common.HexToAddress(order.ToAddress)
		realAmount := order.Amount.Mul(decimal.NewFromInt(1e18)).BigInt() // 假设也是18位精度

		// ABI 打包
		txData, err = a.packTransferData(realTo, realAmount)
		if err != nil {
			return "", fmt.Errorf("pack data failed: %v", err)
		}
	}

	// 推导出地址
	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)
	// 获取这个地址的nonc
	// 这个地址存在并发  注意
	nonce, err := a.client.PendingNonceAt(ctx, fromAddress)
	if err != nil {
		return "", fmt.Errorf("failed to get nonce: %v", err)
	}

	// 3. 估算 Gas (EIP-1559)
	// ===========================
	// A. 获取建议的小费 (Tip)
	gasTipCap, err := a.client.SuggestGasTipCap(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get gas tip: %v", err)
	}
	// B. 获取当前区块头，为了拿到 BaseFee
	head, err := a.client.HeaderByNumber(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("failed to get header: %v", err)
	}
	// BaseFee 只有在支持 London 硬分叉的链上才有，Ganache 通常支持
	baseFee := head.BaseFee
	if baseFee == nil {
		// 兼容旧链
		baseFee = big.NewInt(0)
	}
	// C. 计算 MaxFeePerGas = (2 * BaseFee) + Tip
	// 这是一个常用的策略，防止下一个块 BaseFee 暴涨导致交易被丢弃
	gasFeeCap := new(big.Int).Add(
		new(big.Int).Mul(baseFee, big.NewInt(2)),
		gasTipCap,
	)

	//F. 估算 Gas Limit (ETH 转账通常是 21000，但估算一下更安全)
	// 构建一个 call msg 模拟执行
	// 21000 units
	gasLimit := uint64(21000)
	if len(txData) > 0 {
		// 如果有 Data，说明是合约调用，调高 Gas Limit
		// 生产环境建议用 client.EstimateGas 估算，这里先给个安全值
		gasLimit = uint64(100000)
	}
	// ===========================
	// 4. 构建交易结构体 (DynamicFeeTx)
	// ===========================
	// ... 构造 DynamicFeeTx ...
	txPayload := &types.DynamicFeeTx{
		ChainID:   a.chainID,
		Nonce:     nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       gasLimit,
		To:        &toAddress,
		Value:     amountWei,
		Data:      txData, // 🔥 注入我们打包好的 Data
	}
	tx := types.NewTx(txPayload)

	// ===========================
	// 5. 签名 & 广播
	// ===========================
	signedTx, err := types.SignTx(tx, types.LatestSignerForChainID(a.chainID), privateKey)
	if err != nil {
		return "", fmt.Errorf("sign failed: %v", err)
	}

	err = a.client.SendTransaction(ctx, signedTx)
	if err != nil {
		return "", fmt.Errorf("broadcast failed: %v", err)
	}

	logger.Info(ctx, "ETH 提现已广播",
		zap.Uint64("nonce", nonce),
		zap.String("hash", signedTx.Hash().Hex()))

	return signedTx.Hash().Hex(), nil
}

func (a *Adapter) GetTransactionStatus(ctx context.Context, hash string) (domain.WithdrawStatus, error) {
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
			return domain.WithdrawStatusConfirmed, nil
		}
		return domain.WithdrawStatusProcessing, nil
	}

	return domain.WithdrawStatusFailed, nil
}

// 辅助工具
func weiToDecimal(wei *big.Int, decimals int32) decimal.Decimal {
	d := decimal.NewFromBigInt(wei, 0)
	return d.Shift(-decimals)
}

// 辅助方法：打包 ERC20 transfer 数据
func (a *Adapter) packTransferData(to common.Address, amount *big.Int) ([]byte, error) {
	parsedABI, err := abi.JSON(strings.NewReader(erc20ABI))
	if err != nil {
		return nil, err
	}
	return parsedABI.Pack("transfer", to, amount)
}
