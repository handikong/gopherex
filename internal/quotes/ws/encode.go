package ws

import (
	"encoding/json"
	"fmt"
	"time"

	"gopherex.com/internal/quotes/kline"
)

// 你贴的结构（保持一致）
// type Bar struct {
// 	Symbol   string
// 	Interval time.Duration
// 	StartMs int64
// 	EndMs   int64
// 	Open  int64
// 	High  int64
// 	Low   int64
// 	Close int64
// 	Volume int64
// 	Count  int64
// }

func EncodeEvent(ev kline.Bar) (topic string, payload []byte, err error) {
	return EncodeBar(ev)
}

// EncodeBar: kline:<tf>:<symbol>
func EncodeBar(b kline.Bar) (string, []byte, error) {
	tf := durToTF(b.Interval)
	b.TF = tf
	topic := fmt.Sprintf("kline:%s:%s", tf, b.Symbol)
	buf, err := json.Marshal(b)
	if err != nil {
		return "", nil, err
	}
	return topic, buf, nil
}

// durToTF 把 time.Duration 归一化成你 topic 里用的 timeframe 字符串
// 你现在聚合的周期是 1s/1m/1h/1d（以及可能更多），这里做成通用格式。
func durToTF(d time.Duration) string {
	switch d {
	case time.Second:
		return "1s"
	case time.Minute:
		return "1m"
	case time.Hour:
		return "1h"
	case 24 * time.Hour:
		return "1d"
	}

	// 通用：优先整秒，否则整毫秒
	if d > 0 && d%time.Second == 0 {
		return fmt.Sprintf("%ds", int64(d/time.Second))
	}
	if d > 0 && d%time.Millisecond == 0 {
		return fmt.Sprintf("%dms", int64(d/time.Millisecond))
	}

	// 实在不整，就用 Duration 字符串（可能是 1m30s 这种）
	if d > 0 {
		return d.String()
	}
	return ""
}
