package influxsink

import (
	"context"
	"fmt"
	"log"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
)

type Config struct {
	URL    string
	Token  string
	Org    string
	Bucket string

	// 写入优化项
	BatchSize     uint          // 建议从 1000~5000 起步
	FlushInterval time.Duration // 例如 1s
	UseGzip       bool
}

// 你项目里的 Kline/Bar 模型按你实际字段改一下就行
type Bar struct {
	Symbol   string
	Interval string // "1s"/"1m"/...
	Source   string // "coinbase"/"binance"
	StartTs  time.Time

	Open   float64
	High   float64
	Low    float64
	Close  float64
	Vol    float64
	Trades int64
}

type Sink struct {
	client influxdb2.Client
	write  api.WriteAPI
}

func New(cfg Config) *Sink {
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 2000
	}
	if cfg.FlushInterval == 0 {
		cfg.FlushInterval = 1 * time.Second
	}

	opt := influxdb2.DefaultOptions().
		SetBatchSize(cfg.BatchSize).
		SetFlushInterval(uint(cfg.FlushInterval.Milliseconds())).
		SetUseGZip(cfg.UseGzip)

	c := influxdb2.NewClientWithOptions(cfg.URL, cfg.Token, opt)
	w := c.WriteAPI(cfg.Org, cfg.Bucket)

	// 关键：必须消费 Errors()，否则异步写入错误可能导致阻塞/泄露
	//（官方仓库示例也强调 Errors() 的使用）:contentReference[oaicite:11]{index=11}
	go func() {
		for err := range w.Errors() {
			log.Printf("[influx] write error: %v", err)
		}
	}()

	return &Sink{client: c, write: w}
}

func (s *Sink) Close() {
	// Close 会 flush buffer
	s.client.Close()
}

func (s *Sink) WriteBar(b Bar) {
	// measurement：kline
	// tags：symbol/interval/source（标签用于筛选与分组；注意 tag cardinality）
	tags := map[string]string{
		"symbol":   b.Symbol,
		"interval": b.Interval,
		"source":   b.Source,
	}
	fields := map[string]interface{}{
		"o": b.Open,
		"h": b.High,
		"l": b.Low,
		"c": b.Close,
		"v": b.Vol,
		"n": b.Trades,
	}

	p := write.NewPoint("kline", tags, fields, b.StartTs)
	s.write.WritePoint(p)
}

func (s *Sink) Run(ctx context.Context, in <-chan Bar) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var n uint64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case b, ok := <-in:
			if !ok {
				return nil
			}
			s.WriteBar(b)
			n++
		case <-ticker.C:
			// 轻量心跳：你也可以在这里打 metrics
			_ = n
		}
	}
}

func (cfg Config) String() string {
	return fmt.Sprintf("url=%s org=%s bucket=%s batch=%d flush=%s gzip=%v",
		cfg.URL, cfg.Org, cfg.Bucket, cfg.BatchSize, cfg.FlushInterval, cfg.UseGzip)
}
