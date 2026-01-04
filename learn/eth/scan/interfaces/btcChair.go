package interfaces

import (
	"context"
	"encoding/hex"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/txscript"
	"github.com/shopspring/decimal"
)

type BtcChair struct {
	// btc的链接
	rpcClient   *rpcclient.Client
	networkType *chaincfg.Params
}

func NewBtcChair(r *rpcclient.Client, networkType *chaincfg.Params) Chair {
	return &BtcChair{rpcClient: r, networkType: networkType}
}

func (b BtcChair) GetHeight(ctx context.Context) (uint64, error) {
	count, err := b.rpcClient.GetBlockCount()
	return uint64(count), err

}

func (b BtcChair) GetBlockByHeight(ctx context.Context, height uint64) (*StandardBlock, error) {
	// 获取所有的区块
	hash, err := b.rpcClient.GetBlockHash(int64(height))
	if err != nil {
		return nil, err
	}
	// 根据hash获取区块的所有交易
	block, err := b.rpcClient.GetBlockVerboseTx(hash)
	if err != nil {
		return nil, err
	}
	// 定义我们标准的快
	stdBllock := StandardBlock{
		Height:       block.Height,
		Hash:         block.Hash,
		PrevHash:     block.PreviousHash,
		Time:         block.Time,
		Transactions: make([]ChainTransfer, 0, len(block.Tx)),
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
			_, addrs, _, err := txscript.ExtractPkScriptAddrs(pkScriptBytes, b.networkType)
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
			deposit := ChainTransfer{
				TxHash:      tx.Txid,
				LogIndex:    i, // 比特币没有 Log，用 Vout 的索引代替
				Chain:       "BTC",
				Symbol:      "BTC",
				ToAddress:   addressStr,
				FromAddress: "",
				Amount:      amount,
				BlockHeight: int64(height),
				Status:      TransactionStatusPending, // 刚扫到，状态为待确认
			}
			stdBllock.Transactions = append(stdBllock.Transactions, deposit)
		}
	}
	return &stdBllock, nil
}
