package scanner_test

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
	bitcoin "gopherex.com/internal/watcher/chain/btc"
	ethereum "gopherex.com/internal/watcher/chain/eth"
	"gopherex.com/internal/watcher/domain"
	"gopherex.com/internal/watcher/scanner"
	"gopherex.com/pkg/logger"
	"gopherex.com/pkg/orm"
	"gopherex.com/pkg/xredis"
)

func TestScaner(t *testing.T) {
	// 构建三个扫描类型
	// BTC的block扫描
	// ETH的block扫描
	logger.Init("test", "info")

	db := orm.NewMySQL(&orm.Config{
		DSN:         "root:123456@tcp(127.0.0.1:3307)/gopherex_wallet?charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai",
		MaxIdle:     10,
		MaxOpen:     100,
		MaxLifetime: 3600,
	})

	rdb := xredis.NewRedis(&xredis.Config{
		Addr:     "127.0.0.1:6379",
		Password: "",
		DB:       0,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger.Info(ctx, "✅ Infrastructure initialized")

	btcAdapter, err := bitcoin.New(
		"127.0.0.1:18443",
		"admin",
		"123456",
		&chaincfg.RegressionNetParams, // 暂时硬编码为回归测试网
	)
	if err != nil {
		log.Fatalf("BTC Adapter init failed: %v", err)
	}

	cfg := &domain.RechargeConfig{
		Chain:           "BTC",
		Interval:        3 * time.Second,
		ConfirmInterval: 10 * time.Second,
		ConfirmNum:      1, // Regtest 1个确认就够
		ConsumerCount:   5,
		ScanMode:        domain.ModeBlock,
	}

	btcEngine := scanner.NewRecharge(cfg, rdb, btcAdapter, db)

	// ETH 相关的
	ethAdapter, err := ethereum.New("http://127.0.0.1:8545")
	if err != nil {
		log.Fatal(err)
	}
	cfg = &domain.RechargeConfig{
		Chain:           "ETH",
		Interval:        3 * time.Second,
		ConfirmInterval: 10 * time.Second,
		ConfirmNum:      1, // Regtest 1个确认就够
		ConsumerCount:   5,
		ScanMode:        domain.ModeBlock,
	}
	//2. 初始化 ETH 引擎
	ethEngine := scanner.NewRecharge(cfg,
		rdb,
		ethAdapter,
		db)

	cfg = &domain.RechargeConfig{
		Chain:           "ETH",
		Interval:        3 * time.Second,
		ConfirmInterval: 10 * time.Second,
		ConfirmNum:      1, // Regtest 1个确认就够
		ConsumerCount:   5,
		ScanMode:        domain.ModeLog,
	}
	//2. 初始化 ETH 引擎
	ethEngine1 := scanner.NewRecharge(cfg, rdb, ethAdapter, db)
	go btcEngine.Start(ctx)
	go ethEngine.Start(ctx)
	go ethEngine1.Start(ctx)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info(ctx, "Shutdown signal received...")
	cancel()
}

// TestETHTransactionGenerator 不断往ETH里面写ETH原生转账和合约转账
func TestETHTransactionGenerator(t *testing.T) {
	logger.Init("eth-tx-generator", "info")

	// 直接连接ETH节点
	client, err := ethclient.Dial("http://127.0.0.1:8545")
	if err != nil {
		t.Fatalf("Failed to connect ETH node: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 获取chainID
	chainID, err := client.ChainID(ctx)
	if err != nil {
		t.Fatalf("Failed to get chain ID: %v", err)
	}

	// 使用测试私钥（Ganache/Hardhat默认账户0的私钥）
	privateKeyHex := "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		t.Fatalf("Failed to parse private key: %v", err)
	}

	// 获取发送地址
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		t.Fatal("Failed to cast public key to ECDSA")
	}
	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)

	// 测试接收地址（可以修改为任意地址）
	toAddress := common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8") // Ganache账户1

	// ERC20合约地址（从adapter中获取）
	contractAddress := common.HexToAddress("0x5FC8d32690cc91D4c39d9d3abcBD16989F875707") // USDT合约

	// ERC20 ABI (简化版，只包含transfer方法)
	erc20ABI := `[{"constant":false,"inputs":[{"name":"_to","type":"address"},{"name":"_value","type":"uint256"}],"name":"transfer","outputs":[{"name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"}]`

	// 定时器：每5秒发送一次交易
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// 计数器
	txCount := 0
	ethTxCount := 0
	erc20TxCount := 0

	logger.Info(ctx, "开始生成ETH交易",
		zap.String("from", fromAddress.Hex()),
		zap.String("to", toAddress.Hex()),
		zap.String("contract", contractAddress.Hex()))

	// 启动goroutine持续发送交易
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				txCount++

				// 交替发送ETH原生转账和ERC20转账
				if txCount%2 == 1 {
					// 发送ETH原生转账
					hash, err := sendETHTransfer(ctx, client, privateKey, chainID, fromAddress, toAddress, decimal.NewFromFloat(0.1))
					if err != nil {
						logger.Error(ctx, "发送ETH原生转账失败", zap.Error(err))
					} else {
						ethTxCount++
						logger.Info(ctx, "✅ ETH原生转账已发送",
							zap.String("hash", hash),
							zap.Int("total_eth_tx", ethTxCount))
					}
				} else {
					// 发送ERC20合约转账
					hash, err := sendERC20Transfer(ctx, client, privateKey, chainID, fromAddress, contractAddress, toAddress, decimal.NewFromFloat(100), erc20ABI)
					if err != nil {
						logger.Error(ctx, "发送ERC20转账失败", zap.Error(err))
					} else {
						erc20TxCount++
						logger.Info(ctx, "✅ ERC20合约转账已发送",
							zap.String("hash", hash),
							zap.String("contract", contractAddress.Hex()),
							zap.Int("total_erc20_tx", erc20TxCount))
					}
				}
			}
		}
	}()

	// 等待中断信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info(ctx, "停止生成交易",
		zap.Int("total_eth_tx", ethTxCount),
		zap.Int("total_erc20_tx", erc20TxCount),
		zap.Int("total_tx", txCount))
	cancel()
}

// sendETHTransfer 发送ETH原生转账
func sendETHTransfer(ctx context.Context, client *ethclient.Client, privateKey *ecdsa.PrivateKey, chainID *big.Int, from, to common.Address, amount decimal.Decimal) (string, error) {
	// 获取nonce
	nonce, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		return "", fmt.Errorf("failed to get nonce: %w", err)
	}

	// 转换金额为Wei
	amountWei := amount.Mul(decimal.NewFromInt(1e18)).BigInt()

	// 获取Gas价格
	gasTipCap, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get gas tip: %w", err)
	}

	head, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("failed to get header: %w", err)
	}

	baseFee := head.BaseFee
	if baseFee == nil {
		baseFee = big.NewInt(0)
	}

	gasFeeCap := new(big.Int).Add(
		new(big.Int).Mul(baseFee, big.NewInt(2)),
		gasTipCap,
	)

	// 构建交易
	txPayload := &types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       21000, // ETH转账标准Gas Limit
		To:        &to,
		Value:     amountWei,
		Data:      nil,
	}

	tx := types.NewTx(txPayload)

	// 签名
	signedTx, err := types.SignTx(tx, types.LatestSignerForChainID(chainID), privateKey)
	if err != nil {
		return "", fmt.Errorf("sign failed: %w", err)
	}

	// 发送交易
	err = client.SendTransaction(ctx, signedTx)
	if err != nil {
		return "", fmt.Errorf("send transaction failed: %w", err)
	}

	return signedTx.Hash().Hex(), nil
}

// sendERC20Transfer 发送ERC20合约转账
func sendERC20Transfer(ctx context.Context, client *ethclient.Client, privateKey *ecdsa.PrivateKey, chainID *big.Int, from, contract, to common.Address, amount decimal.Decimal, erc20ABI string) (string, error) {
	// 获取nonce
	nonce, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		return "", fmt.Errorf("failed to get nonce: %w", err)
	}

	// 打包ERC20 transfer数据
	parsedABI, err := abi.JSON(strings.NewReader(erc20ABI))
	if err != nil {
		return "", fmt.Errorf("failed to parse ABI: %w", err)
	}

	// 转换金额为Wei（假设18位精度）
	amountWei := amount.Mul(decimal.NewFromInt(1e18)).BigInt()

	// 打包transfer函数调用数据
	txData, err := parsedABI.Pack("transfer", to, amountWei)
	if err != nil {
		return "", fmt.Errorf("failed to pack transfer data: %w", err)
	}

	// 获取Gas价格
	gasTipCap, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get gas tip: %w", err)
	}

	head, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("failed to get header: %w", err)
	}

	baseFee := head.BaseFee
	if baseFee == nil {
		baseFee = big.NewInt(0)
	}

	gasFeeCap := new(big.Int).Add(
		new(big.Int).Mul(baseFee, big.NewInt(2)),
		gasTipCap,
	)

	// 构建交易（ERC20转账的Value为0，金额在Data中）
	txPayload := &types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       100000, // 合约调用需要更多Gas
		To:        &contract,
		Value:     big.NewInt(0), // ERC20转账Value为0
		Data:      txData,
	}

	tx := types.NewTx(txPayload)

	// 签名
	signedTx, err := types.SignTx(tx, types.LatestSignerForChainID(chainID), privateKey)
	if err != nil {
		return "", fmt.Errorf("sign failed: %w", err)
	}

	// 发送交易
	err = client.SendTransaction(ctx, signedTx)
	if err != nil {
		return "", fmt.Errorf("send transaction failed: %w", err)
	}

	return signedTx.Hash().Hex(), nil
}
