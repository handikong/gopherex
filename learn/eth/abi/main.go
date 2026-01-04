package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

var (
	infuraURL   = "https://sepolia.infura.io/v3/3b3402ed33804bc28c87b29fd1152c0c"
	contractABI = `[{"constant":true,"inputs":[{"name":"","type":"address"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"}]`
)

func main() {
	client, err := ethclient.Dial(infuraURL)
	if err != nil {
		log.Fatalf("Failed to connect to the Ethereum client: %v", err)
	}

	// ERC-20 合约地址
	contractAddress := common.HexToAddress("0x8fcc6467b8bf8d4cfdbc0f52990e3962c8ffd0a3")

	// 解析智能合约的 ABI
	parsedABI, err := abi.JSON(strings.NewReader(contractABI))
	if err != nil {
		log.Fatalf("Failed to parse contract ABI: %v", err)
	}

	// 查询指定地址的代币余额
	address := common.HexToAddress("0x26c7c4473fefe6e9662f2ccfd9501d47c0fbce8b") // 替换为你要查询的地址
	data, err := parsedABI.Pack("balanceOf", address)
	if err != nil {
		log.Fatalf("Failed to pack data: %v", err)
	}

	callMsg := ethereum.CallMsg{
		To:   &contractAddress,
		Data: data,
	}

	// 调用智能合约查询余额
	result, err := client.CallContract(context.Background(), callMsg, nil)
	if err != nil {
		log.Fatalf("Failed to call contract: %v", err)
	}

	// 解析返回的结果
	var tokenBalance *big.Int
	err = parsedABI.UnpackIntoInterface(&tokenBalance, "balanceOf", result)
	if err != nil {
		log.Fatalf("Failed to unpack result: %v", err)
	}
	fmt.Println(tokenBalance)
	// 打印代币余额
	fmt.Printf("Token balance: %s\n", showBalances(tokenBalance, 18))
}

//
// // 根据decimals显示代币余额

//var (
//	infuraURL = "https://sepolia.infura.io/v3/3b3402ed33804bc28c87b29fd1152c0c"
//)
//
//func main() {
//	client, err := ethclient.Dial(infuraURL)
//	if err != nil {
//		log.Fatalf("Failed to connect to the Ethereum client: %v", err)
//	}
//
//	// 合约地址
//	contractAddress := common.HexToAddress("0x8fcc6467b8bf8d4cfdbc0f52990e3962c8ffd0a3")
//
//	// 获取合约的 ETH 余额
//	balance, err := client.BalanceAt(context.Background(), contractAddress, nil)
//	if err != nil {
//		log.Fatalf("Failed to get balance: %v", err)
//	}
//
//	fmt.Printf("Contract ETH balance: %s\n", showBalances(balance, 18))
//}

func showBalances(b *big.Int, decimals int) string {
	// 将余额转为浮动数值
	wei := new(big.Float).SetInt(b)
	eth := new(big.Float).Quo(wei, new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))) // 除以 10^decimals

	// 返回可读的代币余额
	return fmt.Sprintf("%s", eth.Text('f', 18)) // 以 18 位小数格式化
}
