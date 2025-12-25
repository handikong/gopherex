package kline

import "gopherex.com/internal/quotes/datasource/model"

// 关键：别名，不是复制类型
type Trade = model.Trade
type Side = model.Side

const (
	SideUnknown = model.SideUnknown
	SideBuy     = model.SideBuy
	SideSell    = model.SideSell
)
