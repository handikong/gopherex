package orm

import "gorm.io/gorm"

// ApplyPagination 应用分页到 GORM 查询
// 如果 page <= 0 或 limit <= 0，则不应用分页
func ApplyPagination(db *gorm.DB, page, limit int) *gorm.DB {
	if page > 0 && limit > 0 {
		offset := (page - 1) * limit
		return db.Offset(offset).Limit(limit)
	}
	return db
}

