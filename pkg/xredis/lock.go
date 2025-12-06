package xredis

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type RedisLockMaster struct {
	rdb *redis.Client
	id  string // 当前节点的唯一ID（比如 IP+UUID）
}

func NewRedisLockMaster(rdb *redis.Client) *RedisLockMaster {
	// 组合随机id
	uuid := uuid.New().String()
	timeUnix := time.Now().Nanosecond()
	id := fmt.Sprintf("%s%d", uuid, timeUnix)
	return &RedisLockMaster{
		rdb: rdb,
		id:  id,
	}

}

// 实现redis的锁
func (r *RedisLockMaster) TryAcquireMaster(
	ctx context.Context,
	MasterLockKey string,
	ttl time.Duration,
) bool {
	// SETNX: 如果 Key 不存在则设置成功，否则失败
	// 我们设置一个过期时间，防止死锁（Master 挂了后锁会自动释放）
	success, err := r.rdb.SetNX(ctx, MasterLockKey, r.id, ttl).Result()
	if err != nil {
		fmt.Printf("[%s] Redis error: %v\n", r.id, err)
		return false
	}

	if !success {
		// 如果抢锁失败，检查锁是不是自己的（用于续期）
		//  使用lua并发处理
		val, _ := r.rdb.Get(ctx, MasterLockKey).Result()
		if val == r.id {
			// 是自己的锁，续期
			r.rdb.Expire(ctx, MasterLockKey, ttl)
			return true
		}
	}

	return success
}
