package strage

import (
	"github.com/redis/go-redis/v9"
	"gopherex.com/internal/watcher/domain"
	"gopherex.com/internal/watcher/service"
)

func NewStrategy(cfg *domain.RechargeConfig, adapter domain.ChainAdapter, svc *service.ScanService, rs *redis.Client) domain.ScanStrageWatcher {
	switch cfg.ScanMode {
	case domain.ModeBlock: // BTC 或 ETH原生
		return newBlockStrage(rs, svc, adapter)
	case domain.ModeLog: // ETH ERC20
		return newLogStrage(rs, svc, adapter)
	default:
		panic("unknown scan mode")
	}
}
