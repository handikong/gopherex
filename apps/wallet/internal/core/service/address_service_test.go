package service

import (
	"context"
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
