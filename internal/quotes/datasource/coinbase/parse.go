package coinbase

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/segmentio/encoding/json"
	"gopherex.com/internal/quotes/datasource/model"
)

type cbMarketTradesMsg struct {
	Channel string `json:"channel"`
	Events  []struct {
		Type   string `json:"type"`
		Trades []struct {
			TradeID   string `json:"trade_id"`
			ProductID string `json:"product_id"`
			Price     string `json:"price"`
			Size      string `json:"size"`
			Side      string `json:"side"`
			Time      string `json:"time"`
		} `json:"trades"`
	} `json:"events"`
}

var msgPool = sync.Pool{
	New: func() any {
		return &cbMarketTradesMsg{}
	},
}

func ParseCoinbaseMarketTrades(b []byte) ([]model.Trade, error) {
	msg := msgPool.Get().(*cbMarketTradesMsg)
	*msg = cbMarketTradesMsg{} // 清空，避免残留 slice
	defer msgPool.Put(msg)
	if err := json.Unmarshal(b, &msg); err != nil {
		return nil, err
	}
	if msg.Channel != "market_trades" {
		return nil, errors.New("not market_trades")
	}

	out := make([]model.Trade, 0, 16)
	for _, ev := range msg.Events {
		for _, t := range ev.Trades {
			base, quote, ok := splitByDash(t.ProductID)
			if !ok {
				continue
			}
			ts, err := time.Parse(time.RFC3339Nano, t.Time)
			if err != nil {
				ts, err = time.Parse(time.RFC3339, t.Time)
				if err != nil {
					continue
				}
			}
			out = append(out, model.Trade{
				Src:       "coinbase",
				Symbol:    base + "-" + quote,
				Base:      base,
				Quote:     quote,
				PriceStr:  t.Price,
				SizeStr:   t.Size,
				MakerSide: parseSideUpper(t.Side),
				TsUnixMs:  ts.UnixMilli(),
				TradeID:   t.TradeID,
			})
		}
	}
	return out, nil
}

func splitByDash(productID string) (base, quote string, ok bool) {
	parts := strings.Split(productID, "-")
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func parseSideUpper(s string) model.Side {
	switch strings.ToUpper(s) {
	case "BUY":
		return model.SideBuy
	case "SELL":
		return model.SideSell
	default:
		return model.SideUnknown
	}
}
