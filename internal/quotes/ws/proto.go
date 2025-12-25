package ws

type ClientMsg struct {
	Type   string   `json:"type"`   // "sub" | "unsub"
	Topics []string `json:"topics"` // topic list
}

type BarDTO struct {
	Symbol   string `json:"symbol"`
	Interval string `json:"interval"` // "1s" "1m" "1h" "1d"
	StartMs  int64  `json:"startMs"`
	EndMs    int64  `json:"endMs"`

	Open   string `json:"open"`
	High   string `json:"high"`
	Low    string `json:"low"`
	Close  string `json:"close"`
	Volume string `json:"volume"`
	Count  int64  `json:"count"`
}

type ServerMsg struct {
	Type  string `json:"type"`  // "kline"
	Topic string `json:"topic"` // e.g. kline:1m:BTC-USDT
	Bar   BarDTO `json:"bar"`
}
