package bitcoin

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/txscript"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/shopspring/decimal"
	"gopherex.com/internal/watcher/domain"
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

func (r *Adapter) FetchLog(ctx context.Context, from, to int64, address []string) ([]types.Log, error) {
	return nil, nil
}

// 获取区块的数据
func (r *Adapter) FetchBlock(ctx context.Context, height int64) (*domain.StandardBlock, error) {
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
	stdBllock := domain.StandardBlock{
		Height:       block.Height,
		Hash:         block.Hash,
		PrevHash:     block.PreviousHash,
		Time:         block.Time,
		Transactions: make([]domain.ChainTransfer, 0, len(block.Tx)),
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
			deposit := domain.ChainTransfer{
				TxHash:      tx.Txid,
				LogIndex:    i, // 比特币没有 Log，用 Vout 的索引代替
				Chain:       "BTC",
				Symbol:      "BTC",
				ToAddress:   addressStr,
				FromAddress: "",
				Amount:      amount,
				BlockHeight: height,
				Status:      domain.TransactionStatusPending, // 刚扫到，状态为待确认
			}
			stdBllock.Transactions = append(stdBllock.Transactions, deposit)
		}
	}
	return &stdBllock, nil

}

// GetTransactionStatus 查询 BTC 交易状态
func (a *Adapter) GetTransactionStatus(ctx context.Context, hash string) (domain.TransactionType, error) {
	// 1. 解析 Hash 字符串为 chainhash.Hash 对象
	txHash, err := chainhash.NewHashFromStr(hash)
	if err != nil {
		return domain.TransactionStatusPending, fmt.Errorf("invalid hash: %v", err)
	}

	// 2. 调用 RPC: gettransaction
	// 注意：gettransaction 只能查钱包内的交易（我们发出去的交易肯定在钱包里）
	// 如果是任意交易，需要用 GetRawTransactionVerbose
	txResult, err := a.rpcClinet.GetTransaction(txHash)

	if err != nil {
		// 如果报错包含 "Invalid or non-wallet transaction id"，说明节点没找到这笔交易
		// 可能是还没同步到，或者被丢弃了
		if strings.Contains(err.Error(), "Invalid or non-wallet") {
			return domain.TransactionFailed, nil
		}
		return domain.TransactionFailed, err
	}

	// 3. 判断确认数 (Confirmations)
	// BTC 的逻辑：只要确认数 > 0，就是上链了
	// 生产环境通常要求 >= 1 或 >= 2 才算稳
	if txResult.Confirmations > 0 {
		return domain.TransactionConfirmed, nil
	}

	// 4. 特殊情况：检测是否被“抛弃”或“冲突”
	// Details 里如果 category 是 "conflict" 或者 "abandoned"
	if len(txResult.Details) > 0 {
		for _, detail := range txResult.Details {
			if detail.Category == "conflict" || detail.Category == "abandoned" {
				return domain.TransactionFailed, nil
			}
		}
	}

	// 5. 确认数是 0，且没失败，那就是 Pending
	return domain.TransactionConfirmed, nil
}

// Close 关闭连接 (如有需要)
func (r *Adapter) Close() {
	r.rpcClinet.Shutdown()
}
