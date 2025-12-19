package engine

import (
	"context"
	"io"
	"os"
	"time"

	"gopherex.com/pkg/wal"
)

type OutboxPublisher struct {
	ctx        context.Context
	bus        *ChanBus
	evPath     string
	cursorPath string
	notify     <-chan struct{}
	evCodec    EvCodec
	poll       time.Duration
}

func NewOutboxPublisher(ctx context.Context, bus *ChanBus, evPath, cursorPath string, notify <-chan struct{}, poll time.Duration, evcode EvCodec) *OutboxPublisher {
	if poll <= 0 {
		poll = 50 * time.Millisecond
	}
	return &OutboxPublisher{
		ctx: ctx, bus: bus,
		evPath:     evPath,
		cursorPath: cursorPath,
		notify:     notify,
		poll:       poll,
		evCodec:    evcode,
	}
}

func (p *OutboxPublisher) Run() {
	// 先读取
	committedOff := loadCursor(p.cursorPath)
	off := committedOff
	// cursor 可能大于文件大小（比如修复/截断过），需要矫正
	if st, err := os.Stat(p.evPath); err == nil && off > st.Size() {
		off = st.Size()
		committedOff = off
		if err = storeCursor(p.cursorPath, off); err != nil {
			return
		}
	}
	open := func() (*wal.Reader, error) {
		return wal.OpenReader(p.evPath, off, wal.ReaderOptions{AllowTruncatedTail: true})
	}

	r, err := open()
	if err != nil {
		// 文件不存在就等
		for err != nil && os.IsNotExist(err) {
			p.wait()
			if p.ctx.Err() != nil {
				return
			}
			r, err = open()
		}
		if err != nil {
			return
		}
	}
	defer r.Close()

	for {
		select {
		case <-p.ctx.Done():
			return
		default:
		}
		payload, nextOff, err := r.Next()
		if err != nil {
			_ = r.Close()
			if err == io.EOF {
				p.wait()
				continue
			}
			// 真错误：等一会重试
			off = committedOff
			p.wait()
			continue
		}

		ev, err := p.evCodec.Decode(payload)
		if err != nil {
			off = committedOff
			_ = r.Close()
			p.wait()
			continue
		}

		// CmdEnd：不发布，但把 cursor 落盘（断点只推进到命令边界）
		if ev.Type == EvCmdEnd {
			_ = storeCursor(p.cursorPath, off)
			continue
		}

		// 关键事件：阻塞发布（Publisher 不在撮合线程里，允许阻塞）
		if err := p.bus.Publish(p.ctx, ev); err != nil {
			_ = r.Close()
			off = committedOff // ✅ 回滚，避免丢
			p.wait()
			continue
		}
		off = nextOff
	}
}

func (p *OutboxPublisher) wait() {
	select {
	case <-p.ctx.Done():
		return
	case <-p.notify:
		return
	case <-time.After(p.poll):
		return
	}
}
