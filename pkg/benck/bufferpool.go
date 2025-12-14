package benck

import (
	"bytes"
	"encoding/json"
	"strconv"
	"sync"
)

type Order struct {
	ID     int64
	Symbol string
	Price  float64
	Amount float64
	Side   string
}

func encodeOrderNaiveBuffer(o Order) []byte {
	var buf bytes.Buffer
	// 往buffer里面写入
	buf.WriteString(`{"id":`)
	buf.WriteString(strconv.FormatInt(o.ID, 10))
	buf.WriteString(`,"symbol":"`)
	buf.WriteString(o.Symbol)
	buf.WriteString(`","price":`)
	buf.WriteString(strconv.FormatFloat(o.Price, 'f', -1, 64))
	buf.WriteString(`,"amount":`)
	buf.WriteString(strconv.FormatFloat(o.Amount, 'f', -1, 64))
	buf.WriteString(`,"side":"`)
	buf.WriteString(o.Side)
	buf.WriteString(`"}`)
	return buf.Bytes()
}

var orderBufferPool = sync.Pool{
	New: func() any {
		return bytes.NewBuffer(make([]byte, 0, 1024))
	},
}

func encodeOrderPoolBuffer(o Order) []byte {
	buf := orderBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer orderBufferPool.Put(buf)

	buf.WriteString(`{"id":`)
	buf.WriteString(strconv.FormatInt(o.ID, 10))
	buf.WriteString(`,"symbol":"`)
	buf.WriteString(o.Symbol)
	buf.WriteString(`","price":`)
	buf.WriteString(strconv.FormatFloat(o.Price, 'f', -1, 64))
	buf.WriteString(`,"amount":`)
	buf.WriteString(strconv.FormatFloat(o.Amount, 'f', -1, 64))
	buf.WriteString(`,"side":"`)
	buf.WriteString(o.Side)
	buf.WriteString(`"}`)
	out := make([]byte, buf.Len())
	copy(out, buf.Bytes())
	return out

}

func writePriceNaive(buf *bytes.Buffer, price float64) {
	s := strconv.FormatFloat(price, 'f', -1, 64)
	buf.WriteString(s)
}

type MyBuf struct {
	bs []byte
}

func (b *MyBuf) AppendFloat64(f float64) {
	b.bs = strconv.AppendFloat(b.bs, f, 'f', -1, 64)
}

func (b *MyBuf) Bytes() []byte { return b.bs }
func (b *MyBuf) Reset()        { b.bs = b.bs[:0] }

func writePriceFast(buf *MyBuf, price float64) {
	buf.AppendFloat64(price)
}

func encodeOrderStdJSON(o Order) []byte {
	b, _ := json.Marshal(o)
	return b
}

func encodeOrderManualJSON(buf *bytes.Buffer, o Order) []byte {
	buf.Reset()
	buf.WriteByte('{')

	buf.WriteString(`"id":`)
	buf.WriteString(strconv.FormatInt(o.ID, 10))

	buf.WriteString(`,"symbol":"`)
	buf.WriteString(o.Symbol)
	buf.WriteByte('"')

	buf.WriteString(`,"price":`)
	buf.WriteString(strconv.FormatFloat(o.Price, 'f', -1, 64))

	buf.WriteString(`,"amount":`)
	buf.WriteString(strconv.FormatFloat(o.Amount, 'f', -1, 64))

	buf.WriteString(`,"side":"`)
	buf.WriteString(o.Side)
	buf.WriteByte('"')

	buf.WriteByte('}')
	out := make([]byte, buf.Len())
	copy(out, buf.Bytes())
	return out
}
