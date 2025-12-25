package ws

import (
	"encoding/json"
	"strings"
	"time"

	"gopherex.com/internal/quotes/kline"
)

func TopicForBar(b kline.Bar) string {
	tf := intervalKey(b.Interval)    // 统一 interval
	sym := normalizeSymbol(b.Symbol) // 统一 symbol（大写、-）
	return "kline:" + tf + ":" + sym
}

func intervalKey(d time.Duration) string {
	switch d {
	case time.Second:
		return "1s"
	case time.Minute:
		return "1m"
	case time.Hour:
		return "1h"
	case 24 * time.Hour:
		return "1d"
	case 7 * 24 * time.Hour:
		return "1w"
	default:
		// v0：不支持就别发，避免前端订不到
		return d.String()
	}
}

func normalizeSymbol(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	// v0：保证用 "-" 分隔，别出现 "_" 或 "/" 等
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, "/", "-")
	return s
}

func normalizeInterval(d time.Duration) string {
	switch d {
	case time.Second:
		return "1s"
	case time.Minute:
		return "1m"
	case time.Hour:
		return "1h"
	case 24 * time.Hour:
		return "1d"
	default:
		return d.String()
	}
}

func ToDTO(b kline.Bar) BarDTO {
	return BarDTO{
		Symbol:   b.Symbol,
		Interval: normalizeInterval(b.Interval),
		StartMs:  b.StartMs,
		EndMs:    b.EndMs,
		Open:     kline.FormatFixed(b.Open),
		High:     kline.FormatFixed(b.High),
		Low:      kline.FormatFixed(b.Low),
		Close:    kline.FormatFixed(b.Close),
		Volume:   kline.FormatFixed(b.Volume),
		Count:    b.Count,
	}
}

// Bridge：把聚合器输出的 bar 转成 ws payload 并 publish 到 hub
func Bridge(h *Hub, ev kline.KlineEvent) {
	topic := TopicForBar(ev.Bar)
	msg := ServerMsg{
		Type:  "kline",
		Topic: topic,
		Bar:   ToDTO(ev.Bar),
	}
	b, _ := json.Marshal(msg)
	h.Publish(topic, b)
}
