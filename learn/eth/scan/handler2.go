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

// Handler2Cfg 提供更清晰的“幂等 + 低延迟 flush”版本配置。
// 关键点：
// - StreamID 固定为 symbol+height，重复写会被 Redis 拒绝，解决“推送成功、游标失败”导致的重复问题。
// - 首条数据启动一次性定时器，未满也会尽快 flush，避免低吞吐场景等完整 FlushInterval。
// - 继续保留退避与日志，避免原实现静默退出或热循环。
type Handler2Cfg struct {
	Symbol         string        // 链符号，用于游标与 Redis ID
	Stream         string        // Redis stream 名称
	MasterInterval time.Duration // 轮询链高度的间隔
	FlushInterval  time.Duration // 批量 flush 的最长等待时间
	BatchSize      int           // 每批最大推送条数
	Backoff        time.Duration // 失败后的退避
}

type pushReq2 struct {
	height uint64
	block  *interfaces.StandardBlock
	result chan error
}

// Handler2：增加幂等写（固定 ID），首条延迟 flush，注重可读性与注释。
type Handler2 struct {
	ctx   context.Context
	chair interfaces.Chair
	repo  *Repo
	rs    *redis.Client
	cfg   Handler2Cfg

	reqCh chan pushReq2
}

func NewHandler2(ctx context.Context, c interfaces.Chair, db *gorm.DB, rs *redis.Client, cfg Handler2Cfg) *Handler2 {
	repo := NewDb(db)

	// 默认值，避免 0 导致 ticker/timer 失效。
	if cfg.MasterInterval <= 0 {
		cfg.MasterInterval = time.Second
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 200 * time.Millisecond // 默认更短，降低等待
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	}
	if cfg.Backoff <= 0 {
		cfg.Backoff = time.Second
	}
	if cfg.Stream == "" {
		cfg.Stream = "stream:scans"
	}

	h := &Handler2{
		ctx:   ctx,
		chair: c,
		repo:  repo,
		rs:    rs,
		cfg:   cfg,
		reqCh: make(chan pushReq2, cfg.BatchSize*2),
	}
	go h.runPushLoop()
	return h
}

// Master 负责追块：立即执行一次，再按间隔循环；错误会退避重试。
func (h *Handler2) Master() {
	cursor, err := h.repo.GetLastCursor(h.ctx, h.cfg.Symbol)
	for err != nil {
		log.Printf("get last cursor failed, retrying: %v", err)
		if !h.wait() {
			return
		}
		cursor, err = h.repo.GetLastCursor(h.ctx, h.cfg.Symbol)
	}

	uCursor, err := cursorInt64ToUint64(cursor)
	if err != nil {
		log.Printf("invalid cursor %d: %v", cursor, err)
		return
	}

	if err := h.catchUp(&uCursor); err != nil && err != context.Canceled {
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
			if err := h.catchUp(&uCursor); err != nil && err != context.Canceled {
				log.Printf("catch up failed: %v", err)
			}
		}
	}
}

// catchUp 顺序抓取并推送区块：推送成功后再更新游标，保证不漏。
func (h *Handler2) catchUp(cursor *uint64) error {
	height, err := h.chair.GetHeight(h.ctx)
	if err != nil {
		return fmt.Errorf("get height: %w", err)
	}

	for *cursor < height {
		block, err := h.chair.GetBlockByHeight(h.ctx, uint64(*cursor))
		if err != nil {
			log.Printf("get block %d failed, retry: %v", *cursor, err)
			if !h.wait() {
				return h.ctx.Err()
			}
			continue
		}

		resultCh := make(chan error, 1)
		req := pushReq2{
			height: uint64(*cursor),
			block:  block,
			result: resultCh,
		}

		// 发送时监听 ctx，避免缓冲满时永久阻塞。
		select {
		case <-h.ctx.Done():
			return h.ctx.Err()
		case h.reqCh <- req:
		}

		// 等待推送完成结果，失败退避重试；避免游标先推进。
		if err := <-resultCh; err != nil {
			log.Printf("push block %d failed, retry: %v", *cursor, err)
			if !h.wait() {
				return h.ctx.Err()
			}
			continue
		}

		nextCursor, err := cursorUint64ToInt64(*cursor + 1)
		if err != nil {
			return fmt.Errorf("cursor convert: %w", err)
		}
		if err := h.repo.UpdateCursor(h.ctx, h.cfg.Symbol, nextCursor); err != nil {
			log.Printf("update cursor %d failed, retry: %v", *cursor, err)
			if !h.wait() {
				return h.ctx.Err()
			}
			continue
		}

		*cursor++
	}
	return nil
}

// runPushLoop 聚合批量写入 Redis，首条数据启动短延迟 timer，避免低吞吐时等待完整 FlushInterval。
func (h *Handler2) runPushLoop() {
	var (
		batch []pushReq2
		timer *time.Timer
	)

	resetTimer := func() {
		if timer != nil {
			if !timer.Stop() {
				<-timer.C
			}
		}
		timer = nil
	}

	flush := func(reason string) {
		if len(batch) == 0 {
			return
		}

		if err := h.push(batch); err != nil {
			log.Printf("flush(%s) failed: %v", reason, err)
			for _, req := range batch {
				req.result <- err
			}
		} else {
			for _, req := range batch {
				req.result <- nil
			}
		}

		// 清空 batch，修复原版未清空导致的重复发送/内存增长问题。
		batch = batch[:0]
		resetTimer()
	}

	for {
		select {
		case <-h.ctx.Done():
			// 尽量 flush 剩余；若失败/取消，向等待方返回错误，避免 goroutine 泄漏。
			flush("ctx-done")
			return
		case req := <-h.reqCh:
			batch = append(batch, req)
			// 首条数据启动一次性 timer，未满也会在 FlushInterval 内 flush，避免低吞吐长等待。
			if len(batch) == 1 && timer == nil {
				timer = time.NewTimer(h.cfg.FlushInterval)
			}
			if len(batch) >= h.cfg.BatchSize {
				flush("max-batch")
			}
		case <-func() <-chan time.Time {
			if timer == nil {
				return nil
			}
			return timer.C
		}():
			flush("timer")
		}
	}
}

// push 将一批区块以固定 ID 写入 Redis Stream，利用 ID 幂等避免重复消费。
func (h *Handler2) push(batch []pushReq2) error {
	pipeline := h.rs.Pipeline()
	ids := make([]string, 0, len(batch))
	cmds := make([]*redis.StringCmd, 0, len(batch))
	for _, req := range batch {
		jsonBytes, err := json.Marshal(req.block)
		if err != nil {
			return fmt.Errorf("marshal block %d: %w", req.height, err)
		}

		// 固定 ID: symbol-height-0，重复写会被 Redis 拒绝/覆盖，避免游标失败导致的重复消费。
		id := fmt.Sprintf("%s-%d-0", h.cfg.Symbol, req.height)
		cmd := pipeline.XAdd(h.ctx, &redis.XAddArgs{
			Stream: h.cfg.Stream,
			ID:     id,
			Values: map[string]any{
				"data": string(jsonBytes),
				"type": "native",
			},
		})
		ids = append(ids, id)
		cmds = append(cmds, cmd)
	}

	if _, err := pipeline.Exec(h.ctx); err == nil {
		return nil
	}

	for i, cmd := range cmds {
		cmdErr := cmd.Err()
		if cmdErr == nil {
			continue
		}
		if isRedisXAddIDTooSmallErr(cmdErr) {
			exists, existsErr := xaddIDExists(h.ctx, h.rs, h.cfg.Stream, ids[i])
			if existsErr != nil {
				return fmt.Errorf("xrange %s %s: %w", h.cfg.Stream, ids[i], existsErr)
			}
			if exists {
				continue
			}
		}
		return fmt.Errorf("xadd %s failed: %w", ids[i], cmdErr)
	}
	return nil
}

// wait 在错误后退避；尊重 ctx。
func (h *Handler2) wait() bool {
	select {
	case <-h.ctx.Done():
		return false
	case <-time.After(h.cfg.Backoff):
		return true
	}
}
