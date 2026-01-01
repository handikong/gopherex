package mysql

import (
	"context"

	"gopherex.com/internal/funds"
	"gopherex.com/internal/funds/repo"
	"gopherex.com/internal/funds/repo/model"
	"gorm.io/gorm"
)

type balancesRepo struct {
	db *gorm.DB
}

func NewBalancesRepo(db *gorm.DB) repo.BalancesRepo {
	return &balancesRepo{db: db}
}

func (r *balancesRepo) GetBalances(ctx context.Context, userID uint64, asset string) ([]model.BalanceRow, error) {
	// repo 层可以做最小防呆，避免 userID=0 全表扫
	if userID == 0 {
		return []model.BalanceRow{}, nil
	}
	q := r.db.WithContext(ctx).
		Model(&model.BalanceRow{}).
		Where("owner_type = ? AND owner_id = ?", funds.OwnerUser, userID)

	if asset != "" {
		q = q.Where("asset = ?", asset)
	}

	// 按 asset/bucket 排序，输出稳定
	var rows []model.BalanceRow
	if err := q.Order("asset ASC, bucket ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
