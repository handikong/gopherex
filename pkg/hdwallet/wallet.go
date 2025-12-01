// 钱包功能
package hdwallet

import (
	"errors"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/btcutil/hdkeychain"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/tyler-smith/go-bip39"
)

type HDWallet struct {
	// 主私钥
	masterKey *hdkeychain.ExtendedKey
	// 网络地址
	btcParams *chaincfg.Params
}

// 实例化结构
// 传递一个助词器和网络
func New(mnemonic string, netParams *chaincfg.Params) (*HDWallet, error) {
	if mnemonic == "" {
		return nil, errors.New("mnemonic cannot empty ")
	}
	// 根据助词器生成随机种子
	seed := bip39.NewSeed(mnemonic, "")
	// 生成根私钥
	extendKey, err := hdkeychain.NewMaster(seed, netParams)
	if err != nil {
		return nil, err
	}
	return &HDWallet{
		masterKey: extendKey,
		btcParams: netParams,
	}, nil
}

func (w *HDWallet) DeriveAddress(coinType uint32, accountIdx uint32) (string, string, error) {
	// 按照bip44的派生地址  生成对应地址
	// BIP44 路径: m / 44' / coin_type' / 0' / 0 / account_index
	// 1. 逐步推导 (Hardened = +0x80000000)
	path := []uint32{
		44 + hdkeychain.HardenedKeyStart,       //Purpose
		coinType + hdkeychain.HardenedKeyStart, // CoinType
		0 + hdkeychain.HardenedKeyStart,        // Account (交易所总账户)
		0,
		accountIdx,
	}
	// 循环逐步推动地址
	key := w.masterKey
	var err error
	for _, idx := range path {
		key, err = key.Derive(idx)
		if err != nil {
			return "", "", err
		}
	}
	// 通过不同的来源 生成不同的地址
	privKey, err := key.ECPrivKey()
	if err != nil {
		return "", "", err
	}
	// 导出私钥Hex (仅用于调试或归集服务，不要返回给前端！)
	// 在这里我们返回它方便 Service 层做冷热分离处理(如果有的话)
	privateKeyHex := fmt.Sprintf("%x", privKey.Serialize())
	// 3. 获取公钥
	address, err := w.GetAddress(coinType, privKey)
	if err != nil {
		return "", "", err
	}
	return address, privateKeyHex, nil

}

func (w *HDWallet) GetAddress(coinType uint32, privKey *btcec.PrivateKey) (string, error) {
	var address string
	switch coinType {
	case 0: // // 生成 SegWit 地址 (p2wpkh) - 最省钱
		publicKeyHash, err := btcutil.NewAddressWitnessPubKeyHash(
			btcutil.Hash160(privKey.PubKey().SerializeCompressed()),
			w.btcParams,
		)
		if err != nil {
			return "", err
		}
		address = publicKeyHash.EncodeAddress()
	case 60:
		// 转换成 ECDSA 私钥
		ethPrivateKey := privKey.ToECDSA()
		addr := crypto.PubkeyToAddress(ethPrivateKey.PublicKey)
		address = addr.Hex()
	default:
		return "", errors.New("invalid coin type")
	}
	return address, nil
}
