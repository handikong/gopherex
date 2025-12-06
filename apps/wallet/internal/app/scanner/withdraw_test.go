package scanner_test

// func TestETHWithdrawal(t *testing.T) {
// 	// 初始化日志
// 	logger.Init("test", "")
// 	ctx := context.Background()

// 	// 1. 初始化 Adapter (连接本地 Anvil)
// 	// 确保你的 Adapter 代码里使用的是私钥: ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80
// 	rpcURL := "http://127.0.0.1:8545"
// 	adapter, err := ethereum.New(rpcURL)
// 	if err != nil {
// 		t.Fatalf("连接 Anvil 失败: %v", err)
// 	}

// 	// 2. 准备测试数据
// 	// 接收方地址 (Anvil Account #1)
// 	toAddress := "0x5fc8d32690cc91d4c39d9d3abcbd16989f875707"
// 	// 提现金额 1.0 ETH
// 	amount := decimal.NewFromFloat(1.0)

// 	// 3. 【执行前】查询接收方余额
// 	balanceBefore, err := getBalance(ctx, adapter, toAddress)
// 	if err != nil {
// 		t.Fatalf("查询余额失败: %v", err)
// 	}
// 	t.Logf("💰 转账前余额: %s ETH", balanceBefore.String())

// 	// 4. 发起提现
// 	order := &domain.Withdraw{
// 		ID:        8888, // 模拟订单ID
// 		ToAddress: toAddress,
// 		Amount:    amount,
// 		Symbol:    "ETH",
// 	}

// 	txHash, err := adapter.SendWithdrawal(ctx, order)
// 	if err != nil {
// 		t.Fatalf("❌ 提现广播失败: %v", err)
// 	}
// 	t.Logf("✅ 提现广播成功, Hash: %s", txHash)

// 	// 5. 等待出块 (Anvil 默认是瞬间出块，但为了稳妥等 1 秒)
// 	time.Sleep(1 * time.Second)

// 	// 6. 验证交易状态
// 	status, err := adapter.GetTransactionStatus(ctx, txHash)
// 	assert.NoError(t, err)
// 	assert.Equal(t, domain.WithdrawStatusConfirmed, status, "交易应当已确认")

// 	// 7. 【执行后】查询接收方余额
// 	balanceAfter, err := getBalance(ctx, adapter, toAddress)
// 	if err != nil {
// 		t.Fatalf("查询余额失败: %v", err)
// 	}
// 	t.Logf("💰 转账后余额: %s ETH", balanceAfter.String())

// 	// 8. 断言：余额差值必须正好是 1.0
// 	diff := balanceAfter.Sub(balanceBefore)
// 	assert.True(t, diff.Equal(amount), "余额增加量应为 1.0 ETH")
// 	t.Log("🎉 测试通过！ETH 提现链路跑通！")
// }

// // 辅助函数：获取余额并转换为 Decimal (ETH单位)
// func getBalance(ctx context.Context, a domain.ChainAdapter, addressHex string) (decimal.Decimal, error) {
// 	addr := common.HexToAddress(addressHex)
// 	// AtBlockNumber: nil 代表最新高度
// 	wei, err := a.client.BalanceAt(ctx, addr, nil)
// 	if err != nil {
// 		return decimal.Zero, err
// 	}
// 	// Wei -> ETH
// 	d := decimal.NewFromBigInt(wei, 0)
// 	return d.Div(decimal.NewFromInt(1000000000000000000)), nil
// }
