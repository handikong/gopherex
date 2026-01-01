package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopherex.com/pkg/safe"
	"gopherex.com/pkg/wal"
)

/*
*
负责分开请求去执行
*/
type BookFactory func(symbol string) (OrderBook, error)

type EngineConfig struct {
	EventBusSize    int           // event的size
	ActorCfg        ActorConfig   //act的配重
	BookFactory     BookFactory   // 使用的订单接口
	WALDir          string        // 文件地址
	EnableCmdWAL    bool          // 是否打开写入
	WALBufSize      int           // wal的大小
	EnableOutbox    bool          // 是否开启outBox
	OutboxBufSize   int           // outbox的大小
	EnablePublisher bool          // 是否开启publisher
	PublisherPoll   time.Duration //pulibsh的时间
	CmdCodec        CmdCodec
	EvCodec         EvCodec
	bus             *ChanBus
}

type Engine struct {
	ctx    context.Context         //  ctx
	cancel context.CancelFunc      //取消事件
	mu     sync.RWMutex            // 读锁
	actors map[string]*SymbolActor // 一一对应
	bus    *ChanBus                // 这个后面再理解
	cfg    EngineConfig
}

func NewEngine(cfg EngineConfig) *Engine {
	// 如果没有设置  就默认
	if cfg.EventBusSize <= 0 {
		cfg.EventBusSize = 1 << 16 //设置1M
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{
		ctx:    ctx,
		cancel: cancel,
		mu:     sync.RWMutex{},
		// 如果一开始就预设值好了 如果没有的话 就属于浪费了
		actors: make(map[string]*SymbolActor, cfg.EventBusSize),
		bus:    cfg.bus,
		cfg:    cfg,
	}
}

// 这个是推送事件
func (e *Engine) Events() <-chan Event { return e.bus.C() }

// 这个不晓得
func (e *Engine) DroppedEvents() uint64 { return e.bus.Dropped() }

func (e *Engine) getOrCreateActor(symbol string) (*SymbolActor, error) {
	// 1) 快路径：读锁查
	e.mu.RLock()
	a := e.actors[symbol]
	e.mu.RUnlock()
	if a != nil {
		return a, nil
	}

	// 2) 慢路径：写锁双检 + 创建
	e.mu.Lock()
	defer e.mu.Unlock()

	// double-check
	if a = e.actors[symbol]; a != nil {
		return a, nil
	}
	if e.cfg.BookFactory == nil {
		return nil, ErrUnknownSym
	}
	book, err := e.cfg.BookFactory(symbol)
	if err != nil {
		return nil, err
	}

	// 强约束：只要开启持久化相关能力，WALDir 必须配置
	if (e.cfg.EnableCmdWAL || e.cfg.EnableOutbox || e.cfg.EnablePublisher) && e.cfg.WALDir == "" {
		return nil, fmt.Errorf("WALDir is empty but persistence is enabled")
	}

	// 创建目录
	if (e.cfg.EnableCmdWAL || e.cfg.EnableOutbox) && e.cfg.WALDir != "" {
		_ = os.MkdirAll(e.cfg.WALDir, 0o755)
	}
	cmdPath := cmdWalPath(e.cfg.WALDir, symbol)       // <sym>.cmd.wal
	evPath := outboxWalPath(e.cfg.WALDir, symbol)     // <sym>.ev.wal
	curPath := outboxCursorPath(e.cfg.WALDir, symbol) // <sym>.ev.cursor
	if e.cfg.EnableOutbox && e.cfg.WALDir != "" {
		// ✅ 预创建 ev.wal：只要存在即可，不写内容也行
		if _, err = os.Stat(evPath); os.IsNotExist(err) {
			f, err := os.OpenFile(evPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
			if err == nil {
				_ = f.Close()
			}
			// 如果 err != nil 且不是 IsExist，返回错误更稳（权限/磁盘问题）
			if err != nil && !os.IsExist(err) {
				return nil, err
			}
		}
		// ✅ 也确保 cursor 目录存在（万一你未来 cursorPath 不在 WALDir 根）
		_ = os.MkdirAll(filepath.Dir(curPath), 0o755)
	}

	// outbox 里“最后一个完整命令边界”的 seq
	var lastCompleteSeq uint64
	var outboxWriter Outbox //实现 Outbox 接口的 writer（FileOutbox）
	pubNotify := make(chan struct{}, 1)

	if e.cfg.EnableOutbox && e.cfg.WALDir != "" {
		// 扫描修复文件
		lastCompleteSeq, _, err = ScanAndRepairOutbox(evPath, e.cfg.EvCodec)
		if err != nil {
			return nil, err
		}
		outboxWriter, err = OpenEventOutbox(evPath, e.cfg.OutboxBufSize, e.cfg.EvCodec)
		if err != nil {
			return nil, err
		}
	}

	// 4) replay cmd WAL to rebuild book; and if outbox exists,补齐缺失事件（seq > lastCompleteSeq）
	var lastSeq uint64
	if e.cfg.EnableCmdWAL && e.cfg.WALDir != "" {
		// 回放所有的事件  lastCompleteSeq 非常重要
		lastSeq, err = replayCmdWALAndFillOutbox(cmdPath, book, outboxWriter, lastCompleteSeq, e.cfg.CmdCodec)
		if err != nil {
			_ = closeIfNotNil(outboxWriter)
			return nil, err
		}
	} else {
		// 没开 cmd WAL 的话就没法重建簿（Step5 的前提），这里你可以选择：return error 或允许空簿
		lastSeq = 0
	}
	// 5) after replay: flush outbox once (如果补齐了事件，这里会把它 durable)
	if outboxWriter != nil {
		if err := outboxWriter.Flush(); err != nil {
			_ = closeIfNotNil(outboxWriter)
			return nil, err
		}
	}

	// 6) 打开cmd的后续读写
	// 启动完恢复后，打开写端，后面每条新命令都会 Append+Flush（按 batch）到 cmd.wal。
	var cmdWriter walWriter
	if e.cfg.EnableCmdWAL && e.cfg.WALDir != "" {
		cmdWriter, err = wal.OpenWrite(cmdPath, e.cfg.WALBufSize)
		if err != nil {
			_ = closeIfNotNil(outboxWriter)
			return nil, err
		}
	}
	// 7) 创建一个actor
	a = NewSymbolActor(book, e.cfg.ActorCfg, cmdWriter, outboxWriter, pubNotify, e.cfg.CmdCodec, e.cfg.EvCodec)
	//保证重启后 seq 连续（新命令从 lastSeq+1 开始）
	a.seq = lastSeq
	e.actors[symbol] = a
	// 8) start actor
	safe.Go(func() {
		a.Run(e.ctx)
	})

	// 9) start publisher (per active symbol)
	//publisher tail ev.wal，读到事件就发布到 bus；读到 EvCmdEnd 就推进 cursor
	if e.cfg.EnablePublisher && outboxWriter != nil {
		pub := NewOutboxPublisher(e.ctx, e.bus, evPath, curPath, pubNotify, e.cfg.PublisherPoll, e.cfg.EvCodec)
		safe.Go(func() {
			pub.Run()
		})
	}
	return a, nil
}

func (e *Engine) TrySubmit(symbol string, cmd Command) error {
	if cmd.Type != CmdSubmitLimit {
		return ErrBadCommand
	}
	a, err := e.getOrCreateActor(symbol)
	if err != nil {
		return err
	}
	return a.TryEnqueue(cmd)

}
func (e *Engine) TryCancel(symbol string, cmd Command) error {
	if cmd.Type != CmdCancel {
		return ErrBadCommand
	}
	a, err := e.getOrCreateActor(symbol)
	if err != nil {
		return err
	}
	return a.TryEnqueue(cmd)
}

func (e *Engine) Stop() { e.cancel() }

func cmdWalPath(dir, symbol string) string {
	// 最小清理：把文件名里不安全字符替换掉（你也可以更严格）
	s := make([]rune, 0, len(symbol))
	for _, r := range symbol {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_' || r == '-' {
			s = append(s, r)
		} else {
			s = append(s, '_')
		}
	}
	return filepath.Join(dir, string(s)+".wal")
}

func replayCmdWALAndFillOutbox(cmdPath string, book OrderBook, outbox Outbox, lastCompleteSeq uint64, code CmdCodec) (lastSeq uint64, err error) {
	_, err = wal.Replay(cmdPath, wal.ReplayOptions{
		AllowTruncatedTail: true,
	}, func(payload []byte) error {
		seq, cmd, err := code.Decode(payload) // Step5.2 的 decode
		if err != nil {
			return err
		}
		if seq > lastSeq {
			lastSeq = seq
		}
		/**
		如果 seq <= lastCompleteSeq：
		outbox 里已经完整存在这些事件了，你 不应该再写 outbox
		所以 emitter 应该是 noopEmitter（或只用于 book 内部不产生外部事件）

		如果 seq > lastCompleteSeq 且 outboxWriter != nil：
		outbox 缺事件，需要补
		所以 emitter 是 outboxEmitter{out: outboxWriter, seq: seq, req: cmd.ReqID}
		*/
		// 选择 emitter
		if outbox == nil || seq <= lastCompleteSeq {
			applyCommandToBook(book, cmd, noopEmitter{})
			return nil
		}

		// seq > lastCompleteSeq：补齐 outbox
		// 进行回溯事件
		em := &outboxEmitter{out: outbox, seq: seq, req: cmd.ReqID}
		applyCommandToBook(book, cmd, em)
		if em.err != nil {
			return em.err
		}
		//执行完这条命令后，必须 outbox.AppendCmdEnd(seq)
		//表示：这条 seq 的事件集合完整
		//
		//最后（通常在 replay 结束或每 N 条）outbox.Flush() 一次让补齐事件 durable
		return outbox.AppendCmdEnd(seq)
	})
	if err != nil {
		return 0, err
	}
	return lastSeq, nil
}

func applyCommandToBook(book OrderBook, cmd Command, emit Emitter) {
	switch cmd.Type {
	case CmdSubmitLimit:
		book.SubmitLimit(cmd.ReqID, cmd.OrderID, cmd.UserID, cmd.Side, cmd.Price, cmd.Qty, emit)
	case CmdCancel:
		book.Cancel(cmd.ReqID, cmd.CancelOrderID, emit)
	default:
		// ignore or emit reject
	}
}

func closeIfNotNil(ob Outbox) error {
	if ob == nil {
		return nil
	}
	return ob.Close()
}
