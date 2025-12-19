package engine

type OrderBook interface {
	SubmitLimit(reqID, orderID, userID uint64, side uint8, price, qty int64, emit Emitter)
	Cancel(reqID, orderID uint64, emit Emitter) bool
}
type Emitter interface {
	Accepted(reqID uint64, orderID, userID uint64)
	Rejected(reqID uint64, orderID, userID uint64, reason string)
	Added(reqID uint64, orderID, userID uint64)
	Cancelled(reqID uint64, orderID uint64)
	Trade(reqID uint64, makerOrderID, takerOrderID uint64, price, qty int64)
}

// EventSink：下游“可能慢”，所以只提供 TryPublish（非阻塞）
// V1：关键事件会接 WAL/专用通道
type EventSink interface {
	TryPublish(ev Event)
}

type CmdCodec interface {
	Encode(dst []byte, seq uint64, cmd Command) ([]byte, error)
	Decode(payload []byte) (seq uint64, cmd Command, err error)
}

type EvCodec interface {
	Encode(dst []byte, ev Event) ([]byte, error)
	Decode(payload []byte) (Event, error)
}
