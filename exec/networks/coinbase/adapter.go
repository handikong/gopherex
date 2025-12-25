package coinbase

import (
	"encoding/json"
	"time"

	"gopherex.com/exec/networks/md"
)

const scale = int64(1e8)

type Adapter struct{}

func (a Adapter) Name() string { return "coinbase" }
func (a Adapter) URL() string  { return "wss://advanced-trade-ws.coinbase.com" }

func (a Adapter) SubscribeMessages(products []string) [][]byte {
	type sub struct {
		Type       string   `json:"type"`
		Channel    string   `json:"channel"`
		ProductIDs []string `json:"product_ids,omitempty"`
	}
	m1, _ := json.Marshal(sub{Type: "subscribe", Channel: "heartbeats"})
	mTR, _ := json.Marshal(sub{Type: "subscribe", Channel: "market_trades", ProductIDs: products})

	m2, _ := json.Marshal(sub{Type: "subscribe", Channel: "ticker", ProductIDs: products})
	return [][]byte{m1, m2, mTR}

}

type envelope struct {
	Channel   string          `json:"channel"`
	Timestamp string          `json:"timestamp"`
	Events    json.RawMessage `json:"events"`
}

// heartbeats: events: [{ current_time, heartbeat_counter }]  :contentReference[oaicite:10]{index=10}
type hbEvent struct {
	HeartbeatCounter string `json:"heartbeat_counter"`
}
type hbWrap struct {
	Events []hbEvent `json:"events"`
}

type tickerWrap struct {
	Events []struct {
		Type    string `json:"type"`
		Tickers []struct {
			ProductID  string `json:"product_id"`
			Price      string `json:"price"`
			BestBid    string `json:"best_bid"`
			BestBidQty string `json:"best_bid_quantity"`
			BestAsk    string `json:"best_ask"`
			BestAskQty string `json:"best_ask_quantity"`
			// 其它字段先不管（volume_24_h 等）
		} `json:"tickers"`
	} `json:"events"`
}

// 只保留我们要用的字段
type envJson struct {
	Channel   string `json:"channel"`
	Timestamp string `json:"timestamp"`
	Events    []struct {
		Type   string `json:"type"` // snapshot/update
		Trades []struct {
			TradeID   string `json:"trade_id"`
			ProductID string `json:"product_id"`
			Price     string `json:"price"`
			Size      string `json:"size"`
			Side      string `json:"side"` // BUY/SELL
			Time      string `json:"time"` // RFC3339
		} `json:"trades"`
	} `json:"events"`
}

func (a Adapter) Decode(raw []byte, emit func(md.Event)) error {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return err
	}
	var e envJson
	if err := json.Unmarshal(raw, &e); err != nil {
		return err
	}

	// timestamp: "2023-02-09T20:30:37.167359596Z" 见示例 :contentReference[oaicite:12]{index=12}
	ts, _ := time.Parse(time.RFC3339Nano, env.Timestamp)

	switch env.Channel {
	case "heartbeats":
		var w hbWrap
		if err := json.Unmarshal(raw, &w); err != nil {
			return err
		}
		// events 里通常只有一个 counter（每秒一次）:contentReference[oaicite:13]{index=13}
		if len(w.Events) > 0 {
			emit(md.HeartbeatEvent{Exchange: a.Name(), Counter: w.Events[0].HeartbeatCounter, Ts: ts})
		}
		return nil
	case "market_trades":
		for _, ev := range e.Events {
			for _, t := range ev.Trades {
				px, ok1 := parseScaled(t.Price)
				sz, ok2 := parseScaled(t.Size)
				if !(ok1 && ok2) {
					continue
				}
				ts, err := time.Parse(time.RFC3339Nano, t.Time)
				if err != nil {
					// 有些时间可能不是 nano，退一步
					ts, _ = time.Parse(time.RFC3339, t.Time)
				}
				emit(md.TradeEvent{
					Exchange: a.Name(),
					Symbol:   t.ProductID,
					TradeID:  t.TradeID,
					Side:     t.Side,
					Price:    px,
					Size:     sz,
					Ts:       ts.UTC(),
				})
			}
		}
		return nil

	case "ticker":
		var w tickerWrap
		if err := json.Unmarshal(raw, &w); err != nil {
			return err
		}
		for _, ev := range w.Events {
			for _, t := range ev.Tickers {
				last, ok1 := parseScaled(t.Price)
				bidPx, ok2 := parseScaled(t.BestBid)
				bidQty, ok3 := parseScaled(t.BestBidQty)
				askPx, ok4 := parseScaled(t.BestAsk)
				askQty, ok5 := parseScaled(t.BestAskQty)
				if !(ok1 && ok2 && ok3 && ok4 && ok5) {
					continue
				}
				emit(md.BBOEvent{
					Exchange: a.Name(),
					Symbol:   t.ProductID, // 先原样，后面统一映射策略
					Ts:       ts,
					Last:     last,
					BidPx:    bidPx, BidQty: bidQty,
					AskPx: askPx, AskQty: askQty,
				})
			}
		}
		return nil

	default:
		// 按 Coinbase 要求：忽略未知消息类型/频道。:contentReference[oaicite:14]{index=14}
		return nil
	}
}

func parseScaled(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	var neg bool
	if s[0] == '-' {
		neg = true
		s = s[1:]
	}
	var intPart, fracPart int64
	var fracDigits int64
	dot := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '.' {
			if dot {
				return 0, false
			}
			dot = true
			continue
		}
		if ch < '0' || ch > '9' {
			return 0, false
		}
		d := int64(ch - '0')
		if !dot {
			intPart = intPart*10 + d
		} else if fracDigits < 8 {
			fracPart = fracPart*10 + d
			fracDigits++
		}
	}
	for fracDigits < 8 {
		fracPart *= 10
		fracDigits++
	}
	v := intPart*scale + fracPart
	if neg {
		v = -v
	}
	return v, true
}
