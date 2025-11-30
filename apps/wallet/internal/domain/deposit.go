package domain

import "math/big"

type HashStr []byte
type Deposit struct {
	TxHash   HashStr // hash地址
	From     string  // 充值到什么账户
	Amount   big.Int // 充值金额
	Status   uint8   // 充值状态
	ErrorMsg string  // 充值失败原因
	TimeStap uint    // 充值时间
}
