package engine

// 回放阶段只重建簿内状态，不对外 emit
// 回放只相当于回放状态 不复用事件
type noopEmitter struct{}

func (noopEmitter) Accepted(reqID uint64, orderID, userID uint64)                           {}
func (noopEmitter) Rejected(reqID uint64, orderID, userID uint64, reason string)            {}
func (noopEmitter) Added(reqID uint64, orderID, userID uint64)                              {}
func (noopEmitter) Cancelled(reqID uint64, orderID uint64)                                  {}
func (noopEmitter) Trade(reqID uint64, makerOrderID, takerOrderID uint64, price, qty int64) {}
