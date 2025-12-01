// 钱包功能
package hdwallet

import (
	"fmt"
	"testing"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/stretchr/testify/assert"
)

func TestHDWallet_DeriveAddress(t *testing.T) {
	mnemonic := "test test test test test test test test test test test junk"
	wallet, err := New(mnemonic, &chaincfg.MainNetParams)
	assert.NoError(t, err)
	address, privateKeyHex, err := wallet.DeriveAddress(0, 1500)
	assert.NoError(t, err)
	assert.NotEmpty(t, address)
	assert.NotEmpty(t, privateKeyHex)

	_, _, err1 := wallet.DeriveAddress(50, 1500)
	assert.NotNil(t, err1)

	address4, _, err4 := wallet.DeriveAddress(2, 1500)
	assert.NotNil(t, err4)
	assert.Empty(t, address4)

	// 第二次再用助词器生成 是不是一样的
	wallet1, err := New(mnemonic, &chaincfg.MainNetParams)
	assert.NoError(t, err)
	address2, privateKeyHex2, err2 := wallet1.DeriveAddress(0, 1500)
	assert.NoError(t, err2)

	assert.Equal(t, address, address2)
	assert.Equal(t, privateKeyHex, privateKeyHex2)

	t.Log(address)
	fmt.Println(address)
}
