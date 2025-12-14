package benck

import (
	"bytes"
	"testing"
)

func newOrder() Order {
	return Order{
		ID:     123456789,
		Symbol: "BTC-USDT",
		Price:  68000.5,
		Amount: 0.1234,
		Side:   "buy",
	}
}

//func BenchmarkEncodeOrderNaiveBuffer(b *testing.B) {
//	o := newOrder()
//	b.ResetTimer()
//	for i := 0; i < b.N; i++ {
//		_ = encodeOrderNaiveBuffer(o)
//	}
//}
//
//func BenchmarkEncodeOrderPooled(b *testing.B) {
//	o := newOrder()
//	b.ResetTimer()
//	for i := 0; i < b.N; i++ {
//		_ = encodeOrderPoolBuffer(o)
//	}
//}

//func BenchmarkWritePriceNaive(b *testing.B) {
//	buf := &bytes.Buffer{}
//	price := 12345.6789
//	b.ResetTimer()
//	for i := 0; i < b.N; i++ {
//		buf.Reset()
//		writePriceNaive(buf, price)
//	}
//}
//
//func BenchmarkWritePriceFast(b *testing.B) {
//	buf := &MyBuf{bs: make([]byte, 0, 64)}
//	price := 12345.6789
//	b.ResetTimer()
//	for i := 0; i < b.N; i++ {
//		buf.Reset()
//		writePriceFast(buf, price)
//	}
//}

func BenchmarkEncodeOrderStdJSON(b *testing.B) {
	o := newOrder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = encodeOrderStdJSON(o)
	}
}

func BenchmarkEncodeOrderManualJSON(b *testing.B) {
	o := newOrder()
	buf := &bytes.Buffer{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = encodeOrderManualJSON(buf, o)
	}
}
