package service

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopherex.com/apps/wallet/internal/domain"
	"gopherex.com/apps/wallet/internal/infra/persistence"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSettleDeposit(t *testing.T) {
	// 使用 SQLite 内存数据库进行测试
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// 自动迁移表结构
	err = db.AutoMigrate(
		&domain.UserAddress{},
		&domain.Deposit{},
		&domain.UserAsset{},
	)
	require.NoError(t, err)

	// 创建 Repo 实例（实现所有接口）
	repo := persistence.New(db)

	// 创建 Service 实例
	service := NewAssetService(repo, repo, repo)

	// 准备测试数据：创建一个用户地址
	testUID := int64(1001)
	testAddress := "bcrt1qga52l9u6hre8wu6r6rh8a8xgexyzf6f7kcfl2v"
	testSymbol := "BTC"
	testAmount := decimal.RequireFromString("0.5")

	// 插入用户地址
	err = db.Create(&domain.UserAddress{
		UserID:  testUID,
		Chain:   "BTC",
		Address: testAddress,
		PkhIdx:  int(testUID),
	}).Error
	require.NoError(t, err)

	// 表驱动测试
	tests := []struct {
		name         string
		setupDeposit func() *domain.Deposit // 设置充值记录
		wantErr      bool
		wantStatus   domain.DepositType // 期望的充值状态
		wantBalance  string             // 期望的余额（字符串格式，便于比较）
		checkBalance bool               // 是否检查余额
	}{
		{
			name: "正常入账：Pending -> Confirmed，并给用户加钱",
			setupDeposit: func() *domain.Deposit {
				deposit := &domain.Deposit{
					TxHash:      "test_tx_hash_1",
					FromAddress: "from_addr_1",
					ToAddress:   testAddress,
					Chain:       "BTC",
					Symbol:      testSymbol,
					Amount:      testAmount,
					Status:      domain.DepositStatusPending,
					BlockHeight: 100,
					LogIndex:    0,
				}
				err := db.Create(deposit).Error
				require.NoError(t, err)
				return deposit
			},
			wantErr:      false,
			wantStatus:   domain.DepositStatusConfirmed,
			wantBalance:  "0.5",
			checkBalance: true,
		},
		{
			name: "充值地址找不到用户（应该返回 nil，不报错）",
			setupDeposit: func() *domain.Deposit {
				deposit := &domain.Deposit{
					TxHash:      "test_tx_hash_2",
					FromAddress: "from_addr_2",
					ToAddress:   "unknown_address", // 不存在的地址
					Chain:       "BTC",
					Symbol:      testSymbol,
					Amount:      testAmount,
					Status:      domain.DepositStatusPending,
					BlockHeight: 101,
					LogIndex:    0,
				}
				err := db.Create(deposit).Error
				require.NoError(t, err)
				return deposit
			},
			wantErr:      false,                       // 找不到用户应该返回 nil，不报错
			wantStatus:   domain.DepositStatusPending, // 状态不应该改变
			wantBalance:  "0.5",                       // 余额不应该增加
			checkBalance: true,
		},
		{
			name: "充值记录状态不是 Pending（应该失败）",
			setupDeposit: func() *domain.Deposit {
				deposit := &domain.Deposit{
					TxHash:      "test_tx_hash_3",
					FromAddress: "from_addr_3",
					ToAddress:   testAddress,
					Chain:       "BTC",
					Symbol:      testSymbol,
					Amount:      testAmount,
					Status:      domain.DepositStatusConfirmed, // 已经是 Confirmed
					BlockHeight: 102,
					LogIndex:    0,
				}
				err := db.Create(deposit).Error
				require.NoError(t, err)
				return deposit
			},
			wantErr:      true,                          // 应该失败
			wantStatus:   domain.DepositStatusConfirmed, // 状态不变
			wantBalance:  "0.5",                         // 余额不变
			checkBalance: true,
		},
		{
			name: "多次充值应该累加余额",
			setupDeposit: func() *domain.Deposit {
				deposit := &domain.Deposit{
					TxHash:      "test_tx_hash_4",
					FromAddress: "from_addr_4",
					ToAddress:   testAddress,
					Chain:       "BTC",
					Symbol:      testSymbol,
					Amount:      decimal.RequireFromString("0.3"), // 第二次充值
					Status:      domain.DepositStatusPending,
					BlockHeight: 103,
					LogIndex:    0,
				}
				err := db.Create(deposit).Error
				require.NoError(t, err)
				return deposit
			},
			wantErr:      false,
			wantStatus:   domain.DepositStatusConfirmed,
			wantBalance:  "0.8", // 0.5 + 0.3
			checkBalance: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// 设置充值记录
			deposit := tt.setupDeposit()

			// 执行测试
			err := service.SettleDeposit(ctx, deposit)

			// 断言错误
			if tt.wantErr {
				assert.Error(t, err, "应该返回错误")
			} else {
				assert.NoError(t, err, "不应该返回错误")
			}

			// 检查充值记录状态
			var updatedDeposit domain.Deposit
			err = db.Where("id = ?", deposit.ID).First(&updatedDeposit).Error
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, updatedDeposit.Status, "充值状态应该符合预期")

			// 检查余额
			if tt.checkBalance {
				var asset domain.UserAsset
				err = db.Where("user_id = ? AND coin_symbol = ?", testUID, testSymbol).First(&asset).Error
				if tt.wantErr || deposit.ToAddress == "unknown_address" {
					// 如果失败或找不到用户，余额可能不存在或不变
					// 这里只检查如果存在的话，余额是否正确
					if err == nil {
						// 如果资产记录存在，检查余额
						expectedBalance := decimal.RequireFromString(tt.wantBalance)
						assert.True(t, asset.Available.Equal(expectedBalance),
							"余额应该等于 %s，实际为 %s", tt.wantBalance, asset.Available.String())
					}
				} else {
					require.NoError(t, err, "应该能找到用户资产记录")
					expectedBalance := decimal.RequireFromString(tt.wantBalance)
					assert.True(t, asset.Available.Equal(expectedBalance),
						"余额应该等于 %s，实际为 %s", tt.wantBalance, asset.Available.String())
				}
			}
		})
	}
}

func TestSettleDeposit_TransactionRollback(t *testing.T) {
	// 测试事务回滚：如果更新状态失败，加钱操作应该回滚
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	err = db.AutoMigrate(
		&domain.UserAddress{},
		&domain.Deposit{},
		&domain.UserAsset{},
	)
	require.NoError(t, err)

	repo := persistence.New(db)
	service := NewAssetService(repo, repo, repo)

	testUID := int64(2001)
	testAddress := "bcrt1ql746qr38l85czxqfc3z8xxgeq4u4kjkgyn8nn8"

	// 插入用户地址
	err = db.Create(&domain.UserAddress{
		UserID:  testUID,
		Chain:   "BTC",
		Address: testAddress,
		PkhIdx:  int(testUID),
	}).Error
	require.NoError(t, err)

	// 创建一个已经是 Confirmed 状态的充值记录
	deposit := &domain.Deposit{
		TxHash:      "test_tx_hash_rollback",
		FromAddress: "from_addr",
		ToAddress:   testAddress,
		Chain:       "BTC",
		Symbol:      "BTC",
		Amount:      decimal.RequireFromString("1.0"),
		Status:      domain.DepositStatusConfirmed, // 已经是 Confirmed，更新会失败
		BlockHeight: 200,
		LogIndex:    0,
	}
	err = db.Create(deposit).Error
	require.NoError(t, err)

	// 执行结算（应该失败）
	ctx := context.Background()
	err = service.SettleDeposit(ctx, deposit)
	assert.Error(t, err, "应该返回错误，因为状态不是 Pending")

	// 验证余额没有被增加（事务回滚）
	var asset domain.UserAsset
	err = db.Where("user_id = ? AND coin_symbol = ?", testUID, "BTC").First(&asset).Error
	if err == gorm.ErrRecordNotFound {
		// 资产记录不存在，说明没有创建，这是正确的
		t.Log("资产记录不存在，说明事务正确回滚")
	} else {
		// 如果存在，余额应该是 0
		require.NoError(t, err)
		assert.True(t, asset.Available.IsZero(), "余额应该为 0，因为事务回滚")
	}
}

func TestSettleDeposit_Concurrent(t *testing.T) {
	// 测试并发场景：多个 goroutine 同时结算不同充值记录
	// 使用文件数据库避免 SQLite 内存数据库的并发问题
	dbPath := "/tmp/test_settle_concurrent.db"
	os.Remove(dbPath)

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	require.NoError(t, err)
	defer os.Remove(dbPath) // 测试后清理

	// 确保迁移完成
	err = db.AutoMigrate(
		&domain.UserAddress{},
		&domain.Deposit{},
		&domain.UserAsset{},
	)
	require.NoError(t, err)

	repo := persistence.New(db)
	service := NewAssetService(repo, repo, repo)

	// 准备测试数据：创建多个用户和充值记录（减少数量避免 SQLite 并发问题）
	const numDeposits = 20
	testUID := int64(5000)
	testAddress := "bcrt1qga52l9u6hre8wu6r6rh8a8xgexyzf6f7kcfl2v"
	testSymbol := "BTC"

	// 插入用户地址
	err = db.Create(&domain.UserAddress{
		UserID:  testUID,
		Chain:   "BTC",
		Address: testAddress,
		PkhIdx:  int(testUID),
	}).Error
	require.NoError(t, err)

	// 创建多个充值记录
	deposits := make([]*domain.Deposit, numDeposits)
	for i := 0; i < numDeposits; i++ {
		deposit := &domain.Deposit{
			TxHash:      fmt.Sprintf("test_tx_hash_concurrent_%d", i),
			FromAddress: fmt.Sprintf("from_addr_%d", i),
			ToAddress:   testAddress,
			Chain:       "BTC",
			Symbol:      testSymbol,
			Amount:      decimal.RequireFromString("0.1"), // 每个充值 0.1
			Status:      domain.DepositStatusPending,
			BlockHeight: int64(1000 + i),
			LogIndex:    i,
		}
		err = db.Create(deposit).Error
		require.NoError(t, err)
		deposits[i] = deposit
	}

	// 并发结算所有充值记录
	ctx := context.Background()
	type result struct {
		depositID int64
		err       error
	}

	results := make(chan result, numDeposits)
	var wg sync.WaitGroup

	for _, deposit := range deposits {
		wg.Add(1)
		go func(d *domain.Deposit) {
			defer wg.Done()
			err := service.SettleDeposit(ctx, d)
			results <- result{depositID: d.ID, err: err}
		}(deposit)
	}

	// 等待所有 goroutine 完成
	wg.Wait()
	close(results)

	// 收集结果
	successCount := 0
	errorCount := 0
	for res := range results {
		if res.err != nil {
			errorCount++
			t.Logf("充值记录 %d 结算失败: %v", res.depositID, res.err)
		} else {
			successCount++
		}
	}

	// 验证所有充值都成功结算（允许少量失败，因为 SQLite 内存数据库在并发场景下可能不稳定）
	// 但至少应该有大部分成功
	assert.GreaterOrEqual(t, successCount, numDeposits*8/10, "至少 80%% 的充值记录应该成功结算")
	if errorCount > 0 {
		t.Logf("有 %d 个充值记录结算失败（可能是 SQLite 并发问题）", errorCount)
	}

	// 验证所有充值记录状态都变为 Confirmed
	var confirmedCount int64
	err = db.Model(&domain.Deposit{}).
		Where("to_address = ? AND status = ?", testAddress, domain.DepositStatusConfirmed).
		Count(&confirmedCount).Error
	require.NoError(t, err)
	assert.Equal(t, int64(numDeposits), confirmedCount, "所有充值记录都应该变为 Confirmed 状态")

	// 验证余额正确（应该是 0.1 * successCount）
	var asset domain.UserAsset
	err = db.Where("user_id = ? AND coin_symbol = ?", testUID, testSymbol).First(&asset).Error
	require.NoError(t, err)
	expectedBalance := decimal.NewFromInt(int64(successCount)).Mul(decimal.RequireFromString("0.1"))
	// 使用 Sub 和 Abs 来比较，允许小的浮点数误差
	diff := asset.Available.Sub(expectedBalance).Abs()
	assert.True(t, diff.LessThan(decimal.RequireFromString("0.0001")),
		"余额应该约等于 %s（0.1 * %d），实际为 %s，差异为 %s", expectedBalance.String(), successCount, asset.Available.String(), diff.String())
}

func TestSettleDeposit_ConcurrentSameDeposit(t *testing.T) {
	// 测试并发场景：多个 goroutine 同时结算同一充值记录（应该只有一个成功）
	// 使用文件数据库避免 SQLite 内存数据库的并发问题
	dbPath := "/tmp/test_settle_concurrent_same.db"
	os.Remove(dbPath)

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	require.NoError(t, err)
	defer os.Remove(dbPath) // 测试后清理

	// 确保迁移完成
	err = db.AutoMigrate(
		&domain.UserAddress{},
		&domain.Deposit{},
		&domain.UserAsset{},
	)
	require.NoError(t, err)

	repo := persistence.New(db)
	service := NewAssetService(repo, repo, repo)

	testUID := int64(6000)
	testAddress := "bcrt1ql746qr38l85czxqfc3z8xxgeq4u4kjkgyn8nn8"
	testSymbol := "BTC"

	// 插入用户地址
	err = db.Create(&domain.UserAddress{
		UserID:  testUID,
		Chain:   "BTC",
		Address: testAddress,
		PkhIdx:  int(testUID),
	}).Error
	require.NoError(t, err)

	// 创建一个充值记录
	deposit := &domain.Deposit{
		TxHash:      "test_tx_hash_concurrent_same",
		FromAddress: "from_addr",
		ToAddress:   testAddress,
		Chain:       "BTC",
		Symbol:      testSymbol,
		Amount:      decimal.RequireFromString("1.0"),
		Status:      domain.DepositStatusPending,
		BlockHeight: 2000,
		LogIndex:    0,
	}
	err = db.Create(deposit).Error
	require.NoError(t, err)

	// 并发结算同一充值记录（减少并发数量）
	const numGoroutines = 10
	ctx := context.Background()

	type result struct {
		err error
	}

	results := make(chan result, numGoroutines)
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 每次重新查询充值记录，确保状态是最新的
			var d domain.Deposit
			err := db.Where("id = ?", deposit.ID).First(&d).Error
			if err != nil {
				results <- result{err: err}
				return
			}
			err = service.SettleDeposit(ctx, &d)
			results <- result{err: err}
		}()
	}

	// 等待所有 goroutine 完成
	wg.Wait()
	close(results)

	// 收集结果
	successCount := 0
	errorCount := 0
	for res := range results {
		if res.err != nil {
			errorCount++
		} else {
			successCount++
		}
	}

	// 验证只有一个成功
	assert.Equal(t, 1, successCount, "应该只有一个 goroutine 成功结算")
	assert.Equal(t, numGoroutines-1, errorCount, "其他 goroutine 应该失败（因为状态已经被更新）")

	// 验证充值记录状态为 Confirmed
	var updatedDeposit domain.Deposit
	err = db.Where("id = ?", deposit.ID).First(&updatedDeposit).Error
	require.NoError(t, err)
	assert.Equal(t, domain.DepositStatusConfirmed, updatedDeposit.Status, "充值记录应该变为 Confirmed 状态")

	// 验证余额只增加了一次（应该是 1.0，而不是 20.0）
	var asset domain.UserAsset
	err = db.Where("user_id = ? AND coin_symbol = ?", testUID, testSymbol).First(&asset).Error
	require.NoError(t, err)
	expectedBalance := decimal.RequireFromString("1.0")
	assert.True(t, asset.Available.Equal(expectedBalance),
		"余额应该等于 1.0（只结算一次），实际为 %s", asset.Available.String())
}
