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

// Handler3Cfg 高吞吐低延迟版配置：
// - PushWorkers: 并发推送 worker 数量。
// - FlushInterval: 首条到来后最迟多久 flush，一般几十/几百毫秒，低吞吐也不等待整秒。
// - BatchSize: 批量大小，提升管道效率。
// - Backoff: 失败后重试退避时间。
// - MaxRetries: 最大重试次数，0 表示无限重试。
// - Stream: Redis stream 名；ID 使用固定 symbol-height-0，天然幂等。
type Handler3Cfg struct {
	Symbol         string
	Stream         string
	MasterInterval time.Duration
	FlushInterval  time.Duration
	BatchSize      int
	Backoff        time.Duration
	PushWorkers    int
	MaxRetries     int // 0 = unlimited
}

type pushJob struct {
	height  uint64
	block   *interfaces.StandardBlock
	attempt int
}

type ackResult struct {
	job pushJob
	err error
}

// Handler3 设计要点：
// - Master 只负责抓块并入队，不等待推送完成，减少串行等待。
// - 多个 push worker 批量写 Redis，使用固定 ID 幂等，失败通过 ack 通道驱动重试。
// - 单独的 ackManager 维护“期望高度”，只有连续成功的块才更新游标，保证不漏。
type Handler3 struct {
	ctx   context.Context
	chair interfaces.Chair
	repo  *Repo
	rs    *redis.Client
	cfg   Handler3Cfg

	jobCh chan pushJob
	ackCh chan ackResult
}

func NewHandler3(ctx context.Context, c interfaces.Chair, db *gorm.DB, rs *redis.Client, cfg Handler3Cfg) *Handler3 {
	repo := NewDb(db)

	if cfg.MasterInterval <= 0 {
		cfg.MasterInterval = time.Second
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 100 * time.Millisecond
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.Backoff <= 0 {
		cfg.Backoff = 200 * time.Millisecond
	}
	if cfg.PushWorkers <= 0 {
		cfg.PushWorkers = 1
	}
	if cfg.Stream == "" {
		cfg.Stream = "stream:scans"
	}
	// 固定 ID（symbol-height-0）要求按 height 单调递增写入 stream；多 worker 会打乱顺序导致写入失败或漏数据。
	if cfg.PushWorkers > 1 {
		log.Printf("Handler3 PushWorkers=%d is not supported with fixed stream IDs; forcing to 1", cfg.PushWorkers)
		cfg.PushWorkers = 1
	}

	h := &Handler3{
		ctx:   ctx,
		chair: c,
		repo:  repo,
		rs:    rs,
		cfg:   cfg,
		// 大 buffer 减少生产侧阻塞；失败时会重试入队。
		jobCh: make(chan pushJob, cfg.BatchSize*4),
		ackCh: make(chan ackResult, cfg.BatchSize*4),
	}

	// ackManager 必须先启动，保证推送结果能推进游标或发起重试。
	go h.ackManager()
	for i := 0; i < cfg.PushWorkers; i++ {
		go h.pushWorker(i)
	}

	return h
}

// Master 不再等待 push 完成，只负责抓块入队；避免原版逐块阻塞 flush。
func (h *Handler3) Master() {
	cursor, err := h.repo.GetLastCursor(h.ctx, h.cfg.Symbol)
	for err != nil {
		log.Printf("get last cursor failed, retrying: %v", err)
		if !h.wait() {
			return
		}
		cursor, err = h.repo.GetLastCursor(h.ctx, h.cfg.Symbol)
	}

	nextEnqueue, err := cursorInt64ToUint64(cursor)
	if err != nil {
		log.Printf("invalid cursor %d: %v", cursor, err)
		return
	}

	// 立即尝试追赶一次。
	if err := h.enqueueBlocks(&nextEnqueue); err != nil && err != context.Canceled {
		log.Printf("initial enqueue failed: %v", err)
	}

	ticker := time.NewTicker(h.cfg.MasterInterval)
	defer ticker.Stop()

	for {
		select {
		case <-h.ctx.Done():
			log.Printf("master stopped: %v", h.ctx.Err())
			return
		case <-ticker.C:
			if err := h.enqueueBlocks(&nextEnqueue); err != nil && err != context.Canceled {
				log.Printf("enqueue failed: %v", err)
			}
		}
	}
}

// enqueueBlocks 将 [cursor, latest) 的块入队，异步推送；队列满时会阻塞形成背压。
func (h *Handler3) enqueueBlocks(next *uint64) error {
	latest, err := h.chair.GetHeight(h.ctx)
	if err != nil {
		return fmt.Errorf("get height: %w", err)
	}

	for *next < latest {
		block, err := h.chair.GetBlockByHeight(h.ctx, *next)
		if err != nil {
			log.Printf("get block %d failed, retry: %v", *next, err)
			if !h.wait() {
				return h.ctx.Err()
			}
			continue
		}

		job := pushJob{height: *next, block: block, attempt: 0}

		select {
		case <-h.ctx.Done():
			return h.ctx.Err()
		case h.jobCh <- job:
			// 仅入队就前移 next；真正推进游标由 ackManager 负责。
			(*next)++
		}
	}
	return nil
}

// pushWorker 批量写 Redis；首条启动短延迟 timer，低吞吐时也会很快 flush。
func (h *Handler3) pushWorker(id int) {
	var (
		batch []pushJob
		timer *time.Timer
	)

	resetTimer := func() {
		if timer != nil {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		timer = nil
	}

	flush := func(reason string) {
		if len(batch) == 0 {
			return
		}

		err := h.push(batch)
		if err != nil {
			log.Printf("worker %d flush(%s) failed: %v", id, reason, err)
		}
		for _, job := range batch {
			h.ackCh <- ackResult{job: job, err: err}
		}

		// 复用底层数组，避免重复分配；如担心容量膨胀，可在此处做 cap 收缩。
		batch = batch[:0]
		resetTimer()
	}

	for {
		select {
		case <-h.ctx.Done():
			flush("ctx-done")
			return
		case job := <-h.jobCh:
			batch = append(batch, job)
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

// ackManager 维护“期望高度”，只有连续成功的块才推进游标；失败则重试入队。
func (h *Handler3) ackManager() {
	cursor, err := h.repo.GetLastCursor(h.ctx, h.cfg.Symbol)
	for err != nil {
		log.Printf("ackManager get cursor failed, retrying: %v", err)
		if !h.wait() {
			return
		}
		cursor, err = h.repo.GetLastCursor(h.ctx, h.cfg.Symbol)
	}

	expected, err := cursorInt64ToUint64(cursor)
	if err != nil {
		log.Printf("ackManager invalid cursor %d: %v", cursor, err)
		return
	}

	pending := make(map[uint64]ackResult)

	for {
		select {
		case <-h.ctx.Done():
			return
		case res := <-h.ackCh:
			// 失败：按退避重试，保护吞吐不被单点错误拖死。
			if res.err != nil {
				if h.cfg.MaxRetries == 0 || res.job.attempt < h.cfg.MaxRetries {
					job := res.job
					job.attempt++
					go func(j pushJob) {
						if !h.wait() {
							return
						}
						select {
						case <-h.ctx.Done():
						case h.jobCh <- j:
						}
					}(job)
				} else {
					log.Printf("job %d exceeded max retries, dropping: %v", res.job.height, res.err)
				}
				continue
			}

			// 成功：存入 pending，按 expected 顺序推进游标。
			if res.job.height < expected {
				// 已推进过的重复 ack，忽略。
				continue
			}
			pending[res.job.height] = res

			for {
				r, ok := pending[expected]
				if !ok || r.err != nil {
					break
				}
				nextCursor, err := cursorUint64ToInt64(expected + 1)
				if err != nil {
					log.Printf("convert cursor %d failed: %v", expected, err)
					return
				}
				if err := h.repo.UpdateCursor(h.ctx, h.cfg.Symbol, nextCursor); err != nil {
					log.Printf("update cursor %d failed, retry: %v", expected, err)
					if !h.wait() {
						return
					}
					// 失败不弹出，保留等待下一次循环重试。
					continue
				}
				delete(pending, expected)
				expected++
			}
		}
	}
}

// push 将一批块以固定 ID 写入 Redis，利用 ID 幂等消除重试重复。
func (h *Handler3) push(batch []pushJob) error {
	pipeline := h.rs.Pipeline()
	ids := make([]string, 0, len(batch))
	cmds := make([]*redis.StringCmd, 0, len(batch))
	for _, job := range batch {
		jsonBytes, err := json.Marshal(job.block)
		if err != nil {
			return fmt.Errorf("marshal block %d: %w", job.height, err)
		}
		id := fmt.Sprintf("%s-%d-0", h.cfg.Symbol, job.height)
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

// wait 统一的退避逻辑；尊重 ctx。
func (h *Handler3) wait() bool {
	select {
	case <-h.ctx.Done():
		return false
	case <-time.After(h.cfg.Backoff):
		return true
	}
}
