package engine

import (
	"context"
	"sync/atomic"
)

type ActorConfig struct {
	MailboxSize int // 有多少个mail处理
	BatchMax    int // 一次最多多少
}
type walWriter interface {
	Append(payload []byte) error
	Flush() error
	Close() error
}

type SymbolActor struct {
	book OrderBook    // 订单id
	in   chan Command // Common通道
	//out  EventSink    // 输出事件
	cfg ActorConfig // 配置

	seq uint64 //序列号

	// metrics
	mailboxFull uint64 // 再说
	eventsDrop  uint64 // 再说
	wal         walWriter
	outbox      Outbox
	pubNotify   chan struct{} // buffered=1，用于通知 Publisher “有新事件了
	cmdCodec    CmdCodec
	evCodec     EvCodec
}

func NewSymbolActor(book OrderBook, cfg ActorConfig, wal walWriter,
	ob Outbox,
	pubNotify chan struct{},
	cmdCodec CmdCodec,
	evCodec EvCodec,
) *SymbolActor {
	if cfg.MailboxSize <= 0 {
		cfg.MailboxSize = 4096
	}
	if cfg.BatchMax <= 0 {
		cfg.BatchMax = 258
	}
	if pubNotify == nil {
		pubNotify = make(chan struct{}, 1)
	}

	return &SymbolActor{
		book: book,
		in:   make(chan Command, cfg.MailboxSize), //mailbox
		//out:       out,
		cfg:       cfg,
		wal:       wal,       //WAL（cmd.wal）
		outbox:    ob,        //事件 outbox（ev.wal）
		pubNotify: pubNotify, // actor 写完 outbox 并 flush 后，用它“踢一脚”publisher，减少 poll 延迟
		cmdCodec:  cmdCodec,
		evCodec:   evCodec,
	}
}

func (a *SymbolActor) TryEnqueue(cmd Command) error {
	//  将命令写入in chan
	// chan限制了数量 如果chan满了 就直接走default 导致刷爆
	// 可以使用这种方式来限制并发
	select {
	case a.in <- cmd:
		return nil
	default:
		atomic.AddUint64(&a.mailboxFull, 1)
		return ErrEngineBusy
	}
}

func (a *SymbolActor) MailboxFull() uint64   { return atomic.LoadUint64(&a.mailboxFull) }
func (a *SymbolActor) EventsDropped() uint64 { return atomic.LoadUint64(&a.eventsDrop) }

func (a *SymbolActor) Run(ctx context.Context) {
	if a.wal != nil {
		defer a.wal.Close()
	}
	if a.outbox != nil {
		defer a.outbox.Close()
	}

	// 复用 batch slice，避免每轮分配
	batch := make([]Command, 0, a.cfg.BatchMax)
	seqs := make([]uint64, 0, a.cfg.BatchMax) // 对齐 batch，用于第二段 apply
	for {
		var first Command
		//这段结构是一个非常常见的模式：“先阻塞拿 1 条，再尽量多拿几条（不阻塞）”。
		select {
		case <-ctx.Done():
			return
		case first = <-a.in:
		}
		// 这句不是“清空数组”，而是：
		//把 slice 的 长度变成 0
		//但 slice 背后那块 数组容量（cap）保留不变
		//也就是说：
		//✅ 复用同一块内存当缓冲区，避免每轮循环都重新分配新的 slice。
		//你可以把它理解成：
		//“把篮子里东西倒掉，但篮子还在，下次继续用同一个篮子装新东西。”
		//如果没有这句，你可能会：
		//每轮都 make([]Command, 0, BatchMax) → 频繁分配
		//或者 batch 会不断增长，内容混进上一轮的数据（逻辑错误）
		batch = batch[:0]
		batch = append(batch, first)
		// 不阻塞 尽量拿多条
		for len(batch) < a.cfg.BatchMax {
			select {
			case cmd := <-a.in:
				batch = append(batch, cmd)
			default:
				goto PROCESS
			}
		}
	PROCESS:
		//  记录所有执行的命令
		seqs = seqs[:0]
		if cap(seqs) < len(batch) {
			seqs = make([]uint64, 0, len(batch))
		}

		if a.wal != nil {
			for i := 0; i < len(batch); i++ {
				// 每次都进行累加 序列号
				a.seq++
				cmdSeq := a.seq
				seqs = append(seqs, cmdSeq)
				// 栈上数组：避免每条命令分配 payload
				var rec [cmdRecordLen]byte
				// wal写了cmd命令
				payload, _ := a.cmdCodec.Encode(rec[:0], cmdSeq, batch[i])
				if err := a.wal.Append(payload); err != nil {
					// V0：WAL 写失败视为致命（不 apply），直接退出 actor
					// 真实工程可改成：reject + 触发熔断/报警/重启
					return
				}
			}
			// 写了一轮 刷新下flush
			if err := a.wal.Flush(); err != nil {
				return
			}
		} else {
			// 未开启 WAL，也要分配 seq，保持事件序号一致
			for i := 0; i < len(batch); i++ {
				a.seq++
				seqs = append(seqs, a.seq)
			}
		}
		// ---------- Phase 2: Apply + Outbox（事件事实） ----------
		// 逐命令执行，事件写 outbox；每条命令末尾写 EvCmdEnd(seq)
		for i := 0; i < len(batch); i++ {
			cmd := batch[i]
			seq := seqs[i]
			var emit Emitter
			var obEm *outboxEmitter
			if a.outbox != nil {
				//  这个seq是每轮都会重置 是否用这个比较可靠
				//  reqId是由上游传过来的
				emit = &outboxEmitter{out: a.outbox, seq: seq, req: cmd.ReqID}
			} else {
				emit = noopEmitter{} // 或者你旧的 actorEmitter
			}

			// 判断命令类型
			switch cmd.Type {
			case CmdSubmitLimit:
				if cmd.OrderID == 0 || cmd.Qty <= 0 || cmd.Price <= 0 || (cmd.Side != Buy && cmd.Side != Sell) {
					emit.Rejected(cmd.ReqID, cmd.OrderID, cmd.UserID, "bad submit")
					continue
				}
				a.book.SubmitLimit(cmd.ReqID, cmd.OrderID, cmd.UserID, cmd.Side, cmd.Price, cmd.Qty, emit)
			case CmdCancel:
				if cmd.CancelOrderID == 0 {
					emit.Rejected(cmd.ReqID, 0, 0, "bad cancel")
					continue
				}
				ok := a.book.Cancel(cmd.ReqID, cmd.CancelOrderID, emit)
				if !ok {
					// V0 语义：取消不存在也发一个 Rejected（或你可改成 Cancelled(false)）
					emit.Rejected(cmd.ReqID, cmd.CancelOrderID, 0, "order not found")
				}
			default:
				emit.Rejected(cmd.ReqID, cmd.OrderID, cmd.UserID, "unknown cmd")
			}
			// outbox 写事件失败：直接停止（重启会靠 cmd.wal 补齐 outbox）
			if obEm != nil && obEm.err != nil {
				return
			}
			// 写“命令边界”——表示 seq 对应的事件集合完整落盘
			if a.outbox != nil {
				if err := a.outbox.AppendCmdEnd(seq); err != nil {
					return
				}
			}
		}
		// batch 末尾：outbox Flush 一次（组提交）
		if a.outbox != nil {
			if err := a.outbox.Flush(); err != nil {
				return
			}
			// 通知 publisher（不阻塞）
			select {
			case a.pubNotify <- struct{}{}:
			default:
			}
		}
	}
}

//type actorEmitter struct {
//	actor *SymbolActor
//	reqID uint64
//	seq   uint64
//}
//
//func (e actorEmitter) pub(ev Event) {
//	if ok := e.actor.out.TryPublish(ev); !ok {
//		atomic.AddUint64(&e.actor.eventsDrop, 1)
//	}
//}
//
//func (e actorEmitter) Accepted(reqID uint64, orderID, userID uint64) {
//	e.pub(Event{Type: EvAccepted, Seq: e.seq, ReqID: reqID, OrderID: orderID, UserID: userID})
//}
//func (e actorEmitter) Rejected(reqID uint64, orderID, userID uint64, reason string) {
//	e.pub(Event{Type: EvRejected, Seq: e.seq, ReqID: reqID, OrderID: orderID, UserID: userID, Reason: reason})
//}
//func (e actorEmitter) Added(reqID uint64, orderID, userID uint64) {
//	e.pub(Event{Type: EvAdded, Seq: e.seq, ReqID: reqID, OrderID: orderID, UserID: userID})
//}
//func (e actorEmitter) Cancelled(reqID uint64, orderID uint64) {
//	e.pub(Event{Type: EvCancelled, Seq: e.seq, ReqID: reqID, OrderID: orderID})
//}
//func (e actorEmitter) Trade(reqID uint64, makerOrderID, takerOrderID uint64, price, qty int64) {
//	e.pub(Event{
//		Type: EvTrade, Seq: e.seq, ReqID: reqID,
//		MakerOrderID: makerOrderID, TakerOrderID: takerOrderID,
//		Price: price, Qty: qty,
//	})
//}

type outboxEmitter struct {
	out Outbox
	seq uint64
	req uint64
	idx uint16
	err error
}

func (e *outboxEmitter) next() uint16 { i := e.idx; e.idx++; return i }
func (e *outboxEmitter) setErr(err error) {
	if e.err == nil && err != nil {
		e.err = err
	}
}

func (e *outboxEmitter) Accepted(reqID uint64, orderID, userID uint64) {
	e.setErr(e.out.Append(Event{
		Type: EvAccepted, Seq: e.seq, Idx: e.next(), ReqID: reqID,
		OrderID: orderID, UserID: userID,
	}))
}
func (e *outboxEmitter) Rejected(reqID uint64, orderID, userID uint64, reason string) {
	e.setErr(e.out.Append(Event{
		Type: EvRejected, Seq: e.seq, Idx: e.next(), ReqID: reqID,
		OrderID: orderID, UserID: userID,
	}))
}
func (e *outboxEmitter) Added(reqID uint64, orderID, userID uint64) {
	e.setErr(e.out.Append(Event{
		Type: EvAdded, Seq: e.seq, Idx: e.next(), ReqID: reqID,
		OrderID: orderID, UserID: userID,
	}))
}
func (e *outboxEmitter) Cancelled(reqID uint64, orderID uint64) {
	e.setErr(e.out.Append(Event{
		Type: EvCancelled, Seq: e.seq, Idx: e.next(), ReqID: reqID,
		OrderID: orderID,
	}))
}
func (e *outboxEmitter) Trade(reqID uint64, makerOrderID, takerOrderID uint64, price, qty int64) {
	e.setErr(e.out.Append(Event{
		Type: EvTrade, Seq: e.seq, Idx: e.next(), ReqID: reqID,
		MakerOrderID: makerOrderID, TakerOrderID: takerOrderID,
		Price: price, Qty: qty,
	}))
}
