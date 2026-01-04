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

type HandlerCfg struct {
	Symbol string // 区块的名称
	//workerNum      uint8         // worker的数量
	//workerInterval time.Duration //  worker简短多久扫一次
	MasterInterval time.Duration
}
type Handler struct {
	chair    interfaces.Chair
	ctx      context.Context
	repo     *Repo
	rs       *redis.Client
	cfg      *HandlerCfg
	dataChan chan *interfaces.StandardBlock
}

func NewHandler(ctx context.Context, c interfaces.Chair, db *gorm.DB, rs *redis.Client, cfg *HandlerCfg) *Handler {
	repo := NewDb(db)
	dataChan := make(chan *interfaces.StandardBlock, 100)

	h := &Handler{
		ctx:      ctx,
		chair:    c,
		repo:     repo,
		rs:       rs,
		cfg:      cfg,
		dataChan: dataChan,
	}
	go h.pushRedis()
	return h
}

func (h *Handler) Master() {
	// 先扫描数据
	ticker := time.NewTicker(h.cfg.MasterInterval)
	cursor, err := h.repo.GetLastCursor(h.ctx, h.cfg.Symbol)
	if err != nil {
		log.Fatalf("get last cursor: %v", err)
	}
	defer ticker.Stop()
	for {
		select {
		case <-h.ctx.Done():
			log.Printf("master done ")
			return
		case <-ticker.C:
			// 获取当前区块的高度
			blockHeight, err := h.chair.GetHeight(h.ctx)
			if err != nil {
				return
			}
			for uint64(cursor) < blockHeight {
				err := h.GetBlock(uint64(cursor))
				if err != nil {
					continue
				}

				err = h.repo.UpdateCursor(h.ctx, h.cfg.Symbol, cursor+1)
				if err != nil {
					log.Printf("update cursor: %v", err)
					continue
				}
				cursor++
			}
		}
	}
}

func (h *Handler) GetBlock(currentHeight uint64) error {
	block, err := h.chair.GetBlockByHeight(h.ctx, currentHeight)

	if err != nil {
		return err
	}
	// 阻塞发送  是否有问题
	select {
	case h.dataChan <- block:
	}
	return nil
}

func (h *Handler) pushRedis() {
	ticker := time.NewTicker(time.Second * 2)
	defer ticker.Stop()
	var blocks = []*interfaces.StandardBlock{}
	for {
		select {
		case <-h.ctx.Done():
			fmt.Printf("push redis stop")
			return
		case <-ticker.C:
			if len(blocks) > 0 {
				h.push(blocks)
			}
		case data := <-h.dataChan:
			blocks = append(blocks, data)
			if len(blocks) >= 50 {
				h.push(blocks)
			}
		}
	}
}
func (h *Handler) push(blocks []*interfaces.StandardBlock) error {
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
	_, err := pipeline.Exec(h.ctx)
	if err != nil {
		return fmt.Errorf("pipeline exec: %w", err)
	}
	return nil
}
