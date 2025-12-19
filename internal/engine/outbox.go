package engine

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"

	"gopherex.com/pkg/wal"
)

type Outbox interface {
	Append(ev Event) error
	AppendCmdEnd(seq uint64) error
	Flush() error
	Close() error
}

type EventOutbox struct {
	path   string
	w      *wal.Writer
	codec  EvCodec
	binBuf []byte
}

func OpenEventOutbox(path string, bufSize int, codec EvCodec) (*EventOutbox, error) {
	wr, err := wal.OpenWrite(path, bufSize)
	if err != nil {
		return nil, err
	}
	boxBuffer := make([]byte, evRecordLen)
	return &EventOutbox{path: path, w: wr, codec: codec, binBuf: boxBuffer}, nil
}

func (o *EventOutbox) Append(ev Event) error {
	// binary 用栈上 buffer；json 用小 slice（可读为主，测试足够）
	var dst []byte
	switch o.codec.(type) {
	case JSONEvCodec:
		dst = make([]byte, 0, 256)
	default:
		dst = o.binBuf[:0]
	}

	payload, _ := o.codec.Encode(dst, ev)
	return o.w.Append(payload)
}

func (o *EventOutbox) AppendCmdEnd(seq uint64) error {
	ev := Event{Type: EvCmdEnd, Seq: seq, Idx: 0}
	return o.Append(ev)
}

func (o *EventOutbox) Flush() error { return o.w.Flush() }
func (o *EventOutbox) Close() error { return o.w.Close() }

func ScanAndRepairOutbox(path string, codec EvCodec) (lastCompleteSeq uint64, lastCompleteOffset int64, err error) {
	// 文件不存在：正常
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		return 0, 0, nil
	}
	r, err := wal.OpenReader(path, 0, wal.ReaderOptions{
		AllowTruncatedTail: true,
	})
	if err != nil {
		return 0, 0, err
	}
	defer r.Close()

	for {
		p, nextOff, e := r.Next()
		if e != nil {
			// 正常结束
			if errors.Is(e, io.EOF) {
				break
			}
			// 真错误
			return 0, 0, e
		}

		ev, err := codec.Decode(p)
		if err != nil {
			return 0, 0, err
		}
		if ev.Type == EvCmdEnd {
			lastCompleteSeq = ev.Seq
			lastCompleteOffset = nextOff
		}
	}

	// 1) 如果尾部半写：先截断到最后一条“完整 record”
	if r.TruncatedTail() {
		if err := wal.TruncateTo(path, r.LastGoodOffset()); err != nil {
			return 0, 0, err
		}
	}

	// 2) 再把 “没有 CmdEnd 的残留事件” 截断掉（命令边界一致性）
	if lastCompleteOffset > 0 {
		st, e := os.Stat(path)
		if e == nil && st.Size() > lastCompleteOffset {
			if err := wal.TruncateTo(path, lastCompleteOffset); err != nil {
				return 0, 0, err
			}
		}
	}

	return lastCompleteSeq, lastCompleteOffset, nil
}

func outboxCursorPath(walDir, symbol string) string {
	return filepath.Join(walDir, safeSym(symbol)+".ev.cursor")
}

func outboxWalPath(walDir, symbol string) string {
	return filepath.Join(walDir, safeSym(symbol)+".ev.wal")
}

// cursor 文件：8 字节 little endian offset
func loadCursor(path string) int64 {
	b, err := os.ReadFile(path)
	if err != nil || len(b) < 8 {
		return 0
	}
	return int64(binary.LittleEndian.Uint64(b[:8]))
}

func storeCursor(path string, off int64) error {
	_ = os.MkdirAll(filepath.Dir(path), 0o755) // ✅ 确保目录存在

	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(off))

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b[:], 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func safeSym(symbol string) string {
	sb := make([]rune, 0, len(symbol))
	for _, r := range symbol {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_' || r == '-' {
			sb = append(sb, r)
		} else {
			sb = append(sb, '_')
		}
	}
	return string(sb)
}
