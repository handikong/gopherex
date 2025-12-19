package engine

import (
	"errors"
)

// 定义变量

// 定义命令类型
type CmdType uint8

const (
	CmdSubmitLimit CmdType = iota + 1 // 提交
	CmdCancel                         // 取消
)

const (
	Buy = iota + 1
	Sell
)

// V0：命令是“入队即返回”，业务结果一定通过事件返回（不做同步等待）
type Command struct {
	Type     CmdType
	ReqID    uint64 // 上游幂等/追踪用
	ClientTs int64  // 可选：审计

	// SubmitLimit fields
	OrderID       uint64 // OrderId 为什么不是内部生成
	UserID        uint64
	Side          uint8
	Price         int64
	Qty           int64
	CancelOrderID uint64 // 取消订单ID
}
type EventType uint8

const (
	EvAccepted  EventType = iota + 1 // 成功
	EvRejected                       //失败
	EvAdded                          //加入订单薄
	EvCancelled                      //订单薄取消
	EvTrade                          // 交易成功
)

type Event struct {
	Type EventType

	// 关键：同一 symbol Actor 内单调递增，用于对齐/回放/排查
	Seq   uint64
	ReqID uint64
	Idx   uint16 // 新增：同一 cmdSeq 内事件序号

	// 通用字段
	OrderID uint64
	UserID  uint64

	// Trade 字段
	MakerOrderID uint64
	TakerOrderID uint64
	Price        int64
	Qty          int64

	// 非热路径：拒单原因（V1 可以改成 code）
	Reason string
}

// 定义错误
var (
	ErrEngineBusy = errors.New("engine busy: mailbox full")
	ErrUnknownSym = errors.New("unknown symbol")
	ErrBadCommand = errors.New("bad command")
)
