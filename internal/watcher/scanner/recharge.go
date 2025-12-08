package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"gopherex.com/internal/watcher/domain"
	"gopherex.com/internal/watcher/repo"
	"gopherex.com/internal/watcher/scanner/strage"
	"gopherex.com/internal/watcher/service"
	"gopherex.com/pkg/logger"
	"gopherex.com/pkg/safe"
)

type Engine struct {
	config               *domain.RechargeConfig
	rds                  *redis.Client
	adapter              domain.ChainAdapter
	strageWatch          domain.ScanStrageWatcher
	scanerService        *service.ScanService
	chainTransferRpcChan chan *domain.ChainTransfer // 传递RPC的chan
	rdsAckChan           chan string                // 传递ACK确认的chan
	StreamRechargeKey    string
	GroupNameKey         string
}

func NewRecharge(config *domain.RechargeConfig, rds *redis.Client,
	adapter domain.ChainAdapter, db *gorm.DB) *Engine {
	var scanRepo = repo.New(db)
	scanServie := service.NewScanService(scanRepo, rds)
	strageWatch := strage.NewStrategy(config, adapter, scanServie, rds)
	skey := fmt.Sprintf("%s_%s_%s", domain.StreamRecharegeKey, config.Chain, config.ScanMode)
	gkey := fmt.Sprintf("%s_%s_%s", domain.GroupName, config.Chain, config.ScanMode)
	return &Engine{
		config:               config,
		rds:                  rds,
		adapter:              adapter,
		strageWatch:          strageWatch,
		scanerService:        scanServie,
		chainTransferRpcChan: make(chan *domain.ChainTransfer, 100),
		rdsAckChan:           make(chan string, 100),
		StreamRechargeKey:    skey,
		GroupNameKey:         gkey,
	}
}

// startWorker 启动一个 worker 协程的辅助函数
func (r *Engine) startWorker(ctx context.Context, wg *sync.WaitGroup, workerIdx int, fn func(context.Context, int)) {
	wg.Add(1)
	safe.GoCtx(ctx, func(c context.Context) {
		defer wg.Done()
		fn(c, workerIdx)
	})
}

// 启动项目
func (r *Engine) Start(ctx context.Context) {
	var wg sync.WaitGroup

	// 启动 Master 协程
	wg.Add(1)
	logger.Info(ctx, "启动 Master 协程")
	safe.GoCtx(ctx, func(ctx context.Context) {
		defer wg.Done()
		r.master(ctx)
	})

	// 启动 Worker 协程
	logger.Info(ctx, "启动 Worker 协程", zap.Int("worker_count", int(r.config.ConsumerCount)))
	for i := 0; i < int(r.config.ConsumerCount); i++ {
		r.startWorker(ctx, &wg, i, r.worker)
	}

	// 启动 RPC Handler 协程
	logger.Info(ctx, "启动 RPC Handler 协程", zap.Int("handler_count", int(r.config.ConsumerCount)))
	for i := 0; i < int(r.config.ConsumerCount); i++ {
		r.startWorker(ctx, &wg, i, r.rpcHandler)
	}

	// 启动 Redis ACK 协程
	logger.Info(ctx, "启动 Redis ACK 协程", zap.Int("ack_count", 2))
	for i := 0; i < 2; i++ {
		r.startWorker(ctx, &wg, i, r.rdbACK)
	}

	logger.Info(ctx, "Engine started. Waiting for signal...")
	<-ctx.Done()
	logger.Info(ctx, "收到停止信号，正在等待所有任务完成 (Graceful Shutdown)...")
	wg.Wait()
	logger.Info(ctx, "所有任务已停止，Engine 安全退出。")
}

// master 按照设定的间隔定时扫描区块
func (r *Engine) master(ctx context.Context) {
	ticker := time.NewTicker(r.config.ConfirmInterval)
	defer ticker.Stop()

	// 获取区块的高度
	lastHeight, _, err := r.scanerService.GetLastCursor(ctx, r.config.Chain, r.config.ScanMode)
	if err != nil {
		logger.Error(ctx, "Master 启动失败：获取初始区块高度失败", zap.Error(err), zap.String("chain", r.config.Chain))
		panic(err)
	}
	currentHeight := lastHeight
	logger.Info(ctx, "Master 启动成功", zap.Int64("last_height", lastHeight), zap.String("chain", r.config.Chain), zap.Duration("scan_interval", r.config.ConfirmInterval))

	for {
		select {
		case <-ctx.Done():
			logger.Info(ctx, "Master 收到停止信号，退出扫描循环")
			return
		case <-ticker.C:
			// 分布式枷锁 如果抢到锁了就执行
			// redisLock := xredis.NewRedisLockMaster(r.rds)
			// 获取链上的高度
			blockHeight, err := r.adapter.GetBlockHeight(ctx)
			if err != nil {
				logger.Error(ctx, "获取链上高度出错", zap.Error(err), zap.String("chain", r.config.Chain))
				continue
			}

			scanHeight, err := r.scaner(ctx, currentHeight, blockHeight)
			if err != nil {
				logger.Error(ctx, "区块扫描报错", zap.Error(err), zap.Int64("current_height", currentHeight), zap.Int64("block_height", blockHeight))
				continue
			}
			currentHeight = scanHeight
		}
	}
}

// 扫描区块的方法
func (r *Engine) scaner(ctx context.Context, currentHeight int64, blockHeight int64) (int64, error) {
	// 只要小于就扫描
	from := currentHeight + 1
	// 获取步长
	step := r.strageWatch.GetSkip()
	// 记录最后成功处理的高度，初始化为当前高度
	lastSuccessHeight := currentHeight
	// logger.Debug(ctx, "开始扫描区块范围", zap.Int64("from", from), zap.Int64("to", blockHeight), zap.Int64("step", step))
	for from < blockHeight {
		// 使用starge处理
		to := from + step - 1
		if to > blockHeight {
			to = blockHeight
		}

		actualProcessedHeight, transfers, err := r.strageWatch.GetFetchAndPush(ctx, from, to)
		if err != nil {
			logger.Error(ctx, "GetFetchAndPush", zap.Error(err), zap.Int64("from", from), zap.Int64("to", to))
			return lastSuccessHeight, err
		}
		err = r.pushRedis(ctx, transfers)
		if err != nil {
			logger.Error(ctx, "pushRedis", zap.Error(err), zap.Int64("from", from), zap.Int64("to", to))
			return lastSuccessHeight, err
		}
		// logger.Debug(ctx, "区块数据获取并推送成功", zap.Int64("processed_height", actualProcessedHeight), zap.Int64("from", from), zap.Int64("to", to))
		err = r.scanerService.UpdateCursor(ctx, r.config.Chain, actualProcessedHeight, r.config.ScanMode)
		if err != nil {
			// 数据库更新失败，这很危险，为了数据一致性，建议中止，下次重试
			logger.Error(ctx, "数据库游标更新失败", zap.Error(err), zap.Int64("processed_height", actualProcessedHeight), zap.String("chain", r.config.Chain))
			return lastSuccessHeight, err
		}
		logger.Debug(ctx, "游标更新成功", zap.Int64("new_cursor", actualProcessedHeight), zap.String("chain", r.config.Chain))
		// 更新成功指针
		lastSuccessHeight = actualProcessedHeight
		from = actualProcessedHeight + 1
	}
	// logger.Info(ctx, "区块扫描完成", zap.Int64("final_height", lastSuccessHeight), zap.Int64("start_height", currentHeight), zap.Int64("target_height", blockHeight))
	return lastSuccessHeight, nil
}

func (r *Engine) worker(ctx context.Context, workNum int) {
	consumerName := fmt.Sprintf("consumer-%d", workNum)
	logger.Info(ctx, "Worker 启动", zap.Int("worker_num", workNum), zap.String("consumer", consumerName))
	_ = r.rds.XGroupCreateMkStream(ctx, r.StreamRechargeKey, r.GroupNameKey, "0").Err()

	for {
		// 检查 context 是否已取消
		if ctx.Err() != nil {
			logger.Info(ctx, "Worker 收到停止信号，退出", zap.Int("worker_num", workNum))
			return
		}

		// 从 Redis Stream 读取消息
		streams, err := r.rds.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    r.GroupNameKey,
			Consumer: consumerName,
			Streams:  []string{r.StreamRechargeKey, ">"},
			Count:    10,              // 一次批处理 10 条
			Block:    2 * time.Second, // 阻塞等待 2 秒，如果没数据就返回空
		}).Result()

		if err != nil {
			if err == redis.Nil {
				// 没有数据，继续循环
				continue
			}
			logger.Error(ctx, "XReadGroup 错误", zap.Error(err), zap.Int("worker_num", workNum), zap.String("consumer", consumerName))
			time.Sleep(time.Second) // 出错休眠一下，防止日志刷屏
			continue
		}

		// 统计读取到的消息数量
		totalMessages := 0
		for _, stream := range streams {
			totalMessages += len(stream.Messages)
		}
		if totalMessages > 0 {
			logger.Debug(ctx, "Worker 读取到消息", zap.Int("worker_num", workNum), zap.Int("message_count", totalMessages))
		}

		r.streamHandler(ctx, streams)
	}
}

// 处理流消息
func (r *Engine) streamHandler(ctx context.Context, streams []redis.XStream) {
	for _, stream := range streams {
		for _, msg := range stream.Messages {
			r.processMessage(ctx, msg)
		}
	}
}

func (r *Engine) processMessage(ctx context.Context, msg redis.XMessage) {
	// 解析数据
	var transferData domain.ChainTransfer
	dataRaw, ok := msg.Values["data"]
	if !ok {
		logger.Error(ctx, "消息中缺少data字段，跳过", zap.String("msg_id", msg.ID))
		return
	}
	dataStr, ok := dataRaw.(string)
	if !ok {
		logger.Error(ctx, "data字段类型断言失败，跳过", zap.Any("data", dataRaw), zap.String("msg_id", msg.ID))
		return
	}
	err := json.Unmarshal([]byte(dataStr), &transferData)
	if err != nil {
		logger.Error(ctx, "JSON解析失败，跳过", zap.Error(err), zap.String("msg_id", msg.ID))
		return
	}
	transferData.MsgId = msg.ID
	logger.Info(ctx, "消息解析成功，发送到 RPC 处理通道", zap.String("msg_id", msg.ID), zap.String("tx_hash", transferData.TxHash))

	// 使用非阻塞发送，避免 channel 满时阻塞整个 worker
	// 如果 channel 满了，消息会留在 Redis Stream 中，下次重试处理
	select {
	case <-ctx.Done():
		logger.Warn(ctx, "Context 已取消，跳过消息发送", zap.String("msg_id", msg.ID))
		return
	case r.chainTransferRpcChan <- &transferData:
		logger.Debug(ctx, "消息已发送到 RPC 处理通道", zap.String("msg_id", msg.ID))
	default:
		// Channel 已满，记录警告但不阻塞
		// 消息会留在 Redis Stream 中，等待下次 XReadGroup 时重新处理
		logger.Warn(ctx, "RPC 处理通道已满，消息将留在 Stream 中等待下次处理",
			zap.String("msg_id", msg.ID),
			zap.String("tx_hash", transferData.TxHash))
		return
	}

}

func (r *Engine) insertRpc(ctx context.Context, block *domain.ChainTransfer) error {
	// 模拟数据库事务
	// tx := r.db.Begin()
	// ... 插入充值记录 ...
	// ... 幂等性检查 (TxHash 是否已存在) ...
	// return tx.Commit().Error
	return nil
}

func (r *Engine) rpcHandler(ctx context.Context, workNum int) {
	logger.Info(ctx, "RPC Handler 启动", zap.Int("handler_num", workNum))
	for {
		select {
		case <-ctx.Done():
			logger.Info(ctx, "RPC Handler 收到停止信号，退出", zap.Int("handler_num", workNum))
			return
		case data := <-r.chainTransferRpcChan:
			logger.Info(ctx, "开始处理 RPC 请求", zap.String("msg_id", data.MsgId), zap.String("tx_hash", data.TxHash), zap.Int("handler_num", workNum))
			err := r.insertRpc(ctx, data)
			if err != nil {
				logger.Error(ctx, "RPC 处理失败", zap.Error(err), zap.String("msg_id", data.MsgId), zap.String("tx_hash", data.TxHash), zap.Int("handler_num", workNum))
				continue
			}

			logger.Info(ctx, "RPC 处理成功，发送 ACK", zap.String("msg_id", data.MsgId), zap.String("tx_hash", data.TxHash), zap.Int("handler_num", workNum))
			// 使用非阻塞发送 ACK，避免 channel 满时阻塞 rpcHandler
			// 如果 ACK 通道满了，消息会留在 Stream 中，下次重试时会重新处理并再次尝试 ACK
			select {
			case <-ctx.Done():
				logger.Warn(ctx, "Context 已取消，跳过 ACK 发送", zap.String("msg_id", data.MsgId))
				return
			case r.rdsAckChan <- data.MsgId:
				logger.Debug(ctx, "ACK 消息已发送到确认通道", zap.String("msg_id", data.MsgId))
			default:
				// ACK 通道已满，记录警告但不阻塞
				// 消息会留在 Redis Stream 中，下次 XReadGroup 时会重新处理并再次尝试 ACK
				logger.Warn(ctx, "ACK 确认通道已满，消息将留在 Stream 中等待下次处理",
					zap.String("msg_id", data.MsgId),
					zap.String("tx_hash", data.TxHash))
			}
		}
	}
}

func (r *Engine) rdbACK(ctx context.Context, workNum int) {
	logger.Info(ctx, "Redis ACK Handler 启动", zap.Int("ack_handler_num", workNum))
	for {
		select {
		case <-ctx.Done():
			logger.Info(ctx, "Redis ACK Handler 收到停止信号，退出", zap.Int("ack_handler_num", workNum))
			return
		case streamID := <-r.rdsAckChan:
			err := r.rds.XAck(ctx, r.StreamRechargeKey, r.GroupNameKey, streamID).Err()
			if err != nil {
				logger.Error(ctx, "Redis ACK 失败", zap.Error(err), zap.String("msg_id", streamID), zap.Int("ack_handler_num", workNum))
			} else {
				logger.Debug(ctx, "Redis ACK 成功", zap.String("msg_id", streamID), zap.Int("ack_handler_num", workNum))
			}
		}
	}
}
func (e *Engine) pushRedis(ctx context.Context, transfers []*domain.ChainTransfer) error {
	if len(transfers) == 0 {
		return nil
	}
	pipe := e.rds.Pipeline()
	for _, chainTransfer := range transfers {
		jsonBytes, err := json.Marshal(chainTransfer)
		if err != nil {
			logger.Error(ctx, "序列化 ChainTransfer 失败", zap.Error(err), zap.String("tx_hash", chainTransfer.TxHash))
			return fmt.Errorf("marshal chain transfer: %w", err)
		}
		pipe.XAdd(ctx, &redis.XAddArgs{
			Stream: e.StreamRechargeKey,
			Values: map[string]interface{}{
				"data": string(jsonBytes),
				"type": "native",
			},
		})
	}
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("pipeline exec: %w", err)
	}
	return nil
}
