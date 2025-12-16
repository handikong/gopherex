package matching

//
//import (
//	"math/rand"
//	"testing"
//)
//
//func prepareOrders(n int, side Side, price int64) []Order {
//	os := make([]Order, n)
//	for i := 0; i < n; i++ {
//		os[i] = Order{
//			ID:    uint64(i + 1),
//			Side:  side,
//			Price: price,
//			Qty:   1,
//		}
//	}
//	return os
//}
//
//// -------- Cancel benchmarks --------
//
//// Naive: 扫描+memmove（你 Step1/Step2 的版本）
//func BenchmarkCancel_Naive_Scan_Mid(b *testing.B) {
//	book := NewNaiveOrderBook()
//	const N = 20000
//	for i := 0; i < N; i++ {
//		book.Add(&Order{ID: uint64(i + 1), Side: Sell, Price: 100, Qty: 1})
//	}
//	target := uint64(N / 2)
//
//	b.ResetTimer()
//	for i := 0; i < b.N; i++ {
//		_ = book.Cancel(target)
//		book.Add(&Order{ID: target, Side: Sell, Price: 100, Qty: 1})
//	}
//}
//
//// Level: O(1) 摘链（不 memmove）
//func BenchmarkCancel_Level_O1_Mid(b *testing.B) {
//	book := NewLevelOrderBook()
//	const N = 20000
//	for i := 0; i < N; i++ {
//		book.Add(&Order{ID: uint64(i + 1), Side: Sell, Price: 100, Qty: 1})
//	}
//	target := uint64(N / 2)
//
//	b.ResetTimer()
//	for i := 0; i < b.N; i++ {
//		_ = book.Cancel(target)
//		book.Add(&Order{ID: target, Side: Sell, Price: 100, Qty: 1})
//	}
//}
//
//// 更“真实”的撤单：随机撤单（避免总是撤到末尾导致假快）
//func BenchmarkCancel_Level_O1_Random(b *testing.B) {
//	book := NewLevelOrderBook()
//	const N = 20000
//	for i := 0; i < N; i++ {
//		book.Add(&Order{ID: uint64(i + 1), Side: Sell, Price: 100, Qty: 1})
//	}
//
//	r := rand.New(rand.NewSource(1))
//	b.ResetTimer()
//	for i := 0; i < b.N; i++ {
//		target := uint64(r.Intn(N) + 1)
//		_ = book.Cancel(target)
//		// 重新塞回去，保持规模稳定（注意 ID 不能重复：这里用一个新 ID）
//		newID := uint64(N + 1 + i)
//		book.Add(&Order{ID: newID, Side: Sell, Price: 100, Qty: 1})
//	}
//}
//
//// -------- Add benchmarks --------
//
//// Add（含分配）：真实路径（&Order{} 逃逸 + GC）
//func BenchmarkAdd_Naive_Alloc(b *testing.B) {
//	book := NewNaiveOrderBook()
//	b.ResetTimer()
//	for i := 0; i < b.N; i++ {
//		book.Add(&Order{ID: uint64(i + 1), Side: Sell, Price: 100, Qty: 1})
//	}
//}
//
//func BenchmarkAdd_Level_Alloc(b *testing.B) {
//	book := NewLevelOrderBook()
//	b.ResetTimer()
//	for i := 0; i < b.N; i++ {
//		book.Add(&Order{ID: uint64(i + 1), Side: Sell, Price: 100, Qty: 1})
//	}
//}
//
//// Add（去分配）：只测结构（Order 预先分配在数组中）
//func BenchmarkAdd_Naive_NoAlloc(b *testing.B) {
//	book := NewNaiveOrderBook()
//	os := prepareOrders(b.N, Sell, 100)
//	b.ResetTimer()
//	for i := 0; i < b.N; i++ {
//		book.Add(&os[i])
//	}
//}
//
//func BenchmarkAdd_Level_NoAlloc(b *testing.B) {
//	book := NewLevelOrderBook()
//	os := prepareOrders(b.N, Sell, 100)
//	b.ResetTimer()
//	for i := 0; i < b.N; i++ {
//		book.Add(&os[i])
//	}
//}
//
//// -------- Match benchmarks --------
//
//// 构造多个价位：让撮合跨档位
//func seedAsks_Level(book *LevelOrderBook, levels, perLevel int) {
//	var id uint64 = 1
//	for p := 0; p < levels; p++ {
//		price := int64(100 + p) // 100..100+levels-1
//		for i := 0; i < perLevel; i++ {
//			book.Add(&Order{ID: id, Side: Sell, Price: price, Qty: 1})
//			id++
//		}
//	}
//}
//
//func BenchmarkMatch_Level_CrossLevels(b *testing.B) {
//	book := NewLevelOrderBook()
//	seedAsks_Level(book, 50, 200) // 50 档，每档 200 单
//
//	b.ResetTimer()
//	for i := 0; i < b.N; i++ {
//		// 每次都买一批，跨多个价位
//		_ = book.SubmitLimit(&Order{ID: uint64(1_000_000 + i), Side: Buy, Price: 200, Qty: 500})
//	}
//}
