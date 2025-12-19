package engine

import (
	"encoding/binary"
	"errors"
)

const (
	cmdWalVersion = 1
	cmdRecordLen  = 67

	offVer      = 0
	offType     = 1
	offSeq      = 2  // uint64
	offReqID    = 10 // uint64
	offClientTs = 18 // int64 as uint64
	offOrderID  = 26 // uint64
	offUserID   = 34 // uint64
	offSide     = 42 // uint8
	offPrice    = 43 // int64 as uint64
	offQty      = 51 // int64 as uint64
	offCancelID = 59 // uint64
)

var (
	ErrBadCmdRecordLen = errors.New("wal cmd: bad record length")
	ErrBadCmdVersion   = errors.New("wal cmd: bad version")
	ErrBadCmdType      = errors.New("wal cmd: bad cmd type")
)

type BinaryCMDCode struct {
}

func (b BinaryCMDCode) Encode(dst []byte, cmdSeq uint64, cmd Command) ([]byte, error) {
	if cap(dst) < cmdRecordLen {
		dst = make([]byte, cmdRecordLen)
	} else {
		dst = dst[:cmdRecordLen]
	}

	dst[offVer] = byte(cmdWalVersion)
	dst[offType] = byte(cmd.Type)

	binary.LittleEndian.PutUint64(dst[offSeq:offSeq+8], cmdSeq)
	binary.LittleEndian.PutUint64(dst[offReqID:offReqID+8], cmd.ReqID)
	binary.LittleEndian.PutUint64(dst[offClientTs:offClientTs+8], uint64(cmd.ClientTs))

	binary.LittleEndian.PutUint64(dst[offOrderID:offOrderID+8], cmd.OrderID)
	binary.LittleEndian.PutUint64(dst[offUserID:offUserID+8], cmd.UserID)

	dst[offSide] = byte(cmd.Side)

	binary.LittleEndian.PutUint64(dst[offPrice:offPrice+8], uint64(cmd.Price))
	binary.LittleEndian.PutUint64(dst[offQty:offQty+8], uint64(cmd.Qty))

	var cancelID uint64
	if cmd.Type == CmdCancel {
		cancelID = cmd.CancelOrderID
	}
	binary.LittleEndian.PutUint64(dst[offCancelID:offCancelID+8], cancelID)

	return dst, nil
}

func (B BinaryCMDCode) Decode(payload []byte) (cmdSeq uint64, cmd Command, err error) {
	if len(payload) != cmdRecordLen {
		return 0, Command{}, ErrBadCmdRecordLen
	}
	if int(payload[offVer]) != cmdWalVersion {
		return 0, Command{}, ErrBadCmdVersion
	}

	ct := CmdType(payload[offType])
	if ct != CmdSubmitLimit && ct != CmdCancel {
		return 0, Command{}, ErrBadCmdType
	}

	cmdSeq = binary.LittleEndian.Uint64(payload[offSeq : offSeq+8])

	cmd.Type = ct
	cmd.ReqID = binary.LittleEndian.Uint64(payload[offReqID : offReqID+8])
	cmd.ClientTs = int64(binary.LittleEndian.Uint64(payload[offClientTs : offClientTs+8]))

	cmd.OrderID = binary.LittleEndian.Uint64(payload[offOrderID : offOrderID+8])
	cmd.UserID = binary.LittleEndian.Uint64(payload[offUserID : offUserID+8])

	cmd.Side = payload[offSide]

	cmd.Price = int64(binary.LittleEndian.Uint64(payload[offPrice : offPrice+8]))
	cmd.Qty = int64(binary.LittleEndian.Uint64(payload[offQty : offQty+8]))

	cmd.CancelOrderID = binary.LittleEndian.Uint64(payload[offCancelID : offCancelID+8])

	return cmdSeq, cmd, nil
}
