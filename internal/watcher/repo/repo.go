package repo

import (
	"context"

	"gopherex.com/internal/watcher/domain"
	"gorm.io/gorm"
)

type Repo struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

var (
	_ domain.ScanerRepo = (*Repo)(nil)
)

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
