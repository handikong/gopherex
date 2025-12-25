package model

type Side uint8

const (
	SideUnknown Side = iota + 1
	SideBuy          // maker is buyer
	SideSell         // maker is seller
)

func (s Side) String() string {
	switch s {
	case SideBuy:
		return "BUY"
	case SideSell:
		return "SELL"
	default:
		return "UNKNOWN"
	}
}

// Trade: 统一后的“成交”模型（v0-2 先用 string 存 price/size，避免 float64 误差）
//
// 语义约定：MakerSide 表示“挂单方(maker)的方向”
// - Coinbase market_trades: side 就是 maker side :contentReference[oaicite:5]{index=5}
// - Binance aggTrade: m=true 表示 buyer 是 maker => MakerSide=BUY :contentReference[oaicite:6]{index=6}
type Trade struct {
	Src    string // "coinbase" | "binance"
	Symbol string // 统一成 "BASE-QUOTE"（例如 BTC-USD / BTC-USDT）
	Base   string
	Quote  string

	PriceStr string // 十进制字符串
	SizeStr  string // 十进制字符串

	MakerSide Side
	TsUnixMs  int64  // 统一用毫秒时间戳（Binance 原生就是 ms；Coinbase 需从 RFC3339 解析）
	TradeID   string // 统一成 string（Coinbase 可能是 string；Binance 是数字）
}
