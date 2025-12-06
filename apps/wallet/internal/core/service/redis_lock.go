package service

import (
	"context"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Lua 脚本：释放锁
// KEYS[1]: 锁的 key
// ARGV[1]: 锁的 value (token)，防止误删别人的锁
const unlockScript = `
if redis.call("get", KEYS[1]) == ARGV[1] then
    return redis.call("del", KEYS[1])
else
    return 0
end
`

type DistLock struct {
	client     *redis.Client
	key        string
	token      string        // 锁的唯一标识 (UUID)，谁加锁谁解锁
	expiration time.Duration // 锁的自动过期时间 (看门狗机制的基础)
}

func NewDistLock(client *redis.Client, key string, expiration time.Duration) *DistLock {
	return &DistLock{
		client:     client,
		key:        key,
		token:      uuid.New().String(), // 每个锁实例生成唯一的 Token
		expiration: expiration,
	}
}

// TryLock 尝试获取锁（非阻塞，一次性）
func (l *DistLock) TryLock(ctx context.Context) (bool, error) {
	// SetNX 在 Redis 2.6.12 之后被整合到 SET 命令的参数中
	// NX: 只有 Key 不存在时才设置
	// PX: 过期时间 (毫秒)
	success, err := l.client.SetNX(ctx, l.key, l.token, l.expiration).Result()
	if err != nil {
		return false, err
	}
	return success, nil
}

// Lock 自旋锁 (SpinLock) - 推荐使用
// 实现了简单的重试机制，适合高并发短任务
func (l *DistLock) Lock(ctx context.Context, retryTimes int, retryInterval time.Duration) (bool, error) {
	for i := 0; i < retryTimes; i++ {
		success, err := l.TryLock(ctx)
		if err != nil {
			return false, err
		}
		if success {
			return true, nil
		}

		// 没抢到锁，稍微睡一会儿再试 (随机抖动，防止共振)
		// 性能优化细节：加上随机时间，防止所有等待线程同时唤醒冲击 Redis
		sleepTime := retryInterval + time.Duration(rand.Intn(10))*time.Millisecond

		select {
		case <-ctx.Done(): // 上下文超时/取消，立刻退出
			return false, ctx.Err()
		case <-time.After(sleepTime):
			continue
		}
	}
	return false, nil // 重试次数用尽
}

// Unlock 安全释放锁
func (l *DistLock) Unlock(ctx context.Context) (bool, error) {
	// 执行 Lua 脚本，确保原子性
	res, err := l.client.Eval(ctx, unlockScript, []string{l.key}, l.token).Result()
	if err != nil {
		return false, err
	}
	// redis 返回 1 表示删除成功，0 表示 Key 不存在或 Token 不匹配
	return res.(int64) == 1, nil
}
