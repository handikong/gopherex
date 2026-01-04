package scan

import (
	"context"
	"errors"
	"fmt"

	"gopherex.com/pkg/xerr"
	"gorm.io/gorm"
)

type Scan struct {
	symbol        string // 链类型
	CurrentHeight int64  //高度
}

type Repo struct {
	db *gorm.DB
}

func NewDb(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

// Transaction 实现事务
func (r *Repo) Transaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 把 tx 注入到 context 中
		txCtx := context.WithValue(ctx, "tx_db", tx)
		return fn(txCtx)
	})
}

// getDb 获取数据库连接，如果 context 中有事务则使用事务，否则使用普通连接
func (r *Repo) getDb(ctx context.Context) *gorm.DB {
	if tx, ok := ctx.Value("tx_db").(*gorm.DB); ok {
		return tx
	}
	return r.db
}

// GetLastCursor 获取指定链的最后扫描高度和Hash
func (r *Repo) GetLastCursor(ctx context.Context, symbol string) (int64, error) {
	var scan = Scan{}
	// 查询 scans 表
	err := r.db.WithContext(ctx).Table("scans").
		Select("current_height").
		Where("symbol", symbol).
		First(&scan).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 如果没找到，说明是第一次运行，返回 0, ""
			return 0, nil
		}
		return 0, xerr.New(xerr.DbError, fmt.Sprintf("query cursor failed: %v", err))
	}

	return scan.CurrentHeight, nil
}

// UpdateCursor 根据 symbol 更新当前扫描高度
func (r *Repo) UpdateCursor(ctx context.Context, symbol string, height int64) error {
	db := r.getDb(ctx).WithContext(ctx)
	err := db.Table("scans").Where("symbol = ?", symbol).Update("current_height", height).Error
	if err != nil {
		return xerr.New(xerr.DbError, fmt.Sprintf("update cursor failed: %v", err))
	}
	return nil
}
