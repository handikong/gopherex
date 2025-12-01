package bitcoin

import (
	"context"
	"encoding/hex"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/txscript"
	"github.com/shopspring/decimal"
	"gopherex.com/apps/wallet/internal/domain"
)

// 时间btc的逻辑
type Adapter struct {
	// btc的链接
	rpcClinet   *rpcclient.Client
	networkType *chaincfg.Params
}

// 编译时检查：确保 Adapter 实现了 domain.ChainAdapter 接口
var _ domain.ChainAdapter = (*Adapter)(nil)

// 创建这个类型
func New(host, user, password string, network *chaincfg.Params) (*Adapter, error) {
	// 链接服务器
	rpcConfig := &rpcclient.ConnConfig{
		Host:         host,
		User:         user,
		Pass:         password,
		HTTPPostMode: true, // 比特币核心节点必须使用 POST 模式
		DisableTLS:   true, // 本地 Docker 环境通常不加密，一定要关掉 TLS
	}
	client, err := rpcclient.New(rpcConfig, nil)
	if err != nil {
		return nil, err
	}
	return &Adapter{
		rpcClinet:   client,
		networkType: network,
	}, nil
}

// // 实现接口
// 获取区块的长度
func (r *Adapter) GetBlockHeight(ctx context.Context) (int64, error) {
	return r.rpcClinet.GetBlockCount()
}

// 获取区块的数据
func (r *Adapter) FetchBlock(ctx context.Context, height int64) (*domain.StandarBlock, error) {
	// 获取所有的区块
	hash, err := r.rpcClinet.GetBlockHash(height)
	if err != nil {
		return nil, err
	}
	// 根据hash获取区块的所有交易
	block, err := r.rpcClinet.GetBlockVerboseTx(hash)
	if err != nil {
		return nil, err
	}
	// 定义我们标准的快
	stdBllock := domain.StandarBlock{
		Height:       block.Height,
		Hash:         block.Hash,
		PrevHash:     block.PreviousHash,
		Time:         block.Time,
		Transactions: make([]domain.Deposit, 0, len(block.Tx)),
	}

	// 循环交易 去判断
	for i, tx := range block.Tx {
		// 充值逻辑只关心vout的数据
		for _, vout := range tx.Vout {
			//  解析hdex
			pkScriptBytes, err := hex.DecodeString(vout.ScriptPubKey.Hex)
			if err != nil {
				continue // 解析失败，跳过
			}

			// 2. 提取脚本类型和地址
			// ExtractPkScriptAddrs 会自动识别 P2PKH, P2SH, P2WPKH 等各种格式
			// 需要传入当前网络的参数 (RegressionNetParams / TestNet3Params / MainNetParams)
			_, addrs, _, err := txscript.ExtractPkScriptAddrs(pkScriptBytes, r.networkType)
			if err != nil {
				continue
			}
			// 3. 如果提取不到地址 (比如 OP_RETURN)，跳过
			if len(addrs) == 0 {
				continue
			}
			// 4. 取第一个地址 (通常也就是唯一的一个)
			addressStr := addrs[0].EncodeAddress()

			// 提取金额 需要转化为decima
			amount := decimal.NewFromFloat(vout.Value)
			// 组装充值记录
			deposit := domain.Deposit{
				TxHash:      tx.Txid,
				LogIndex:    i, // 比特币没有 Log，用 Vout 的索引代替
				Chain:       "BTC",
				Symbol:      "BTC",
				ToAddress:   addressStr,
				Amount:      amount,
				BlockHeight: height,
				Status:      domain.DepositStatusPending, // 刚扫到，状态为待确认
			}
			stdBllock.Transactions = append(stdBllock.Transactions, deposit)
		}
	}

	return &stdBllock, nil

}

// Close 关闭连接 (如有需要)
func (r *Adapter) Close() {
	r.rpcClinet.Shutdown()
}
