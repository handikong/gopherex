package wal

import (
	"bufio"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"
)

type ReaderOptions struct {
	MaxPayload         int  //最大长度
	AllowTruncatedTail bool //是否允许丢弃
	BufferSize         int  // buf的长度
}
type Reader struct {
	f   *os.File
	br  *bufio.Reader
	off int64

	maxPayload int
	allowTail  bool

	truncatedTail  bool
	lastGoodOffset int64
}

// 开始读数据
// path 地址
// offset 开始读的地址
func OpenReader(path string, offset int64, opts ReaderOptions) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			_ = f.Close()
			return nil, err
		}
	}
	if opts.BufferSize <= 0 {
		opts.BufferSize = 1 << 20 // 1m
	}
	maxPayload := opts.MaxPayload
	if maxPayload <= 0 {
		maxPayload = DefaultMaxPayload
	}
	return &Reader{
		f:              f,
		br:             bufio.NewReaderSize(f, opts.BufferSize),
		off:            offset,
		maxPayload:     maxPayload,
		allowTail:      false,
		truncatedTail:  false,
		lastGoodOffset: offset, // 已经偏移了
	}, nil
}
func (r *Reader) Close() error { return r.f.Close() }

func (r *Reader) TruncatedTail() bool   { return r.truncatedTail }
func (r *Reader) LastGoodOffset() int64 { return r.lastGoodOffset }

func (r *Reader) Next() (payload []byte, nextOffset int64, err error) {
	var hdr [headerSize]byte
	_, err = io.ReadFull(r.br, hdr[:])
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, r.off, io.EOF
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			r.truncatedTail = true
			if r.allowTail {
				return nil, r.off, io.EOF
			}
			return nil, r.off, ErrCorruptHeader
		}
		return nil, r.off, err
	}

	ln := int(binary.LittleEndian.Uint32(hdr[0:4]))
	crc := binary.LittleEndian.Uint32(hdr[4:8])

	if ln < 0 || ln > r.maxPayload {
		return nil, r.off, ErrPayloadTooLarge
	}

	payload = make([]byte, ln)
	_, err = io.ReadFull(r.br, payload)
	if err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			r.truncatedTail = true
			if r.allowTail {
				return nil, r.off, io.EOF
			}
			return nil, r.off, ErrCorruptPayload
		}
		return nil, r.off, err
	}

	if crc32.ChecksumIEEE(payload) != crc {
		return nil, r.off, ErrChecksumMismatch
	}

	r.off += int64(headerSize + ln)
	r.lastGoodOffset = r.off
	return payload, r.off, nil
}
