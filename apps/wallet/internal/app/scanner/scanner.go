package scanner

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gopherex.com/apps/wallet/internal/domain"
	"gopherex.com/pkg/logger"
	"gopherex.com/pkg/safe"
)

// 先定义数据结构
type Config struct {
	Chain           string        // "BTC" "ETH"
	Interval        time.Duration //间隔扫描Scaner
	ConfirmNum      int64         //  确认数量
	StepBlock       uint8         // 每次跳跃多少个区块  一起扫描
	ConfirmInterval time.Duration // 间隔扫码确定充值
	ConsumerCount   uint8         // 多少个消费者
}

type Engine struct {
	config      *Config
	redisClinet *redis.Client
	adapter     domain.ChainAdapter
	handler     domain.Handler
	repository  domain.Repository
	blockChan   chan *domain.StandardBlock
}

func New(cfg *Config, r *redis.Client, adapter domain.ChainAdapter,
	handler domain.Handler, respo domain.Repository) *Engine {
	// 对默认的配置进行兜底
	if cfg.ConsumerCount == 0 {
		cfg.ConsumerCount = 1
	}
	if cfg.StepBlock == 0 {
		cfg.StepBlock = 1
	}

	return &Engine{
		config:      cfg,
		redisClinet: r,
		adapter:     adapter,
		handler:     handler,
		repository:  respo,
		blockChan:   make(chan *domain.StandardBlock, cfg.ConfirmNum*2),
	}
}

func (e *Engine) Start(ctx context.Context) {
	logger.Info(ctx, "Scaner Engine start",
		zap.String("Chain", e.config.Chain),
		zap.Int("confirmations", int(e.config.ConfirmNum)),
		zap.Any("config", e.config))

	// 构造消费者
	var wg sync.WaitGroup
	for work := uint8(0); work < e.config.ConsumerCount; work++ {
		wg.Add(1)
		safe.Go(func() {
			defer wg.Done()
			e.Consumer(ctx, work)
		})
	}
	// 构造消费者
	safe.Go(func() {
		e.Product(ctx)
		//  退出时关闭通道
		close(e.blockChan)
	})
	// 构造用户扫描的携程
	safe.Go(func() {
		e.Confirmer(ctx)
	})

	//  接受停止命令
	<-ctx.Done()
	// 等待所有消费者处理完
	wg.Wait()
	logger.Info(ctx, "🛑 Scanner Engine Stopped", zap.Any("config ", e.config))
}

// 消费者代码
func (e *Engine) Consumer(ctx context.Context, workNum uint8) {
	logger.Info(ctx, "👷 Worker started", zap.Uint8("worker_id", workNum))
	for block := range e.blockChan {
		logger.Info(ctx, "Processing block",
			zap.Uint8("worker", workNum),
			zap.Int64("height", block.Height),
			zap.Int("txs", len(block.Transactions)),
		)

		// 1. 调用业务 Handler (入库)
		// 这里假设 Handler 内部处理了幂等性
		if err := e.handler.HandlerBlock(ctx, block); err != nil {
			logger.Error(ctx, "Handle block failed", zap.Int64("height", block.Height), zap.Error(err))
			// 失败重试逻辑 (这里简单跳过，生产环境需要死信队列)
			continue
		}
		logger.Info(ctx, fmt.Sprintf("写入数据库的数据%d,%s", block.Height, block.Hash))
		// 2. 更新数据库游标 (Checkpoint)
		// 在分布式环境下，这一步其实应该由 Handler 在事务里一起做。
		// 如果 Handler 没做，这里补发一个 Update
		if err := e.repository.UpdateCursor(ctx, e.config.Chain, block.Height, block.Hash); err != nil {
			logger.Error(ctx, "Update cursor failed", zap.Error(err))
		}
	}
}

// 生产者代码
func (e *Engine) Product(ctx context.Context) {
	logger.Info(ctx, "进入Product")
	ticker := time.NewTicker(e.config.Interval)
	// 进来先查询高度
	currentHeight, currentHash, err := e.repository.GetLastCursor(ctx, e.config.Chain)

	logger.Info(ctx, fmt.Sprintf("当前数据库的高度是%d,区块hash是%s", currentHeight, currentHash))
	if err != nil {
		logger.Fatal(ctx, "Init cursor failed", zap.Error(err))
		return
	}
	logger.Info(ctx, "Scanner init cursor", zap.Int64("height", currentHeight), zap.String("hash", currentHash))

	// 进行循环
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 下一个区块
			nextHeight := currentHeight + 1
			// 当前链接上的区块
			chainHeight, err := e.adapter.GetBlockHeight(ctx)
			logger.Info(ctx, fmt.Sprintf("当前下一个区块的高度是%d,链上的高度是%d", nextHeight, chainHeight))
			if err != nil {
				logger.Error(ctx, "Get tip height failed", zap.Error(err))
				continue
			}

			// 如果下一个区块小于 链上的区块  就循环追加区块
			for nextHeight < chainHeight {
				// 分布式锁  这里锁也有问题
				// Key: scanner:lock:BTC:100
				lockKey := fmt.Sprintf("scanner:lock:%s:%d", e.config.Chain, nextHeight)
				locked, err := e.redisClinet.SetNX(ctx, lockKey, 1, 5*time.Minute).Result()
				if err != nil {
					logger.Error(ctx, "Redis lock error", zap.Error(err))
					time.Sleep(time.Second)
					continue
				}
				//  如果锁已经通过 就直接丢给生产者
				//  然后对现有高度进行添加
				// 如果没抢到锁，说明别的节点在处理，或者已经处理过了
				// 我们直接跳过这个块，去试探下一个 (蛙跳模式)
				if !locked {
					logger.Error(ctx, "Get tip height failed", zap.Error(err))
					// 为了保证本地状态连续，我们还是得更新 currentHeight
					// 但这里有个风险：如果别的节点处理失败了怎么办？
					// 商业级方案通常配合 Kafka，这里为了简化 MVP，我们假设别的节点会成功
					// 或者我们可以简单地休眠等待，不做蛙跳
					// e.logger.Debug("Block locked by others", zap.Int64("height", nextHeight))

					// 简单策略：没抢到就不处理，也不更新本地游标 (这意味着我们会反复尝试抢这个块，直到锁过期或我们抢到)
					// 这种策略适合 "主备模式" 或者是 "竞争消费模式"
					break
				}
				logger.Info(ctx, fmt.Sprintf("当前获取的区块为%d", nextHeight))

				// 获取区块
				block, err := e.adapter.FetchBlock(ctx, nextHeight)
				// 获取的数据为
				logger.Info(ctx, fmt.Sprintf("获取的数据为%+v", block))

				if err != nil {
					logger.Error(ctx, "Fetch block failed", zap.Int64("height", nextHeight), zap.Error(err))
					time.Sleep(time.Second)
					// 释放锁以便重试？或者等待锁自然过期
					e.redisClinet.Del(ctx, lockKey)
					break
				}
				// 4. 🔥 核心：防分叉 (Reorg Check)
				// 检查新块的父哈希是否等于我本地记录的当前哈希
				// 如果是高度1（前面是0），或者是第一次启动（currentHash为空），则跳过检查

				logger.Info(ctx, "当前的判断条件",
					zap.Int64("currentHeight", currentHeight),
					zap.String("currentHash", currentHash),
					zap.String("PrevHash", block.PrevHash))
				if currentHeight > 0 && currentHash != "" && block.PrevHash != currentHash {
					logger.Warn(ctx, "🚨 FORK DETECTED! Reorg triggered",
						zap.Int64("local_height", currentHeight),
						zap.String("local_hash", currentHash),
						zap.String("new_block_prev", block.PrevHash),
					)

					// 触发回滚：删除数据库里 currentHeight 的数据
					if err := e.repository.Rollback(ctx, e.config.Chain, currentHeight); err != nil {
						logger.Error(ctx, "Rollback failed", zap.Error(err))
						break // 停止，等待人工介入或下次重试
					}

					// 内存游标回退
					currentHeight--
					// 重新去数据库查上一块的 Hash，以便下轮循环继续校验
					_, prevHash, _ := e.repository.GetLastCursor(ctx, e.config.Chain)
					currentHash = prevHash

					// 释放锁，因为我们处理失败了（或者说处理的是回滚）
					e.redisClinet.Del(ctx, lockKey)
					continue
				}
				logger.Info(ctx, "发送block数据给消费者了")
				// 5. 发送给消费者 改变数据
				e.blockChan <- block
				// 6. 更新内存状态
				currentHeight = nextHeight
				currentHash = block.Hash
				// e.redisClinet.Del(ctx, lockKey)
			}
		}
	}

}

// Confirmer 独立协程：定期检查 Pending 交易是否成熟
func (e *Engine) Confirmer(ctx context.Context) {
	logger.Info(ctx, "🛡️ Confirmer started")
	// 每 10 秒或者是配置的时间检查一次
	ticker := time.NewTicker(e.config.ConfirmInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 1. 获取当前链上最新高度
			tipHeight, err := e.adapter.GetBlockHeight(ctx)
			if err != nil {
				logger.Error(ctx, "Confirmer get tip failed", zap.Error(err))
				continue
			}

			// 2. 批量更新数据库
			// 把所有 (tip - height >= 6) 的 Pending 记录改成 Confirmed
			count, err := e.repository.ConfirmDeposits(ctx, e.config.Chain, tipHeight, e.config.ConfirmNum)
			if err != nil {
				logger.Error(ctx, "Confirmer update db failed", zap.Error(err))
				continue
			}

			if count > 0 {
				logger.Info(ctx, "✅ 充值到账确认", zap.Int64("count", count), zap.Int64("current_tip", tipHeight))
				// TODO: 这里可以发 Kafka 通知账户系统加钱
			}
		}
	}
}
