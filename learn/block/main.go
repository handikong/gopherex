package main

import (
	"crypto/sha256"
	"fmt"
	"time"
)

type Block struct {
	TimeStamp    int64
	Transactions string
	PrevHash     string
	Hash         string
}

// 生成hash
func (b *Block) calculateHash() {
	data := fmt.Sprintf("%d%s%s", b.TimeStamp, b.Transactions, b.PrevHash)
	hash := sha256.Sum256([]byte(data))
	b.Hash = fmt.Sprintf("%x", hash)
}

func main() {
	genesisBlock := Block{
		TimeStamp:    time.Now().UnixNano(),
		Transactions: "Genesis Block",
		PrevHash:     "",
	}
	genesisBlock.calculateHash()
	secondBlock := &Block{
		TimeStamp:    time.Now().Unix(),
		Transactions: "Second Block Transactions",
		PrevHash:     genesisBlock.Hash,
	}
	secondBlock.calculateHash()
	fmt.Printf("Block hash: %x\n", genesisBlock.Hash)
}
