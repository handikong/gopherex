package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/zeromicro/go-zero/core/conf"
	"gopherex.com/apps/wallet/config"
	"gopherex.com/pkg/logger"
	"gopherex.com/pkg/orm"
	"gopherex.com/pkg/xredis"
)

var configFile = flag.String("f", "apps/wallet/etc/wallet.yaml", "the config file")

func main() {
	flag.Parse()

	// 1. 加载配置
	var c config.Config
	// MustLoad 会自动读取 yaml 并填充到 c 结构体中，如果失败会 panic
	conf.MustLoad(*configFile, &c)
	fmt.Printf("%+v", c)
	// 2. 初始化基础设施 (使用配置里的参数)

	// 初始化日志
	logger.Init(c.Name, "info")

	// 初始化 MySQL
	// 我们要把 config.Config 里的 Mysql 配置，转成 pkg/orm 需要的 Config
	_ = orm.NewMySQL(&orm.Config{
		DSN:         c.Mysql.DataSource,
		MaxIdle:     c.Mysql.MaxIdle,
		MaxOpen:     c.Mysql.MaxOpen,
		MaxLifetime: c.Mysql.MaxLifetime,
	})
	logger.Info(context.Background(), "✅ MySQL 连接成功")

	// 初始化 Redis
	_ = xredis.NewRedis(&xredis.Config{
		Addr:     c.Redis.Addr,
		Password: c.Redis.Password,
		DB:       c.Redis.DB,
	})
	logger.Info(context.Background(), "✅ Redis 连接成功")

	// ... 接下来注入到 Engine 或 Handler 中 ...
	// engine := scanner.NewEngine(..., db, rdb)

	// 阻塞运行
	select {}
}
