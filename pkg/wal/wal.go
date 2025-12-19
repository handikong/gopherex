package wal

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
)

// wal写日志的包
const (
	headerSize      = 8 // len(4) + crc32(4)
	defaultFilePerm = 0o644
)

// 你可以按需调大/调小：防止坏数据把内存吃爆
const DefaultMaxPayload = 4 << 20 // 4MB

// 定义错误
var (
	ErrCorruptHeader    = errors.New("wal: corrupt header")
	ErrCorruptPayload   = errors.New("wal: corrupt payload")
	ErrChecksumMismatch = errors.New("wal: checksum mismatch")
	ErrPayloadTooLarge  = errors.New("wal: payload too large")
)

type Writer struct {
	f  *os.File
	bw *bufio.Writer
	// 记录“已写入文件的逻辑偏移”（包含未 flush 的 bufio 数据也算）
	off int64
}

func OpenWrite(path string, buffSize int) (*Writer, error) {
	if buffSize <= 0 {
		buffSize = 1 << 20 //1M的内存
	}
	// 打开问价
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, defaultFilePerm)
	if err != nil {
		return nil, err
	}
	stat, err := file.Stat()
	if err != nil {
		// 关闭掉文件 返回错误
		_ = file.Close()
		return nil, err
	}
	return &Writer{
		f:   file,
		bw:  bufio.NewWriterSize(file, buffSize),
		off: stat.Size(),
	}, nil
}

// 写入文件内容
func (w *Writer) Append(payload []byte) error {
	var hrd [headerSize]byte
	// 进行二进制编码 八个字节的长度
	binary.LittleEndian.PutUint32(hrd[:4], uint32(len(payload)))
	binary.LittleEndian.PutUint32(hrd[4:], crc32.ChecksumIEEE(payload))
	// 两次写入  先写入head  再写入数据
	if _, err := w.bw.Write(hrd[:]); err != nil {
		return ErrCorruptHeader
	}
	if _, err := w.bw.Write(payload); err != nil {
		return ErrCorruptPayload
	}

	// 记录偏移量
	w.off += int64(headerSize + len(payload))
	return nil
}

// 刷新到磁盘
func (w *Writer) Flush() error {
	// 将buf刷新到内存 什么时候写入磁盘依据操作系统
	if err := w.bw.Flush(); err != nil {
		return err
	}
	// 再次确认 将数据刷新到磁盘
	if err := w.f.Sync(); err != nil {
		return err
	}
	return nil
}

// 加载数据
func (w *Writer) Close() error {
	// Close 前尽量把数据刷出去（你也可以选择不 Sync，只 Flush）
	if err := w.bw.Flush(); err != nil {
		_ = w.f.Close()
		return err
	}
	// 是否 Sync：取决于你希望 Close 也具备持久化语义
	if err := w.f.Sync(); err != nil {
		_ = w.f.Close()
		return err
	}
	return w.f.Close()
}

type ReplayOptions struct {
	MaxPayload int // <=0 则用 DefaultMaxPayload
	// 如果最后一条 record “半写/不完整”，是否认为是正常情况并停止（推荐 true）
	AllowTruncatedTail bool
}

type ReplayStats struct {
	Records        int
	BytesRead      int64
	LastGoodOffset int64
	TruncatedTail  bool
}

func Replay(path string, opts ReplayOptions, onRecord func(payload []byte) error) (ReplayStats, error) {
	var st ReplayStats
	maxPayload := opts.MaxPayload
	if maxPayload <= 0 {
		maxPayload = DefaultMaxPayload
	}

	f, err := os.Open(path)
	if err != nil {
		// WAL 文件不存在：说明还没写过，属于正常情况
		if errors.Is(err, os.ErrNotExist) {
			return st, nil
		}
		return st, err
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 1<<20) // 1MB read buffer
	var hdr [headerSize]byte
	var off int64
	for {
		// 读 header
		_, err = io.ReadFull(br, hdr[:])
		if err != nil {
			if errors.Is(err, io.EOF) {
				// 正常读完
				return st, nil
			}
			if errors.Is(err, io.ErrUnexpectedEOF) {
				// 尾部半写：常见于崩溃时写了一半 header
				st.TruncatedTail = true
				if opts.AllowTruncatedTail {
					return st, nil
				}
				return st, ErrCorruptHeader
			}
			return st, err
		}

		ln := int(binary.LittleEndian.Uint32(hdr[0:4]))
		crc := binary.LittleEndian.Uint32(hdr[4:8])

		if ln < 0 || ln > maxPayload {
			return st, ErrPayloadTooLarge
		}

		payload := make([]byte, ln) // Step5.2 再优化成复用 buffer
		_, err = io.ReadFull(br, payload)
		if err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) {
				// 尾部半写：写完 header 但 payload 没写完
				st.TruncatedTail = true
				if opts.AllowTruncatedTail {
					return st, nil
				}
				return st, ErrCorruptPayload
			}
			return st, err
		}
		// 数据进行验证 反之篡改
		if crc32.ChecksumIEEE(payload) != crc {
			return st, ErrChecksumMismatch
		}

		off += int64(headerSize + ln)
		st.BytesRead = off

		// 回放给上层（Step5.2：这里会 decode 成 Command 再喂给 OrderBook）
		if err := onRecord(payload); err != nil {
			return st, err
		}

		st.Records++
		st.LastGoodOffset = off
	}

}

func TruncateTo(path string, offset int64) error {
	if offset < 0 {
		return fmt.Errorf("wal: negative truncate offset %d", offset)
	}

	// If file doesn't exist, treat as no-op (caller may be in "optional repair" path)
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	// If offset is beyond file size, either:
	// - treat as no-op (truncate bigger than size is meaningless),
	// - OR return error.
	// Here we choose no-op to be repair-friendly.
	if offset >= st.Size() {
		return nil
	}

	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := f.Truncate(offset); err != nil {
		return err
	}

	// Best-effort: sync metadata so the truncation is durable.
	// (On some filesystems, truncation metadata durability matters after crash.)
	_ = f.Sync()
	return nil
}
