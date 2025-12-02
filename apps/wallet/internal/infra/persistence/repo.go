package persistence

import (
	"context"
	"errors"
	"fmt"

	"gopherex.com/apps/wallet/internal/domain"
	"gopherex.com/pkg/xerr"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repo struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

// 确保 Repo 实现了 domain.AddressRepo 接口
var _ domain.AddressRepo = (*Repo)(nil)

// GetLastCursor 获取指定链的最后扫描高度和Hash
func (r *Repo) GetLastCursor(ctx context.Context, chain string) (int64, string, error) {
	// 对应数据库表 scans
	type Scan struct {
		CurrentHeight int64
		CurrentHash   string
	}

	var scan Scan
	// 查询 scans 表
	err := r.db.WithContext(ctx).Table("scans").
		Select("current_height, current_hash").
		Where("chain = ?", chain).
		First(&scan).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 如果没找到，说明是第一次运行，返回 0, ""
			return 0, "", nil
		}
		return 0, "", xerr.New(xerr.DbError, fmt.Sprintf("query cursor failed: %v", err))
	}

	return scan.CurrentHeight, scan.CurrentHash, nil
}

// UpdateCursor 更新扫描游标 (Upsert: 不存在则插入，存在则更新)
func (r *Repo) UpdateCursor(ctx context.Context, chain string, height int64, hash string) error {
	// 使用 GORM 的 Clauses 实现 Upsert (INSERT ON DUPLICATE KEY UPDATE)
	// 这里的表名 scans 必须和数据库一致
	scan := map[string]interface{}{
		"chain":          chain,
		"current_height": height,
		"current_hash":   hash,
	}

	err := r.db.WithContext(ctx).Table("scans").Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "chain"}}, // 唯一索引列
		DoUpdates: clause.AssignmentColumns([]string{"current_height", "current_hash", "updated_at"}),
	}).Create(&scan).Error

	if err != nil {
		return xerr.New(xerr.DbError, fmt.Sprintf("update cursor failed: %v", err))
	}
	return nil
}

// Rollback 回滚操作：删除 >= height 的所有数据，并将游标重置
// 这是一个事务操作
func (r *Repo) Rollback(ctx context.Context, chain string, height int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 删除 deposits 表中所有高度 >= height 的记录 (因为它们是分叉产生的无效数据)
		if err := tx.Table("deposits").
			Where("chain = ? AND block_height >= ?", chain, height).
			Delete(nil).Error; err != nil {
			return err
		}

		updates := map[string]interface{}{
			"current_height": height - 1,
			"current_hash":   "", // <--- 关键！
		}

		// 2. 将 scans 表的游标回退到 height - 1
		// 注意：这里我们没法知道 height-1 的 hash 是什么，暂时留空或者需要业务层传进来
		// 简单处理：只回退高度，Hash 留空 (下次扫描会重新覆盖)
		if err := tx.Table("scans").
			Where("chain = ?", chain).
			Updates(updates).Error; err != nil {
			return err
		}

		return nil
	})
}

// ConfirmDeposits 批量确认充值
func (r *Repo) ConfirmDeposits(ctx context.Context, chain string, currentHeight int64, confirmNum int64) (int64, error) {
	// 1. 先在 Go 里算出"只要小于等于这个高度的块，都算确认了"
	// 例如：当前106，需要6个确认。
	// safeHeight = 106 - 6 + 1 = 101。
	// 也就是 101, 100, 99... 这些块里的交易都安全了。
	safeHeight := currentHeight - confirmNum + 1

	// 2. 执行更新
	// 现在的 SQL 变成了： block_height <= ?
	// MySQL 可以完美利用 block_height 字段上的索引进行范围查询！
	res := r.db.WithContext(ctx).Model(&domain.Deposit{}).
		Where("chain = ? AND status = ? AND block_height <= ?",
			chain, domain.DepositStatusPending, safeHeight).
		Update("status", domain.DepositStatusConfirmed)

	if res.Error != nil {
		return 0, res.Error
	}

	return res.RowsAffected, nil
}
