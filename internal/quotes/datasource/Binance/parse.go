package binance

import (
	"errors"
	"github.com/segmentio/encoding/json"
	"strconv"
	"strings"

	"gopherex.com/internal/quotes/datasource/model"
)

type bnCombined struct {
	Stream string          `json:"stream"`
	Data   json.RawMessage `json:"data"`
}

type bnAggTrade struct {
	EventType string `json:"e"`
	Symbol    string `json:"s"`
	AggID     int64  `json:"a"`
	Price     string `json:"p"`
	Qty       string `json:"q"`
	TradeTime int64  `json:"T"`
	M         bool   `json:"m"`
}

func ParseBinanceAggTradeCombined(b []byte) (model.Trade, error) {
	var wrap bnCombined
	if err := json.Unmarshal(b, &wrap); err != nil {
		return model.Trade{}, err
	}
	var a bnAggTrade
	if err := json.Unmarshal(wrap.Data, &a); err != nil {
		return model.Trade{}, err
	}
	if a.EventType != "aggTrade" {
		return model.Trade{}, errors.New("not aggTrade")
	}

	base, quote, ok := splitBinanceSymbol(a.Symbol)
	if !ok {
		return model.Trade{}, errors.New("cannot split symbol: " + a.Symbol)
	}
	makerSide := model.SideSell
	if a.M {
		makerSide = model.SideBuy
	}

	return model.Trade{
		Src:       "binance",
		Symbol:    base + "-" + quote,
		Base:      base,
		Quote:     quote,
		PriceStr:  a.Price,
		SizeStr:   a.Qty,
		MakerSide: makerSide,
		TsUnixMs:  a.TradeTime,
		TradeID:   strconv.FormatInt(a.AggID, 10),
	}, nil
}

func splitBinanceSymbol(sym string) (base, quote string, ok bool) {
	s := strings.ToUpper(sym)
	quotes := []string{
		"FDUSD", "USDT", "USDC", "BUSD", "TUSD",
		"BTC", "ETH", "BNB",
		"EUR", "GBP", "TRY", "JPY", "AUD", "BRL", "RUB",
	}
	for _, q := range quotes {
		if strings.HasSuffix(s, q) && len(s) > len(q) {
			return s[:len(s)-len(q)], q, true
		}
	}
	return "", "", false
}
