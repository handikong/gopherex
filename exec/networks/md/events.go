package md

import "time"

type HeartbeatEvent struct {
	Exchange string
	Counter  string
	Ts       time.Time
}

func (e HeartbeatEvent) IsEvent() {
	//TODO implement me
	panic("implement me")
}

type BBOEvent struct {
	Exchange string
	Symbol   string    // 统一后的 symbol（例如 BTC-USD 原样保留也行）
	Ts       time.Time // 用 message timestamp
	Last     int64     // price
	BidPx    int64
	BidQty   int64
	AskPx    int64
	AskQty   int64
}

func (e BBOEvent) IsEvent() {
	//TODO implement me
	panic("implement me")
}

type TradeEvent struct {
	Exchange string
	Symbol   string
	TradeID  string
	Side     string // BUY/SELL（maker side）
	Price    int64  // scaled, 1e8
	Size     int64  // scaled, 1e8
	Ts       time.Time
}

func (e TradeEvent) IsEvent() {
	//TODO implement me
	panic("implement me")
}

type KlineEvent struct {
	Exchange string
	Symbol   string
	Interval time.Duration
	OpenTime time.Time

	Open  int64
	High  int64
	Low   int64
	Close int64
	Vol   int64
	Count int64

	UpdatedAt time.Time
}

func (e KlineEvent) IsEvent() {}
