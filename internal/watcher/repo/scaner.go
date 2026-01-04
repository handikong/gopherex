package repo

import (
	"context"
	"errors"
	"fmt"

	"gopherex.com/internal/watcher/domain"
	"gopherex.com/pkg/xerr"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GetLastCursor 获取指定链的最后扫描高度和Hash
func (r *Repo) GetLastCursor(ctx context.Context, chain string, mode string) (int64, string, error) {
	var scan = domain.Scan{}
	// 查询 scans 表
	err := r.db.WithContext(ctx).Table("scans").
		Select("current_height").
		Where("symbol = ?", chain).
		First(&scan).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 如果没找到，说明是第一次运行，返回 0, ""
			return 0, "", nil
		}
		return 0, "", xerr.New(xerr.DbError, fmt.Sprintf("query cursor failed: %v", err))
	}

	return scan.CurrentHeight, "", nil
}

// UpdateCursor 更新扫描游标 (Upsert: 不存在则插入，存在则更新)
func (r *Repo) UpdateCursor(ctx context.Context, chain string, height int64, mode string) error {
	db := r.getDb(ctx).WithContext(ctx)
	// 使用 GORM 的 Clauses 实现 Upsert (INSERT ON DUPLICATE KEY UPDATE)
	// 这里的表名 scans 必须和数据库一致
	// 唯一索引是 symbol 的组合
	scan := map[string]interface{}{
		"symbol":         chain,
		"current_height": height,
	}

	err := db.Table("scans").Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "symbol"}}, // 唯一索引列：symbol
		DoUpdates: clause.AssignmentColumns([]string{"current_height"}),
	}).Create(&scan).Error

	if err != nil {
		return xerr.New(xerr.DbError, fmt.Sprintf("update cursor failed: %v", err))
	}
	return nil
}
