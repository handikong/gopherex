package scan

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"gopherex.com/learn/eth/scan/interfaces"
	"gorm.io/gorm"
)

// Handler1Cfg 是优化版的配置；增加了批量与退避参数。
type Handler1Cfg struct {
	Symbol         string        // 区块链符号
	MasterInterval time.Duration // 主循环轮询高度的间隔
	FlushInterval  time.Duration // 推送到 Redis 的最长等待时间
	BatchSize      int           // 每批次推送的最大块数量
	Backoff        time.Duration // 拉取/推送失败后的退避时间
}

// pushRequest 用于等待 push 完成后再更新游标，避免“先更新游标后推送”导致的丢数据。
type pushRequest struct {
	block  *interfaces.StandardBlock
	result chan error
}

// Handler1 是改进版，修复了原实现的重复发送、无休止阻塞、缺少错误处理等问题。
type Handler1 struct {
	chair interfaces.Chair
	ctx   context.Context
	repo  *Repo
	rs    *redis.Client
	cfg   Handler1Cfg

	reqCh chan pushRequest
}

func NewHandler1(ctx context.Context, c interfaces.Chair, db *gorm.DB, rs *redis.Client, cfg Handler1Cfg) *Handler1 {
	repo := NewDb(db)

	// 安全默认值：避免 0 导致 ticker 不工作或 batch 永不触发。
	if cfg.MasterInterval <= 0 {
		cfg.MasterInterval = time.Second
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 2 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	}
	if cfg.Backoff <= 0 {
		cfg.Backoff = time.Second
	}

	h := &Handler1{
		ctx:   ctx,
		chair: c,
		repo:  repo,
		rs:    rs,
		cfg:   cfg,
		// buffer 翻倍，尽量减少 master 端阻塞；发送时会监听 ctx，避免原实现的“满 buffer 后永久阻塞”。
		reqCh: make(chan pushRequest, cfg.BatchSize*2),
	}
	go h.runPushLoop()
	return h
}

// Master 立即执行一次扫描，再按间隔轮询；错误会日志+退避而不是静默退出或 Fatal。
func (h *Handler1) Master() {
	cursor, err := h.repo.GetLastCursor(h.ctx, h.cfg.Symbol)
	for err != nil {
		log.Printf("get last cursor failed, retrying: %v", err)
		if !h.wait() {
			return
		}
		cursor, err = h.repo.GetLastCursor(h.ctx, h.cfg.Symbol)
	}
	tCursor := uint64(cursor)
	// 启动阶段不等待 ticker，避免原实现启动后空等一轮。
	if err := h.catchUp(&tCursor); err != nil && err != context.Canceled {
		log.Printf("initial catch up failed: %v", err)
	}
	ticker := time.NewTicker(h.cfg.MasterInterval)
	defer ticker.Stop()
	for {
		select {
		case <-h.ctx.Done():
			log.Printf("master stopped: %v", h.ctx.Err())
			return
		case <-ticker.C:
			if err := h.catchUp(&tCursor); err != nil && err != context.Canceled {
				log.Printf("catch up failed: %v", err)
			}
		}
	}
}

// catchUp 从当前游标追到最新高度；遇到错误会退避而非热循环。
func (h *Handler1) catchUp(cursor *uint64) error {
	blockHeight, err := h.chair.GetHeight(h.ctx)
	if err != nil {
		return fmt.Errorf("get height: %w", err)
	}

	for *cursor < blockHeight {
		block, err := h.chair.GetBlockByHeight(h.ctx, *cursor)
		if err != nil {
			log.Printf("get block %d failed, will retry: %v", *cursor, err)
			if !h.wait() {
				return h.ctx.Err()
			}
			continue
		}

		resultCh := make(chan error, 1)
		req := pushRequest{block: block, result: resultCh}

		// 发送时监听 ctx，避免原实现只写 channel 不看取消信号导致的永久阻塞。
		select {
		case <-h.ctx.Done():
			return h.ctx.Err()
		case h.reqCh <- req:
		}

		// 等待 push 完成后再更新游标，避免“游标先写，数据未落地”导致漏扫。
		if err := <-resultCh; err != nil {
			log.Printf("push block %d failed, will retry: %v", *cursor, err)
			if !h.wait() {
				return h.ctx.Err()
			}
			continue
		}
		heightCursor := int64(*cursor)
		if err := h.repo.UpdateCursor(h.ctx, h.cfg.Symbol, heightCursor+1); err != nil {
			log.Printf("update cursor at %d failed, will retry: %v", *cursor, err)
			if !h.wait() {
				return h.ctx.Err()
			}
			continue
		}
		*cursor++
	}
	return nil
}

// runPushLoop 聚合批量写 Redis；每次 push 都会清空 batch，避免原实现的重复发送。
func (h *Handler1) runPushLoop() {
	ticker := time.NewTicker(h.cfg.FlushInterval)
	defer ticker.Stop()

	batch := make([]pushRequest, 0, h.cfg.BatchSize)

	flush := func(reason string) {
		if len(batch) == 0 {
			return
		}

		blocks := make([]*interfaces.StandardBlock, len(batch))
		for i, req := range batch {
			blocks[i] = req.block
		}
		err := h.push(blocks)
		// 不论成功与否都要给等待方返回结果，避免阻塞。
		for _, req := range batch {
			req.result <- err
		}
		// 必须清空，修复原实现 push 后未清空导致的重复发送与内存增长。
		batch = batch[:0]
	}

	for {
		select {
		case <-h.ctx.Done():
			// 给仍在等待的请求返回上下文取消错误。
			for _, req := range batch {
				req.result <- h.ctx.Err()
			}
			return
		case req := <-h.reqCh:
			batch = append(batch, req)
			if len(batch) >= h.cfg.BatchSize {
				flush("max-batch")
			}
		case <-ticker.C:
			flush("timer")
		}
	}
}

// push 单次将一批 block 写入 Redis stream，附带类型标记。
func (h *Handler1) push(blocks []*interfaces.StandardBlock) error {
	pipeline := h.rs.Pipeline()
	for _, chainTransfer := range blocks {
		jsonBytes, err := json.Marshal(chainTransfer)
		if err != nil {
			return fmt.Errorf("marshal chain transfer: %w", err)
		}
		pipeline.XAdd(h.ctx, &redis.XAddArgs{
			Stream: "stream:scans",
			Values: map[string]interface{}{
				"data": string(jsonBytes),
				"type": "native",
			},
		})
	}
	if _, err := pipeline.Exec(h.ctx); err != nil {
		return fmt.Errorf("pipeline exec: %w", err)
	}
	return nil
}

// wait 在错误后做一次退避；尊重 ctx，避免死等。
func (h *Handler1) wait() bool {
	select {
	case <-h.ctx.Done():
		return false
	case <-time.After(h.cfg.Backoff):
		return true
	}
}
