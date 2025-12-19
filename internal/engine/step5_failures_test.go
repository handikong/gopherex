package engine

import (
	"context"
	"errors"
	"io"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"gopherex.com/pkg/wal"
)

/************ JSON codecs (test-only) ************/

/************ Mocks ************/

// mockBook：用来断言“是否发生了 apply”
type mockBook struct {
	submitCalls uint64
	cancelCalls uint64
}

func (m *mockBook) SubmitLimit(reqID, orderID, userID uint64, side uint8, price, qty int64, emit Emitter) {
	atomic.AddUint64(&m.submitCalls, 1)
	// 这里不重要：随便发一个 Added，便于 outbox 看得见
	emit.Added(reqID, orderID, userID)
}
func (m *mockBook) Cancel(reqID, orderID uint64, emit Emitter) bool {
	atomic.AddUint64(&m.cancelCalls, 1)
	emit.Cancelled(reqID, orderID)
	return true
}

type failingWal struct {
	appendErr       error
	flushErr        error
	appendN         int32
	flushN          int32
	failAfterAppend int32 // 第几次 Append 开始失败（1-based）；<=0 表示不失败
	failFlush       bool
}

func (w *failingWal) Append(_ []byte) error {
	n := atomic.AddInt32(&w.appendN, 1)
	if w.failAfterAppend > 0 && n >= w.failAfterAppend {
		if w.appendErr != nil {
			return w.appendErr
		}
		return errors.New("wal append fail")
	}
	return nil
}
func (w *failingWal) Flush() error {
	atomic.AddInt32(&w.flushN, 1)
	if w.failFlush {
		if w.flushErr != nil {
			return w.flushErr
		}
		return errors.New("wal flush fail")
	}
	return nil
}
func (w *failingWal) Close() error { return nil }

type failingOutbox struct {
	appendErr  error
	cmdEndErr  error
	flushErr   error
	failAppend bool
	failCmdEnd bool
	failFlush  bool
}

func (o *failingOutbox) Append(ev Event) error {
	if o.failAppend {
		if o.appendErr != nil {
			return o.appendErr
		}
		return errors.New("outbox append fail")
	}
	return nil
}
func (o *failingOutbox) AppendCmdEnd(seq uint64) error {
	if o.failCmdEnd {
		if o.cmdEndErr != nil {
			return o.cmdEndErr
		}
		return errors.New("outbox cmdend fail")
	}
	return o.Append(Event{Type: EvCmdEnd, Seq: seq})
}
func (o *failingOutbox) Flush() error {
	if o.failFlush {
		if o.flushErr != nil {
			return o.flushErr
		}
		return errors.New("outbox flush fail")
	}
	return nil
}
func (o *failingOutbox) Close() error { return nil }

/************ Helpers ************/

func runActorOnce(t *testing.T, a *SymbolActor, enqueue Command) (exited bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() { a.Run(ctx); close(done) }()

	if err := a.TryEnqueue(enqueue); err != nil {
		t.Fatalf("TryEnqueue: %v", err)
	}

	// 让 actor 处理一小会
	select {
	case <-done:
		return true
	case <-time.After(120 * time.Millisecond):
		// 还在跑：退出
		cancel()
		<-done
		return false
	}
}

// 读取 ev.wal，并 decode 成 Event 列表（JSON 可读）
func readOutboxEvents(t *testing.T, path string, codec JSONEvCodec) []Event {
	t.Helper()
	r, err := wal.OpenReader(path, 0, wal.ReaderOptions{AllowTruncatedTail: true})
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	defer r.Close()

	var out []Event
	for {
		p, _, e := r.Next()
		if e != nil {
			if errors.Is(e, io.EOF) {
				break
			}
			t.Fatalf("Next: %v", e)
		}
		ev, err := codec.Decode(p)
		if err != nil {
			t.Fatalf("decode: %v payload=%s", err, string(p))
		}
		out = append(out, ev)
	}
	return out
}

/************ Tests ************/

func TestFail_CmdWAL_AppendFail_NoApply(t *testing.T) {
	book := &mockBook{}
	cfg := ActorConfig{MailboxSize: 8, BatchMax: 8}

	w := &failingWal{failAfterAppend: 1} // 第一次 append 就失败
	var cmdCode = JSONCmdCodec{Version: 1}
	var evCode = JSONEvCodec{Version: 1}
	a := NewSymbolActor(book, cfg, w, nil, make(chan struct{}, 1), cmdCode, evCode)

	exited := runActorOnce(t, a, Command{
		Type: CmdSubmitLimit, ReqID: 1, OrderID: 1001, UserID: 2001, Side: Buy, Price: 100, Qty: 1,
	})
	if !exited {
		t.Fatalf("actor should exit on wal append fail")
	}
	if atomic.LoadUint64(&book.submitCalls) != 0 {
		t.Fatalf("expected no apply when wal append fails, submitCalls=%d", book.submitCalls)
	}
}

func TestFail_CmdWAL_FlushFail_NoApply(t *testing.T) {
	book := &mockBook{}
	cfg := ActorConfig{MailboxSize: 8, BatchMax: 8}

	w := &failingWal{failFlush: true}
	var cmdCode = JSONCmdCodec{Version: 1}
	var evCode = JSONEvCodec{Version: 1}
	a := NewSymbolActor(book, cfg, w, nil, make(chan struct{}, 1), cmdCode, evCode)

	exited := runActorOnce(t, a, Command{
		Type: CmdSubmitLimit, ReqID: 1, OrderID: 1001, UserID: 2001, Side: Buy, Price: 100, Qty: 1,
	})
	if !exited {
		t.Fatalf("actor should exit on wal flush fail")
	}
	if atomic.LoadUint64(&book.submitCalls) != 0 {
		t.Fatalf("expected no apply when wal flush fails, submitCalls=%d", book.submitCalls)
	}
}

func TestFail_Outbox_AppendFail_ActorExit_AfterWAL(t *testing.T) {
	book := &mockBook{}
	cfg := ActorConfig{MailboxSize: 8, BatchMax: 8}

	// WAL 正常
	w := &failingWal{}
	// Outbox 在写事件时失败（注意：这意味着 apply 阶段会触发退出）
	ob := &failingOutbox{failAppend: true}
	var cmdCode = JSONCmdCodec{Version: 1}
	var evCode = JSONEvCodec{Version: 1}
	a := NewSymbolActor(book, cfg, w, ob, make(chan struct{}, 1), cmdCode, evCode)

	exited := runActorOnce(t, a, Command{
		Type: CmdSubmitLimit, ReqID: 1, OrderID: 1001, UserID: 2001, Side: Buy, Price: 100, Qty: 1,
	})
	if !exited {
		t.Fatalf("actor should exit on outbox append fail")
	}
	// 注意：apply 可能已经发生（你的 mockBook 会 submitCalls++），这在设计上允许：
	// 因为状态只在内存，崩溃后会靠 cmd.wal replay 复原并补齐 outbox
	if atomic.LoadUint64(&book.submitCalls) == 0 {
		t.Fatalf("expected apply attempted before detecting outbox failure")
	}
}

func TestFail_InvalidCmd_WritesRejectedAndCmdEnd(t *testing.T) {
	dir := "./logs/"
	sym := "BTCUSDT"
	evPath := outboxWalPath(dir, sym)

	evCodec := JSONEvCodec{Version: 1}
	ob, err := OpenEventOutbox(evPath, 1<<16, evCodec)
	if err != nil {
		t.Fatal(err)
	}
	defer ob.Close()

	book := &mockBook{}
	cfg := ActorConfig{MailboxSize: 8, BatchMax: 8}
	// 这里 wal=nil：便于聚焦“无效命令 → Rejected”
	var cmdCode = JSONCmdCodec{Version: 1}
	var evCode = JSONEvCodec{Version: 1}
	a := NewSymbolActor(book, cfg, nil, ob, make(chan struct{}, 1), cmdCode, evCode)

	_ = runActorOnce(t, a, Command{
		Type: CmdSubmitLimit, ReqID: 7, OrderID: 999, UserID: 42, Side: Buy, Price: 100, Qty: 0, // Qty=0 => invalid
	})

	evs := readOutboxEvents(t, evPath, evCodec)

	// 你实现里：无效 submit 应 emit.Rejected，然后仍然 AppendCmdEnd(seq)
	// 这里只做“存在性断言”
	var hasRejected, hasCmdEnd bool
	for _, e := range evs {
		if e.Type == EvRejected && e.ReqID == 7 {
			hasRejected = true
		}
		if e.Type == EvCmdEnd {
			hasCmdEnd = true
		}
	}
	if !hasRejected {
		t.Fatalf("expected Rejected event in outbox, got=%v", evs)
	}
	if !hasCmdEnd {
		t.Fatalf("expected CmdEnd event in outbox, got=%v", evs)
	}
	// 无效命令不应触发 book.SubmitLimit
	if atomic.LoadUint64(&book.submitCalls) != 0 {
		t.Fatalf("invalid cmd should not apply book, submitCalls=%d", book.submitCalls)
	}
}

func TestFail_Outbox_ScanRepair_TruncatedTail(t *testing.T) {
	dir := "./logs/"
	sym := "BTCUSDT"
	evPath := outboxWalPath(dir, sym)

	evCodec := JSONEvCodec{Version: 1}
	ob, err := OpenEventOutbox(evPath, 1<<16, evCodec)
	if err != nil {
		t.Fatal(err)
	}

	// 写一条 CmdEnd(1) 作为 lastComplete
	if err := ob.Append(Event{Type: EvCmdEnd, Seq: 1}); err != nil {
		t.Fatal(err)
	}
	if err := ob.Flush(); err != nil {
		t.Fatal(err)
	}
	_ = ob.Close()

	// 手工制造尾部半写：只写 4 个字节（半个 header）
	f, err := os.OpenFile(evPath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte{0x01, 0x02, 0x03, 0x04})
	_ = f.Close()

	lastSeq, lastOff, err := ScanAndRepairOutbox(evPath, evCodec)
	if err != nil {
		t.Fatal(err)
	}
	if lastSeq != 1 {
		t.Fatalf("expected lastCompleteSeq=1, got=%d", lastSeq)
	}
	// repair 后文件大小应 <= lastOff（通常等于 lastOff）
	st, _ := os.Stat(evPath)
	if st.Size() > lastOff {
		t.Fatalf("expected truncated to <= lastCompleteOffset, size=%d lastOff=%d", st.Size(), lastOff)
	}
}

func TestFail_CmdWAL_ChecksumMismatch_ReplayError(t *testing.T) {
	dir := "./logs/"
	sym := "BTCUSDT"
	cmdPath := cmdWalPath(dir, sym)

	cmdCodec := JSONCmdCodec{Version: 1}

	// 写一条 cmd 记录
	wr, err := wal.OpenWrite(cmdPath, 1<<16)
	if err != nil {
		t.Fatal(err)
	}
	var tmp []byte
	payload, _ := cmdCodec.Encode(tmp[:0], 1, Command{
		Type: CmdSubmitLimit, ReqID: 1, OrderID: 1001, UserID: 2001, Side: Buy, Price: 100, Qty: 1,
	})
	if err := wr.Append(payload); err != nil {
		t.Fatal(err)
	}
	if err := wr.Flush(); err != nil {
		t.Fatal(err)
	}
	_ = wr.Close()

	// 破坏 payload 的 1 个字节（触发 checksum mismatch）
	b, err := os.ReadFile(cmdPath)
	if err != nil {
		t.Fatal(err)
	}
	// header 8 bytes 后是 payload；改 payload 第 1 字节
	if len(b) < 9 {
		t.Fatalf("cmd wal too small")
	}
	b[8] ^= 0xFF
	if err := os.WriteFile(cmdPath, b, 0o644); err != nil {
		t.Fatal(err)
	}

	// replay：应报 ErrChecksumMismatch（或你 wal 包对应的错误）
	book := &mockBook{}
	var cmdCode = JSONCmdCodec{Version: 1}
	_, err = replayCmdWALAndFillOutbox(cmdPath, book, nil, 0, cmdCode /*lastCompleteSeq*/)
	if err == nil {
		t.Fatalf("expected replay error on checksum mismatch")
	}
}
