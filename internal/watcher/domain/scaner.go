package domain

import (
	"context"
)

// 对应数据库表 scans
type Scan struct {
	Chain         string // 链类型
	Mode          string // 扫码模式
	CurrentHeight int64  //高度
	CurrentHash   string // hash

}

type ScanStrageWatcher interface {
	// 获取步长
	GetSkip() int64
	// 获取数据并且推送到redis
	GetFetchAndPush(ctx context.Context, to, from int64) (height int64, res []*ChainTransfer, err error)
}

type ScanerRepo interface {
	// 获取最后一个游标
	GetLastCursor(ctx context.Context, chain string, mode string) (height int64, hash string, err error)
	// UpdateCursor 更新游标 (通常和业务处理在一个事务里，这里单独定义是为了灵活性)
	UpdateCursor(ctx context.Context, chain string, height int64, mode string) error
}

// 抽象工厂方法
