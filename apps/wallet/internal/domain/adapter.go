package domain

// 适配器的接口
type ChainAdapter interface {
	// 获取区块的长度
	GetBlockHeight() (int64, error)
	// 获取区块的数据
	GetBlockDeposit(height int64)
}
