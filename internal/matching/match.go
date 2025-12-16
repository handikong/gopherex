package matching

// 进行撮合交易
func (b *NaiveOrderBook) SubmitLimit(taker *Order) (trades []Trade) {
	if taker == nil || taker.Qty <= 0 {
		return nil
	}
	switch taker.Side {
	case Buy:
		return b.matchBuy(taker)
	case Sell:
		return b.matchSell(taker)
	default:
		return nil
	}
}

func (b *NaiveOrderBook) matchBuy(taker *Order) []Trade {
	trades := make([]Trade, 0, 8)

	for taker.Qty > 0 && len(b.asks) > 0 {
		maker := b.asks[0] // best ask (lowest), FIFO within same price already in slice order
		// Price condition: buy can only take asks priced <= taker.Price
		if maker.Price > taker.Price {
			break
		}
		execQty := min64(taker.Qty, maker.Qty)
		trades = append(trades, Trade{
			TakerID: taker.ID,
			MakerID: maker.ID,
			Price:   maker.Price,
			Qty:     execQty,
		})

		taker.Qty -= execQty
		maker.Qty -= execQty

		if maker.Qty == 0 {
			_ = b.popBestAsk()
		}
	}

	// Remaining becomes passive order (add to bids)
	if taker.Qty > 0 {
		// Note: we keep the same pointer; caller can pass a copy if needed.
		b.Add(taker)
	}

	return trades
}

func (b *NaiveOrderBook) matchSell(taker *Order) []Trade {
	trades := make([]Trade, 0, 8)
	for taker.Qty > 0 && len(b.bids) > 0 {
		maker := b.bids[0] // best bid (highest), FIFO within same price already in slice order

		// Price condition: sell can only take bids priced >= taker.Price
		if maker.Price < taker.Price {
			break
		}

		execQty := min64(taker.Qty, maker.Qty)
		trades = append(trades, Trade{
			TakerID: taker.ID,
			MakerID: maker.ID,
			Price:   maker.Price,
			Qty:     execQty,
		})

		taker.Qty -= execQty
		maker.Qty -= execQty

		if maker.Qty == 0 {
			_ = b.popBestBid()
		}
	}

	// Remaining becomes passive order (add to asks)
	if taker.Qty > 0 {
		b.Add(taker)
	}
	return trades
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
