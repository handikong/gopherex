// ConfirmDeposits 批量确认充值
package repo

import (
	"context"
	"fmt"

	"gopherex.com/internal/wallet/domain"
	"gopherex.com/pkg/orm"
	"gopherex.com/pkg/xerr"
	"gorm.io/gorm"
)

func (r *Repo) ConfirmDeposits(ctx context.Context, chain string, currentHeight int64, confirmNum int64) (int64, error) {
	// 1. 先在 Go 里算出"只要小于等于这个高度的块，都算确认了"
	// 例如：当前106，需要6个确认。
	// safeHeight = 106 - 6 + 1 = 101。
	// 也就是 101, 100, 99... 这些块里的交易都安全了。
	safeHeight := currentHeight - confirmNum + 1

	// 2. 执行更新
	// 现在的 SQL 变成了： block_height <= ?
	// MySQL 可以完美利用 block_height 字段上的索引进行范围查询！
	res := r.db.WithContext(ctx).Model(&domain.Recharge{}).
		Where("chain = ? AND status = ? AND block_height <= ?",
			chain, domain.RechargeStatusPending, safeHeight).
		Update("status", domain.RechargeStatusConfirmed)

	if res.Error != nil {
		return 0, res.Error
	}

	return res.RowsAffected, nil
}

// ========== Repository 接口实现（补充方法） ==========

// UpdateDepositStatusToConfirmed 将充值记录状态改为 Confirmed（实现 domain.Repository 接口）
// 必须确保是从 Pending -> Confirmed，防止重复处理
func (r *Repo) UpdateDepositStatusToConfirmed(ctx context.Context, id int64) error {
	res := r.getDb(ctx).WithContext(ctx).Model(&domain.Recharge{}).
		Where("id = ? AND status = ?", id, domain.RechargeStatusPending). // 🔒 乐观锁：确保之前是 Pending
		Update("status", domain.RechargeStatusConfirmed)

	if res.Error != nil {
		return xerr.New(xerr.DbError, fmt.Sprintf("update status failed: %v", res.Error))
	}

	if res.RowsAffected == 0 {
		// 如果影响行数为 0，说明该记录可能已经被别的线程处理过了（状态不是 Pending）
		// 返回一个特定错误，或者直接 nil (视业务为幂等成功)
		return fmt.Errorf("deposit %d status is not pending or not found", id)
	}
	return nil
}
func (r *Repo) GetPendingDeposits(ctx context.Context, chain string, height int64) ([]*domain.Recharge, error) {
	deposits := make([]*domain.Recharge, 0)
	err := r.getDb(ctx).WithContext(ctx).Model(&domain.Recharge{}).
		Where("chain = ? AND status = ? AND block_height <= ?", chain, domain.RechargeStatusPending, height).
		Find(&deposits).Error
	if err != nil {
		return nil, err
	}
	return deposits, nil
}

func (r *Repo) GetRechargeListById(ctx context.Context, chain string, Symbol string, status domain.RechargeType, page int, limit int) ([]*domain.Recharge, error) {
	rechargeList := make([]*domain.Recharge, 0, limit)
	db := r.getDb(ctx).WithContext(ctx).Model(&domain.Recharge{}).Where(" status = ?", status)
	if chain != "" {
		db = db.Where("chain = ?", chain)
	}
	if Symbol != "" {
		db = db.Where("symbol = ?", Symbol)
	}
	db = db.Order("created_at DESC")

	err := orm.ApplyPagination(db, page, limit).Find(&rechargeList).Error
	return rechargeList, err
}

// CreateDeposit 创建充值记录
func (r *Repo) CreateDeposit(ctx context.Context, deposit *domain.Recharge) error {
	err := r.getDb(ctx).WithContext(ctx).Table("deposits").Create(deposit).Error
	if err != nil {
		return xerr.New(xerr.DbError, fmt.Sprintf("create deposit failed: %v", err))
	}
	return nil
}

// GetDeposit 根据ID获取充值记录
func (r *Repo) GetDeposit(ctx context.Context, id int64) (*domain.Recharge, error) {
	var deposit domain.Recharge
	err := r.getDb(ctx).WithContext(ctx).Table("deposits").
		Where("id = ?", id).
		First(&deposit).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, xerr.New(xerr.RequestParamsError, "充值记录不存在")
		}
		return nil, xerr.New(xerr.DbError, fmt.Sprintf("get deposit failed: %v", err))
	}
	return &deposit, nil
}
