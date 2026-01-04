package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gagliardetto/solana-go/rpc"
	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"gopherex.com/learn/eth/scan"
	"gopherex.com/learn/eth/scan/interfaces"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var (
	infuraURL     = "https://sepolia.infura.io/v3/3b3402ed33804bc28c87b29fd1152c0c"
	privateKeyHex = "0xb702274007ec799e812cdc219be1b8bbf9be2b0e9f20ee75a6b92dd4ab480782" // 请替换为实际的私钥（十六进制字符串）
	contractABI   = `[{"constant":true,"inputs":[{"name":"","type":"address"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"}]`
)

// 0x4620e7d0dac58e49dd78e3c66feabc00a6a80a77

func main() {
	fmt.Println(1111)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var cluster rpc.Cluster = rpc.MainNetBeta
	var chair = interfaces.NewSolana(&cluster)
	sqlDB, err := sql.Open("mysql", "root:123456@tcp(127.0.0.1:3307)/funds_service_db?parseTime=true")
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		log.Fatalf("failed to ping database: %v", err)
	}

	gormDB, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      sqlDB, // 复用已有的 sql.DB 连接池
		SkipInitializeWithVersion: false,
	}), &gorm.Config{
		SkipDefaultTransaction: true,
		PrepareStmt:            true,
		NowFunc:                func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		log.Fatalf("failed to init gorm: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:     "127.0.0.1:6379",
		Password: "",
	})

	var cfg = &scan.HandlerCfg{
		Symbol:         "solana",
		MasterInterval: time.Second * 2,
	}
	handler := scan.NewHandler(ctx, chair, gormDB, rdb, cfg)
	handler.Master()
	select {
	case <-ctx.Done():
		log.Println("等待退出")
	}

}

//	// 连接到Infura的Sepolia测试网
//	c, err := ethclient.Dial(infuraURL)
//	if err != nil {
//		log.Fatalf("Failed to connect to the Ethereum client: %v", err)
//	}
//	privateKeyHex = removeHexPrefix(privateKeyHex)
//	// 使用正确的私钥来创建ecdsa.PrivateKey
//	privateKey, err := crypto.HexToECDSA(privateKeyHex)
//	if err != nil {
//		log.Fatalf("Failed to decode private key: %v", err)
//	}
//
//	// 获取公钥
//	publicKey := privateKey.Public()
//	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
//	if !ok {
//		log.Fatalf("Failed to cast public key to ECDSA")
//	}
//
//	// 根据公钥生成地址
//	address := crypto.PubkeyToAddress(*publicKeyECDSA)
//	fmt.Println("fromAddress:", address.Hex())
//	//
//	//balance, err := c.BalanceAt(context.Background(), address, nil)
//	//if err != nil {
//	//	log.Fatalf("Failed to get balance: %v", err)
//	//}
//	//
//	//fmt.Println("balance:", showBalances(balance))
//	// 你可以在这里进一步实现其他操作，比如查询余额、发送交易等
//
//	// 创建并签名交易
//	//nonce, err := c.PendingNonceAt(context.Background(), address)
//	//if err != nil {
//	//	log.Fatalf("Failed to get nonce: %v", err)
//	//}
//	//
//	//gasPrice, err := c.SuggestGasPrice(context.Background())
//	//if err != nil {
//	//	log.Fatalf("Failed to get gas price: %v", err)
//	//}
//	//
//	//toAddress := common.HexToAddress("0x52f1984Cd3e46e1214dB222D3Ff63712E7aCEedD") // 替换为接收地址
//	//// 获取当前 Gas Limit
//	//msg := ethereum.CallMsg{
//	//	To:   &toAddress,
//	//	Data: nil,
//	//}
//	//gasLimit, err := c.EstimateGas(context.Background(), msg)
//	//if err != nil {
//	//	log.Fatalf("Failed to estimate gas: %v", err)
//	//}
//	//
//	//// 签名交易
//	//chainID, err := c.NetworkID(context.Background())
//	//if err != nil {
//	//	log.Fatalf("Failed to get network ID: %v", err)
//	//}
//	//
//	//amount := ethToWei(0.04) // 0.05 ETH
//	//// 创建交易对象，使用 NewTx 替换 NewTransaction
//	//trans := types.NewTx(&types.LegacyTx{
//	//	Nonce:    nonce,
//	//	To:       &toAddress,
//	//	Value:    amount,
//	//	Gas:      gasLimit,
//	//	GasPrice: gasPrice,
//	//})
//	//
//	//signedTx, err := types.SignTx(trans, types.NewEIP155Signer(chainID), privateKey)
//	//if err != nil {
//	//	log.Fatalf("Failed to sign transaction: %v", err)
//	//}
//	//
//	//// 发送交易
//	//err = c.SendTransaction(context.Background(), signedTx)
//	//if err != nil {
//	//	log.Fatalf("Failed to send transaction: %v", err)
//	//}
//	//
//	//fmt.Printf("Transaction sent: %s\n", signedTx.Hash().Hex())
//
//	//toAddress := common.HexToAddress("0x52f1984Cd3e46e1214dB222D3Ff63712E7aCEedD")
//	//使用智能合约 ABI 调用合约的 `balanceOf` 函数
//	contractAddress := common.HexToAddress("0x26c7c4473fefe6e9662f2ccfd9501d47c0fbce8b") // 替换为你要查询的代币合约地址
//	parsedABI, err := abi.JSON(strings.NewReader(contractABI))
//	if err != nil {
//		log.Fatalf("Failed to parse contract ABI: %v", err)
//	}
//	// 查询指定地址的代币余额
//	data, err := parsedABI.Pack("balanceOf", contractAddress)
//	if err != nil {
//		log.Fatalf("Failed to pack data: %v", err)
//	}
//	callMsg := ethereum.CallMsg{
//		To:   &contractAddress,
//		Data: data,
//	}
//	result, err := c.CallContract(context.Background(), callMsg, nil)
//	if err != nil {
//		log.Fatalf("Failed to call contract: %v", err)
//	}
//	var tokenBalance *big.Int
//	err = parsedABI.UnpackIntoInterface(&tokenBalance, "balanceOf", result)
//	if err != nil {
//		log.Fatalf("Failed to unpack result: %v", err)
//	}
//	fmt.Printf("Token balance: %s\n", tokenBalance.String())
//
//}
//
//func removeHexPrefix(hexString string) string {
//	if strings.HasPrefix(hexString, "0x") {
//		return hexString[2:]
//	}
//	return hexString
//}
//
//func showBalances(b *big.Int) string {
//	wei := new(big.Float).SetInt(b)
//	eth := new(big.Float).Quo(wei, new(big.Float).SetInt(big.NewInt(1_000_000_000_000_000_000)))
//
//	return fmt.Sprintf("%s ETH", eth.Text('f', 18))
//}
//
//func ethToWei(eth float64) *big.Int {
//	wei := new(big.Int)
//	wei.SetString(fmt.Sprintf("%.0f", eth*1e18), 10)
//	return wei
//}
