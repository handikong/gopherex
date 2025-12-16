package matching

//
//import "testing"
//
//func BenchmarkSubmitLimit(b *testing.B) {
//	book := NewNaiveOrderBook()
//	// 加入价格100的卖单 1000XXX个
//	book.Add(&Order{ID: 1, Side: Sell, Price: 100, Qty: 1_000_000_000_000})
//	// 用买单来吃掉
//	b.ResetTimer()
//	for i := 0; i < b.N; i++ {
//		// Small taker each time
//		book.SubmitLimit(&Order{ID: uint64(1000 + i), Side: Buy, Price: 100, Qty: 1})
//	}
//
//}
//
//func BenchmarkSubmitLimit_Profile(b *testing.B) {
//	book := NewNaiveOrderBook()
//	book.Add(&Order{ID: 1, Side: Sell, Price: 100, Qty: 1_000_000_000_000})
//
//	o := Order{Side: Buy, Price: 100, Qty: 1}
//	const batch = 200000
//
//	b.ResetTimer()
//	for i := 0; i < b.N; i++ {
//		for j := 0; j < batch; j++ {
//			o.ID++
//			o.Qty = 1
//			book.SubmitLimit(&o)
//		}
//	}
//}
//
//func BenchmarkAddOrder(b *testing.B) {
//	book := NewNaiveOrderBook()
//	b.ResetTimer()
//	for i := 0; i < b.N; i++ {
//		book.Add(&Order{ID: uint64(i + 1), Side: Sell, Price: 100, Qty: 1})
//	}
//}
//
//func BenchmarkCancel_Scan(b *testing.B) {
//	book := NewNaiveOrderBook()
//	const N = 20000
//	// fill asks with same price so FIFO order is just insertion order
//	for i := 0; i < N; i++ {
//		book.Add(&Order{ID: uint64(i + 1), Side: Sell, Price: 100, Qty: 1})
//	}
//
//	// cancel near the middle each time
//	target := uint64(N / 2)
//
//	b.ResetTimer()
//	for i := 0; i < b.N; i++ {
//		// re-add to keep size stable
//		_ = book.Cancel(target)
//		book.Add(&Order{ID: target, Side: Sell, Price: 100, Qty: 1})
//	}
//}
//
//func BenchmarkCancel_Scan2(b *testing.B) {
//	book := NewNaiveOrderBook()
//	const N = 20000
//	// fill asks with same price so FIFO order is just insertion order
//	for i := 0; i < N; i++ {
//		book.Add(&Order{ID: uint64(i + 1), Side: Sell, Price: 100, Qty: 1})
//	}
//
//	// cancel near the middle each time
//	target := uint64(N / 2)
//
//	b.ResetTimer()
//	for i := 0; i < b.N; i++ {
//		// re-add to keep size stable
//		_ = book.Cancel2(target)
//		book.Add(&Order{ID: target, Side: Sell, Price: 100, Qty: 1})
//	}
//}
