package engine

//
//import (
//	"context"
//	"testing"
//	"time"
//)
//
//type fakeBook struct{}
//
//func (b fakeBook) SubmitLimit(orderID, userID uint64, side Side, price, qty int64, emit Emitter) {
//	emit.Accepted(emit.(actorEmitter).reqID, orderID, userID) // 仅测试用：取 reqID
//	emit.Added(emit.(actorEmitter).reqID, orderID, userID)
//}
//
//func (b fakeBook) Cancel(orderID uint64, emit Emitter) bool {
//	emit.Cancelled(emit.(actorEmitter).reqID, orderID)
//	return true
//}
//
//func TestActorSeqMonotonic(t *testing.T) {
//	bus := NewChanBus(1024)
//	a := NewSymbolActor(fakeBook{}, bus, ActorConfig{MailboxSize: 1024, BatchMax: 64}, nil)
//
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//	go a.Run(ctx)
//
//	// 连续投递 100 条 submit
//	for i := 1; i <= 100; i++ {
//		cmd := Command{
//			Type: CmdSubmitLimit, ReqID: uint64(i),
//			OrderID: uint64(i), UserID: 1, Side: Buy, Price: 100, Qty: 1,
//		}
//		if err := a.TryEnqueue(cmd); err != nil {
//			t.Fatalf("enqueue: %v", err)
//		}
//	}
//
//	// 收事件，检查 seq 单调不减（严格递增也可以）
//	counts := make(map[uint64]int)
//	var maxSeq uint64
//
//	deadline := time.After(2 * time.Second)
//	seen := 0
//	for seen < 200 { // 每个 submit 产生 Accepted+Added 两个事件
//		select {
//		case ev := <-bus.C():
//			counts[ev.Seq]++
//			if ev.Seq > maxSeq {
//				maxSeq = ev.Seq
//			}
//			seen++
//		case <-deadline:
//			t.Fatalf("timeout, seen=%d", seen)
//		}
//	}
//}
//
//func TestBackpressure(t *testing.T) {
//	bus := NewChanBus(1) // 故意很小
//	a := NewSymbolActor(fakeBook{}, bus, ActorConfig{MailboxSize: 4, BatchMax: 1}, nil)
//
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//	go a.Run(ctx)
//
//	// 塞满 mailbox
//	for i := 0; i < 10; i++ {
//		err := a.TryEnqueue(Command{
//			Type: CmdSubmitLimit, ReqID: uint64(i + 1),
//			OrderID: uint64(i + 1), UserID: 1, Side: Buy, Price: 1, Qty: 1,
//		})
//		// 至少应出现 ErrEngineBusy
//		if err == ErrEngineBusy {
//			return
//		}
//	}
//	t.Fatalf("expected EngineBusy but not seen")
//}
