package orm

import (
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Config struct {
	DSN         string // 连接字符串
	MaxIdle     int    // 最大空闲连接
	MaxOpen     int    // 最大打开连接
	MaxLifetime int    // 连接存活秒数
}

// NewMySQL 初始化 GORM
func NewMySQL(c *Config) *gorm.DB {
	db, err := gorm.Open(mysql.Open(c.DSN), &gorm.Config{
		// 生产环境建议用 Warn/Error，开发环境用 Info (打印SQL)
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		panic("failed to connect database: " + err.Error())
	}

	sqlDB, err := db.DB()
	if err != nil {
		panic(err)
	}

	// 关键配置：连接池优化
	sqlDB.SetMaxIdleConns(c.MaxIdle)
	sqlDB.SetMaxOpenConns(c.MaxOpen)
	sqlDB.SetConnMaxLifetime(time.Duration(c.MaxLifetime) * time.Second)

	return db
}
