package funds

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
	fundsv1 "gopherex.com/gen/go/fund_service/v1"
)

type Cache interface {
	GetBalances(ctx context.Context, userID uint64, asset string) (*fundsv1.GetBalancesRes, bool, error)
	SetBalances(ctx context.Context, userID uint64, asset string, res *fundsv1.GetBalancesRes, ttl time.Duration) error
	DelBalances(ctx context.Context, userID uint64, asset string) error
}

type redisCache struct {
	client *redis.Client
}

func NewRedisCache(c *redis.Client) Cache {
	return &redisCache{client: c}
}

func (r *redisCache) GetBalances(ctx context.Context, userID uint64, asset string) (*fundsv1.GetBalancesRes, bool, error) {
	key := r.getKey(userID, asset)

	b, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	res := &fundsv1.GetBalancesRes{}
	if err := proto.Unmarshal(b, res); err != nil {
		// 缓存脏了就删掉，避免持续命中错误
		_ = r.client.Del(ctx, key).Err()
		return nil, false, err
	}
	return res, true, nil
}

func (r *redisCache) SetBalances(ctx context.Context, userID uint64, asset string, res *fundsv1.GetBalancesRes, ttl time.Duration) error {
	key := r.getKey(userID, asset)

	b, err := proto.Marshal(res)
	if err != nil {
		return err
	}
	// 加入随机时间 防止抖动
	randTTl := withJitter(ttl, 300*time.Millisecond)
	return r.client.Set(ctx, key, b, randTTl).Err()
}

func (r *redisCache) DelBalances(ctx context.Context, userID uint64, asset string) error {
	key := r.getKey(userID, asset)
	return r.client.Del(ctx, key).Err()
}

func (r *redisCache) getKey(userID uint64, asset string) string {
	// asset 为空：表示“所有资产”
	if asset == "" {
		asset = "ALL"
	}
	return fmt.Sprintf("funds:bal:%d:%s", userID, asset)
}

func withJitter(ttl time.Duration, jitter time.Duration) time.Duration {
	if ttl <= 0 || jitter <= 0 {
		return ttl
	}
	// [0, jitter) 的随机
	j := time.Duration(rand.Int63n(int64(jitter)))
	return ttl + j
}
