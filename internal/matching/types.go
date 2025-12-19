package matching

// 定义数据结构
// 交易类型

const (
	Buy = iota + 1
	Sell
)

// 订单薄
type Order struct {
	ID     uint64 // 交易id
	Side   uint8
	Price  int64 //价格
	Qty    int64 //
	UserID uint64
}

// 交易
type Trade struct {
	TakerID uint64
	MakerID uint64
	Price   int64
	Qty     int64
}
