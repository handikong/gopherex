package app

import (
	"fmt"
	"math/rand"
	"time"

	"golang.org/x/sync/singleflight"
)

type BalanceKey struct {
	UserID int64
	Acct   string // funding/spot/perp
	Symbol string // USDT/BTC
}

type Balance struct {
	Avail   int64
	Frozen  int64
	Version int64
}

type Cache interface {
	Get(k string) (Balance, bool)
	Set(k string, v Balance, ttlSeconds int)
	Del(k string)
}

type Repo interface {
	GetBalanceFromDB(key BalanceKey) (Balance, error)
}

type Service struct {
	repo  Repo
	cache Cache
	sf    singleflight.Group // 防止同一个 key cache miss 时打爆 DB
}

func cacheKey(k BalanceKey) string {
	return fmt.Sprintf("bal:%d:%s:%s", k.UserID, k.Acct, k.Symbol)
}

func (s *Service) GetBalance(key BalanceKey) (Balance, error) {
	k := cacheKey(key)
	if v, ok := s.cache.Get(k); ok {
		return v, nil
	}

	// 防击穿：同一时刻只有一个 goroutine 去 DB
	vAny, err, _ := s.sf.Do(k, func() (any, error) {
		// double-check
		if v, ok := s.cache.Get(k); ok {
			return v, nil
		}
		v, err := s.repo.GetBalanceFromDB(key)
		if err != nil {
			return Balance{}, err
		}

		ttl := 30 + rand.Intn(30) // 防雪崩：TTL 打散
		s.cache.Set(k, v, ttl)
		return v, nil
	})
	if err != nil {
		return Balance{}, err
	}
	return vAny.(Balance), nil
}

func (s *Service) UpdateBalance_DoubleDelete(key BalanceKey, newV Balance) error {
	// 1) 先更新 DB（假设这里已经事务提交成功）
	// repo.UpdateBalanceInDB(...)

	// 2) 删缓存
	k := cacheKey(key)
	s.cache.Del(k)

	// 3) 延迟删（把“旧读回填”再清掉）
	time.AfterFunc(500*time.Millisecond, func() {
		s.cache.Del(k)
	})

	return nil
}

func (s *Service) OnBalanceChangedEvent(key BalanceKey, v Balance) {
	k := cacheKey(key)

	cur, ok := s.cache.Get(k)
	if !ok || v.Version >= cur.Version {
		// 只允许新版本覆盖旧版本：乱序/重放都不怕
		s.cache.Set(k, v, 60)
	}
}
