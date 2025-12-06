package service

import (
	"context"

	"github.com/redis/go-redis/v9"
	"gopherex.com/internal/watcher/domain"
)

type ScanService struct {
	repo        domain.ScanerRepo // 使用接口类型，符合依赖倒置原则
	redisClinet *redis.Client
}

func NewScanService(repo domain.ScanerRepo, redisClient *redis.Client) *ScanService {
	return &ScanService{repo: repo, redisClinet: redisClient}
}

// 获取游标
func (s *ScanService) GetLastCursor(ctx context.Context, chain string, mode string) (int64, string, error) {
	return s.repo.GetLastCursor(ctx, chain, mode)
}

// 更新游标
func (s *ScanService) UpdateCursor(ctx context.Context, chain string, height int64, mode string) error {
	return s.repo.UpdateCursor(ctx, chain, height, mode)
}

// 判断地址是否存在
func (s *ScanService) IsAddress(toAddress string) bool {
	return true
}
