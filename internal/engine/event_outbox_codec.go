package engine

import (
	"encoding/binary"
	"errors"
)

const (
	evWalVersion = 1
	evRecordLen  = 68

	evOffVer   = 0
	evOffType  = 1  // uint8
	evOffSeq   = 2  // uint64
	evOffIdx   = 10 // uint16
	evOffReqID = 12 // uint64
	evOffOrder = 20 // uint64
	evOffUser  = 28 // uint64
	evOffMaker = 36 // uint64
	evOffTaker = 44 // uint64
	evOffPrice = 52 // int64 as uint64
	evOffQty   = 60 // int64 as uint64
)

var (
	ErrBadEvRecordLen = errors.New("outbox: bad record length")
	ErrBadEvVersion   = errors.New("outbox: bad version")
)

// 约定一个“不会和正常事件冲突”的类型：命令结束标记
const EvCmdEnd EventType = 250

type EvCmdCodec struct{}

func (e EvCmdCodec) Encode(dst []byte, ev Event) ([]byte, error) {
	if cap(dst) < evRecordLen {
		dst = make([]byte, evRecordLen)
	} else {
		dst = dst[:evRecordLen]
	}

	dst[evOffVer] = byte(evWalVersion)
	dst[evOffType] = byte(ev.Type)

	binary.LittleEndian.PutUint64(dst[evOffSeq:evOffSeq+8], ev.Seq)
	binary.LittleEndian.PutUint16(dst[evOffIdx:evOffIdx+2], ev.Idx)
	binary.LittleEndian.PutUint64(dst[evOffReqID:evOffReqID+8], ev.ReqID)

	binary.LittleEndian.PutUint64(dst[evOffOrder:evOffOrder+8], ev.OrderID)
	binary.LittleEndian.PutUint64(dst[evOffUser:evOffUser+8], ev.UserID)
	binary.LittleEndian.PutUint64(dst[evOffMaker:evOffMaker+8], ev.MakerOrderID)
	binary.LittleEndian.PutUint64(dst[evOffTaker:evOffTaker+8], ev.TakerOrderID)

	binary.LittleEndian.PutUint64(dst[evOffPrice:evOffPrice+8], uint64(ev.Price))
	binary.LittleEndian.PutUint64(dst[evOffQty:evOffQty+8], uint64(ev.Qty))
	return dst, nil
}

func (e EvCmdCodec) Decode(payload []byte) (Event, error) {
	if len(payload) != evRecordLen {
		return Event{}, ErrBadEvRecordLen
	}
	if int(payload[evOffVer]) != evWalVersion {
		return Event{}, ErrBadEvVersion
	}

	var ev Event
	ev.Type = EventType(payload[evOffType])
	ev.Seq = binary.LittleEndian.Uint64(payload[evOffSeq : evOffSeq+8])
	ev.Idx = binary.LittleEndian.Uint16(payload[evOffIdx : evOffIdx+2])
	ev.ReqID = binary.LittleEndian.Uint64(payload[evOffReqID : evOffReqID+8])

	ev.OrderID = binary.LittleEndian.Uint64(payload[evOffOrder : evOffOrder+8])
	ev.UserID = binary.LittleEndian.Uint64(payload[evOffUser : evOffUser+8])
	ev.MakerOrderID = binary.LittleEndian.Uint64(payload[evOffMaker : evOffMaker+8])
	ev.TakerOrderID = binary.LittleEndian.Uint64(payload[evOffTaker : evOffTaker+8])

	ev.Price = int64(binary.LittleEndian.Uint64(payload[evOffPrice : evOffPrice+8]))
	ev.Qty = int64(binary.LittleEndian.Uint64(payload[evOffQty : evOffQty+8]))
	return ev, nil
}
