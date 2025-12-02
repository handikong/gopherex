package service

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopherex.com/apps/wallet/internal/domain"
	"gopherex.com/apps/wallet/internal/infra/persistence"
	"gopherex.com/pkg/hdwallet"
	"gopherex.com/pkg/logger"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func init() {
	// 初始化 logger，避免测试时 panic
	logger.Init("wallet-service-test", "info")
}

func TestGenerateAddress(t *testing.T) {
	// 准备测试数据
	mnemonic := "test test test test test test test test test test test junk"
	wallet, err := hdwallet.New(mnemonic, &chaincfg.RegressionNetParams)
	require.NoError(t, err)

	// 使用 SQLite 内存数据库进行测试
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// 自动迁移表结构
	err = db.AutoMigrate(&domain.UserAddress{})
	require.NoError(t, err)

	// 创建 Repo 实例
	repo := persistence.New(db)

	// 创建 Service 实例
	service := NewAddressService(db, repo, wallet)

	// 表驱动测试
	tests := []struct {
		name    string
		uid     int64
		wantErr bool
		wantBTC bool // 是否期望生成 BTC 地址
		wantETH bool // 是否期望生成 ETH 地址
		checkDB bool // 是否检查数据库
	}{
		{
			name:    "正常生成地址",
			uid:     1001,
			wantErr: false,
			wantBTC: true,
			wantETH: true,
			checkDB: true,
		},
		{
			name:    "生成第二个用户地址",
			uid:     1002,
			wantErr: false,
			wantBTC: true,
			wantETH: true,
			checkDB: true,
		},
		{
			name:    "重复生成同一用户地址（应该失败，因为唯一约束）",
			uid:     1001,
			wantErr: true,
			wantBTC: false,
			wantETH: false,
			checkDB: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// 执行测试
			btcAddr, ethAddr, err := service.GenerateAddress(ctx, tt.uid)

			// 断言错误
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			// 断言地址不为空
			if tt.wantBTC {
				assert.NotEmpty(t, btcAddr, "BTC 地址不应为空")
				assert.Contains(t, btcAddr, "bcrt1", "BTC 地址应该是 regtest 格式")
			}

			if tt.wantETH {
				assert.NotEmpty(t, ethAddr, "ETH 地址不应为空")
				assert.Contains(t, ethAddr, "0x", "ETH 地址应该以 0x 开头")
				assert.Len(t, ethAddr, 42, "ETH 地址长度应该是 42 字符（包含 0x）")
			}

			// 检查数据库
			if tt.checkDB {
				// 检查 BTC 地址是否保存
				var btcUserAddr domain.UserAddress
				err = db.Where("user_id = ? AND chain = ?", tt.uid, "BTC").First(&btcUserAddr).Error
				assert.NoError(t, err, "应该能在数据库中找到 BTC 地址")
				assert.Equal(t, tt.uid, btcUserAddr.UserID)
				assert.Equal(t, "BTC", btcUserAddr.Chain)
				assert.Equal(t, btcAddr, btcUserAddr.Address)
				assert.Equal(t, int(tt.uid), btcUserAddr.PkhIdx)

				// 检查 ETH 地址是否保存
				var ethUserAddr domain.UserAddress
				err = db.Where("user_id = ? AND chain = ?", tt.uid, "ETH").First(&ethUserAddr).Error
				assert.NoError(t, err, "应该能在数据库中找到 ETH 地址")
				assert.Equal(t, tt.uid, ethUserAddr.UserID)
				assert.Equal(t, "ETH", ethUserAddr.Chain)
				assert.Equal(t, ethAddr, ethUserAddr.Address)
				assert.Equal(t, int(tt.uid), ethUserAddr.PkhIdx)
			}
		})
	}
}

func TestGenerateAddress_Deterministic(t *testing.T) {
	// 测试确定性：相同助记词和用户ID应该生成相同的地址
	mnemonic := "test test test test test test test test test test test junk"
	wallet1, err := hdwallet.New(mnemonic, &chaincfg.RegressionNetParams)
	require.NoError(t, err)

	wallet2, err := hdwallet.New(mnemonic, &chaincfg.RegressionNetParams)
	require.NoError(t, err)

	db1, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	db1.AutoMigrate(&domain.UserAddress{})
	repo1 := persistence.New(db1)
	service1 := NewAddressService(db1, repo1, wallet1)

	db2, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	db2.AutoMigrate(&domain.UserAddress{})
	repo2 := persistence.New(db2)
	service2 := NewAddressService(db2, repo2, wallet2)

	ctx := context.Background()
	uid := int64(2000)

	// 第一次生成
	btc1, eth1, err1 := service1.GenerateAddress(ctx, uid)
	require.NoError(t, err1)

	// 第二次生成（应该相同）
	btc2, eth2, err2 := service2.GenerateAddress(ctx, uid)
	require.NoError(t, err2)

	// 断言地址相同
	assert.Equal(t, btc1, btc2, "相同助记词和用户ID应该生成相同的 BTC 地址")
	assert.Equal(t, eth1, eth2, "相同助记词和用户ID应该生成相同的 ETH 地址")
}

func TestGenerateAddress_Concurrent(t *testing.T) {
	// 测试并发场景：多个 goroutine 同时生成不同用户的地址
	// 使用文件数据库避免 SQLite 内存数据库的并发问题
	mnemonic := "test test test test test test test test test test test junk"
	
	dbPath := "/tmp/test_address_concurrent.db"
	// 清理可能存在的旧数据库
	os.Remove(dbPath)
	
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	require.NoError(t, err)
	defer os.Remove(dbPath) // 测试后清理

	err = db.AutoMigrate(&domain.UserAddress{})
	require.NoError(t, err)

	repo := persistence.New(db)

	// 并发生成 50 个不同用户的地址（减少并发数量避免 SQLite 内存数据库问题）
	const numUsers = 50
	ctx := context.Background()

	type result struct {
		uid     int64
		btcAddr string
		ethAddr string
		err     error
	}

	results := make(chan result, numUsers)
	var wg sync.WaitGroup

	// 启动多个 goroutine 并发生成地址
	// 每个 goroutine 使用独立的 wallet 实例（因为 HDWallet 不是线程安全的）
	for i := 0; i < numUsers; i++ {
		wg.Add(1)
		go func(uid int64) {
			defer wg.Done()
			// 为每个 goroutine 创建独立的 wallet 实例
			wallet, err := hdwallet.New(mnemonic, &chaincfg.RegressionNetParams)
			if err != nil {
				results <- result{uid: uid, err: err}
				return
			}
			service := NewAddressService(db, repo, wallet)
			btcAddr, ethAddr, err := service.GenerateAddress(ctx, uid)
			results <- result{uid: uid, btcAddr: btcAddr, ethAddr: ethAddr, err: err}
		}(int64(3000 + i))
	}

	// 等待所有 goroutine 完成
	wg.Wait()
	close(results)

	// 收集结果并验证
	successCount := 0
	errorCount := 0
	addresses := make(map[int64]struct {
		btcAddr string
		ethAddr string
	})

	for res := range results {
		if res.err != nil {
			errorCount++
			t.Logf("用户 %d 生成地址失败: %v", res.uid, res.err)
		} else {
			successCount++
			addresses[res.uid] = struct {
				btcAddr string
				ethAddr string
			}{btcAddr: res.btcAddr, ethAddr: res.ethAddr}
		}
	}

	// 验证所有用户都成功生成地址
	assert.Equal(t, numUsers, successCount, "所有用户都应该成功生成地址")
	assert.Equal(t, 0, errorCount, "不应该有错误")

	// 验证数据库中的记录数量
	var count int64
	err = db.Model(&domain.UserAddress{}).Count(&count).Error
	require.NoError(t, err)
	assert.Equal(t, int64(numUsers*2), count, "应该有 %d 条地址记录（每个用户 BTC + ETH）", numUsers*2)

	// 验证每个用户的地址都正确保存
	for uid, addrs := range addresses {
		var btcAddr domain.UserAddress
		err = db.Where("user_id = ? AND chain = ?", uid, "BTC").First(&btcAddr).Error
		assert.NoError(t, err, "应该能找到用户 %d 的 BTC 地址", uid)
		assert.Equal(t, addrs.btcAddr, btcAddr.Address)

		var ethAddr domain.UserAddress
		err = db.Where("user_id = ? AND chain = ?", uid, "ETH").First(&ethAddr).Error
		assert.NoError(t, err, "应该能找到用户 %d 的 ETH 地址", uid)
		assert.Equal(t, addrs.ethAddr, ethAddr.Address)
	}
}

func TestGenerateAddress_ConcurrentSameUser(t *testing.T) {
	// 测试并发场景：多个 goroutine 同时为同一用户生成地址（应该只有一个成功）
	// 使用文件数据库避免 SQLite 内存数据库的并发问题
	mnemonic := "test test test test test test test test test test test junk"
	
	dbPath := "/tmp/test_address_concurrent_same.db"
	os.Remove(dbPath)
	
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	require.NoError(t, err)
	defer os.Remove(dbPath) // 测试后清理

	err = db.AutoMigrate(&domain.UserAddress{})
	require.NoError(t, err)

	repo := persistence.New(db)

	const numGoroutines = 10 // 减少并发数量
	const testUID = int64(4000)
	ctx := context.Background()

	type result struct {
		btcAddr string
		ethAddr string
		err     error
	}

	results := make(chan result, numGoroutines)
	var wg sync.WaitGroup

	// 启动多个 goroutine 同时为同一用户生成地址
	// 每个 goroutine 使用独立的 wallet 实例（因为 HDWallet 不是线程安全的）
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 为每个 goroutine 创建独立的 wallet 实例
			wallet, err := hdwallet.New(mnemonic, &chaincfg.RegressionNetParams)
			if err != nil {
				results <- result{err: err}
				return
			}
			service := NewAddressService(db, repo, wallet)
			btcAddr, ethAddr, err := service.GenerateAddress(ctx, testUID)
			results <- result{btcAddr: btcAddr, ethAddr: ethAddr, err: err}
		}()
	}

	// 等待所有 goroutine 完成
	wg.Wait()
	close(results)

	// 收集结果
	successCount := 0
	errorCount := 0
	var successResult result

	for res := range results {
		if res.err != nil {
			errorCount++
		} else {
			successCount++
			successResult = res
		}
	}

	// 验证只有一个成功
	assert.Equal(t, 1, successCount, "应该只有一个 goroutine 成功生成地址")
	assert.Equal(t, numGoroutines-1, errorCount, "其他 goroutine 应该因为唯一约束失败")

	// 验证数据库中的记录数量（应该只有 2 条：BTC + ETH）
	var count int64
	err = db.Model(&domain.UserAddress{}).Where("user_id = ?", testUID).Count(&count).Error
	require.NoError(t, err)
	assert.Equal(t, int64(2), count, "应该只有 2 条地址记录（BTC + ETH）")

	// 验证地址正确保存
	if successCount == 1 {
		var btcAddr domain.UserAddress
		err = db.Where("user_id = ? AND chain = ?", testUID, "BTC").First(&btcAddr).Error
		assert.NoError(t, err)
		assert.Equal(t, successResult.btcAddr, btcAddr.Address)

		var ethAddr domain.UserAddress
		err = db.Where("user_id = ? AND chain = ?", testUID, "ETH").First(&ethAddr).Error
		assert.NoError(t, err)
		assert.Equal(t, successResult.ethAddr, ethAddr.Address)
	}
}
