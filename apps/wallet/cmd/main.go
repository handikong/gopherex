package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/zeromicro/go-zero/core/conf"

	"gopherex.com/apps/wallet/config"
	"gopherex.com/apps/wallet/internal/app/scanner"
	"gopherex.com/apps/wallet/internal/core/handler"
	"gopherex.com/apps/wallet/internal/core/service"
	"gopherex.com/apps/wallet/internal/infra/bitcoin"
	"gopherex.com/apps/wallet/internal/infra/ethereum"
	"gopherex.com/apps/wallet/internal/infra/persistence"
	"gopherex.com/pkg/logger"
	"gopherex.com/pkg/orm"
	"gopherex.com/pkg/xredis"
)

var configFile = flag.String("f", "../etc/wallet.yaml", "the config file")

func main() {
	flag.Parse()

	// 1. 加载配置
	var c config.Config
	conf.MustLoad(*configFile, &c)
	// 2. 初始化基础设施
	logger.Init(c.Name, "info")

	db := orm.NewMySQL(&orm.Config{
		DSN:         c.Mysql.DataSource,
		MaxIdle:     c.Mysql.MaxIdle,
		MaxOpen:     c.Mysql.MaxOpen,
		MaxLifetime: c.Mysql.MaxLifetime,
	})

	rdb := xredis.NewRedis(&xredis.Config{
		Addr:     c.Redis.Addr,
		Password: c.Redis.Password,
		DB:       c.Redis.DB,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger.Info(ctx, "✅ Infrastructure initialized")

	// 3. 初始化组件 (依赖注入)

	// A. Repo (数据持久化)
	repo := persistence.New(db)

	// B. Handler (业务处理)
	depositHandler := handler.NewDepositHandler(db)
	depositHandler.AddWatchAddress("bcrt1q...") // 添加你要监控的地址(Docker生成的)

	// C. Adapter (比特币适配器)
	// 注意：这里用配置里的参数
	btcAdapter, err := bitcoin.New(
		c.Bitcoin.Host,
		c.Bitcoin.User,
		c.Bitcoin.Pass,
		&chaincfg.RegressionNetParams, // 暂时硬编码为回归测试网
	)
	if err != nil {
		log.Fatalf("BTC Adapter init failed: %v", err)
	}
	assetService := service.NewAssetService(repo)

	// 4. 初始化 Scanner Engine
	_ = scanner.New(
		&scanner.Config{
			Chain:           "BTC",
			Interval:        3 * time.Second,
			ConfirmInterval: 10 * time.Second,
			ConfirmNum:      1, // Regtest 1个确认就够
			ConsumerCount:   5,
		},
		rdb,
		btcAdapter,
		depositHandler,
		repo,
		assetService,
	)

	ethAdapter, err := ethereum.New(c.Bitcoin.EthUrl)
	if err != nil {
		log.Fatal(err)
	}

	// 2. 初始化 ETH 引擎
	ethEngine := scanner.New(
		&scanner.Config{
			Chain:           "ETH",
			Interval:        3 * time.Second,
			ConfirmInterval: 10 * time.Second,
			ConfirmNum:      6, // ETH 需要 6-12 个确认
			ConsumerCount:   5,
		},
		rdb,
		ethAdapter,
		depositHandler, // 复用同一个 Handler!
		repo,
		assetService,
	)

	// 5. 启动引擎
	// go btcEngine.Start(ctx)
	go ethEngine.Start(ctx)

	// 6. 优雅退出
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info(ctx, "Shutdown signal received...")
	cancel()
	// 这里可以加一个 waitGroup 等待 Engine 完全退出
	time.Sleep(1 * time.Second)
}
