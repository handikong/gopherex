package service_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"gopherex.com/apps/wallet/internal/core/service"
	"gopherex.com/pkg/xredis"
)

// 模拟业务逻辑
func RedisLock(rdb *redis.Client, userId int) error {
	ctx := context.Background()
	lockKey := fmt.Sprintf("lock_asset_%d", userId)

	// 🔥 修复点1: 必须乘以 time.Second，否则是纳秒
	mutex := service.NewDistLock(rdb, lockKey, 5*time.Second)

	// 尝试加锁：重试 20 次 (增加重试次数，让更多协程有机会抢到)，间隔 50ms
	locked, err := mutex.Lock(ctx, 1, 50*time.Millisecond)
	if err != nil {
		return fmt.Errorf("redis error: %v", err)
	}
	if !locked {
		return fmt.Errorf("lock contention") // 没抢到
	}

	// 🔥 修复点2: 只有抢到锁了，才注册解锁的 defer
	defer mutex.Unlock(ctx)

	// 🔥 修复点3: 模拟业务处理耗时 (持有锁 20ms)
	// 如果不Sleep，锁瞬间释放，并发测试没有意义
	time.Sleep(20 * time.Millisecond)

	return nil
}

func TestRedisLock(t *testing.T) {
	fmt.Println("🚀 开始并发测试...")

	// 初始化 Redis (确保你的 Redis 真的在 127.0.0.1:6379 运行)
	rdb := xredis.NewRedis(&xredis.Config{
		Addr:     "127.0.0.1:6379",
		Password: "",
		DB:       0,
	})

	var wg sync.WaitGroup
	var successCount int32 // 使用原子计数器
	var failCount int32

	// 模拟 50 个并发 (100个对于本地单机测试可能过多，导致 Redis 连接池排队超时，显得像卡死)
	concurrency := 50

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			err := RedisLock(rdb, 1) // 所有人抢同一个 UserID=1 的锁

			if err == nil {
				atomic.AddInt32(&successCount, 1)
				t.Logf("✅ 协程 %d 抢锁成功", idx)
			} else {
				atomic.AddInt32(&failCount, 1)
				// 打开这个日志可以看到失败原因
				// t.Logf("❌ 协程 %d 失败: %v", idx, err)
			}
		}(i)
	}

	wg.Wait()

	fmt.Printf("\n====== 测试结果 ======\n")
	fmt.Printf("并发数: %d\n", concurrency)
	fmt.Printf("成功拿到锁: %d\n", successCount)
	fmt.Printf("抢锁失败: %d\n", failCount)

	// 验证：理论上成功数应该 > 0，且因为有 Sleep，成功数应该远小于并发数
	if successCount == 0 {
		t.Error("严重错误：没有一个协程抢到锁！检查 Redis 连接或代码逻辑。")
	}
}
