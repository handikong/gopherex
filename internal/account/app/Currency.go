package app

import "fmt"

type Currency struct {
	Symbol    string
	Precision int
	Scale     int64 // 10^Precision（预计算）
}

func NewCurrency(symbol string, precision int) (Currency, error) {
	if symbol == "" || precision < 0 || precision > 18 { // 18 对 ETH 足够；更大你要换 big.Int/DECIMAL(36,0)
		return Currency{}, fmt.Errorf("bad currency meta")
	}
	scale, err := pow10i64(precision)
	if err != nil {
		return Currency{}, err
	}
	return Currency{Symbol: symbol, Precision: precision, Scale: scale}, nil
}

func pow10i64(p int) (int64, error) {
	var v int64 = 1
	for i := 0; i < p; i++ {
		if v > (1<<63-1)/10 {
			return 0, fmt.Errorf("error")
		}
		v *= 10
	}
	return v, nil
}
