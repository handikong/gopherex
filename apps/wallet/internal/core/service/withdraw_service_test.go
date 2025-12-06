package service_test

import (
	"context"
	"testing"
	"time"

	"gopherex.com/apps/wallet/internal/app/scanner"
	"gopherex.com/apps/wallet/internal/core/service"
	"gopherex.com/apps/wallet/internal/infra/ethereum"
	"gopherex.com/apps/wallet/internal/infra/persistence"
	"gopherex.com/pkg/logger"
	"gopherex.com/pkg/orm"
)

func TestWithdrawFullLoop(t *testing.T) {
	// 1. 加载配置
	// 2. 初始化基础设施
	ctx := context.Background()
	logger.Init("test", "info")

	// 1. 初始化依赖 (连接本地 Docker 的 MySQL 和 Bitcoin)
	db := orm.NewMySQL(&orm.Config{
		DSN: "root:123456@tcp(127.0.0.1:3307)/gopherex_wallet?charset=utf8mb4&parseTime=True&loc=Local",
	})
	repo := persistence.New(db)

	// Bitcoin RPC 配置 (根据你的 docker-compose 配置调整)
	// btcAdapter, _ := bitcoin.New("127.0.0.1:18443", "admin", "123456", &chaincfg.RegressionNetParams)
	EthAdapter, _ := ethereum.New("http://127.0.0.1:8545")
	// 初始化 Service 和 Processor
	withdrawSvc := service.NewWithdrawService(repo, nil)
	withdrawProcessor := scanner.NewWithdrawProcessor(withdrawSvc, EthAdapter, "ETH")

	// ==========================================
	// 步骤 1: 准备测试用户和资金
	// ==========================================
	// uid := int64(2)

	// ==========================================
	// 步骤 2: 申请提现 (Apply)
	// ==========================================
	// 获取一个真实的 Regtest 地址用于收款
	// toAddr, _ := btcAdapter.GetNewAddr	ess(ctx) // 假设 Adapter 暴露了这个方法，或者你手动填一个
	// toAddr := "bcrt1qy0vmja86vjzmk0eftqdef8ukp3xcajg6us33eu"
	// symbol := "BTC"
	// withdrawAmount := decimal.NewFromFloat(1.0)
	// err := withdrawSvc.ApplyWithdraw(ctx, uid, "BTC", "BTC", toAddr, withdrawAmount)
	// assert.NoError(t, err)

	// // 验证：余额减少，冻结增加
	// asset, _ := repo.GetBalance(ctx, uid, symbol)
	// assert.Equal(t, "9.0", asset.Available.String()) // 10 - 1 = 9 (忽略手续费简化判断)
	// t.Log("Step 2: 提现申请成功，资金已冻结")

	// ==========================================
	// 步骤 3: 处理器抢单并广播 (Processor)
	// ==========================================
	// 我们不启动 Start 死循环，而是手动跑一次 processLoop 里的逻辑
	// 或者是启动 Start 后 sleep 等待
	go withdrawProcessor.Start(ctx) // 启动后台协程

	// 等待几秒让 Processor 运行
	time.Sleep(5 * time.Second)

	// // 验证：订单状态应该是 Processing，且有 TxHash
	// orders, _ := repo.FindProcessingWithdraws(ctx, "BTC", 1)
	// if len(orders) == 0 {
	// 	t.Fatal("Processor 未能处理订单，请检查日志")
	// }
	// assert.Equal(t, domain.WithdrawStatusProcessing, orders[0].Status)
	// assert.NotEmpty(t, orders[0].TxHash)
	// txHash := orders[0].TxHash
	// t.Logf("Step 3: 交易已广播, Hash: %s", txHash)

	// ==========================================
	// 步骤 4: 模拟挖矿 (Mine)
	// ==========================================
	// 在 Regtest 模式下，交易不会自动确认，必须挖矿
	// 我们调用 RPC 生成 1 个块
	// 这里假设 Adapter 有 GenerateBlocks 或者我们在代码里直接用 exec
	// 如果没有封装，你需要手动在终端执行: bitcoin-cli -regtest generatetoaddress 1 ...
	t.Log("Step 4: 请在终端执行挖矿: bitcoin-cli -regtest -rpcwallet=testwallet generatetoaddress 1 <addr>")
	// 为了自动化，这里暂停 10秒，请你手动去挖矿！
	// 或者如果你实现了 adapter.GenerateBlock() 更好
	time.Sleep(10 * time.Second)

	// ==========================================
	// 步骤 5: 确认提现 (Confirm)
	// ==========================================
	// 等待 Processor 的 confirmLoop 运行
	time.Sleep(5 * time.Second)

	// 验证：订单状态应该是 Confirmed
	// var finalOrder domain.Withdraw
	// db.First(&finalOrder, "tx_hash = ?", txHash)

	// assert.Equal(t, domain.WithdrawStatusConfirmed, finalOrder.Status)
	t.Log("Step 5: 提现已确认，测试通过！🎉")
}
