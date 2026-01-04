package interfaces

import (
	"context"
	"log"
	"math/big"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/shopspring/decimal"
	"golang.org/x/time/rate"
)

type SoloChair struct {
	cluster *rpc.Cluster
	rpc     *rpc.Client
}

func NewSolana(cluster *rpc.Cluster) *SoloChair {
	rpcClient := rpc.NewWithCustomRPCClient(rpc.NewWithLimiter(
		cluster.RPC,
		rate.Every(time.Second), // time frame
		5,                       // limit of requests per time frame
	))
	return &SoloChair{
		cluster: cluster,
		rpc:     rpcClient,
	}
}

func (r *SoloChair) GetHeight(ctx context.Context) (uint64, error) {
	var types rpc.CommitmentType = rpc.CommitmentConfirmed
	return r.rpc.GetBlockHeight(ctx, types)
}

func (r *SoloChair) GetBlockByHeight(ctx context.Context, height uint64) (*StandardBlock, error) {
	block, err := r.rpc.GetBlockWithOpts(ctx, height, &rpc.GetBlockOpts{
		Commitment:         rpc.CommitmentConfirmed,
		TransactionDetails: rpc.TransactionDetailsFull,
		Encoding:           solana.EncodingBase64,
	})
	if err != nil {
		return nil, err
	}

	blockHeight := int64(height)
	if block.BlockHeight != nil {
		blockHeight = int64(*block.BlockHeight)
	}

	blockTime := int64(0)
	if block.BlockTime != nil {
		blockTime = block.BlockTime.Time().Unix()
	}
	log.Printf("block is %+v\n", block)

	standardBlock := &StandardBlock{
		Height:       blockHeight,
		Hash:         block.Blockhash.String(),
		PrevHash:     block.PreviousBlockhash.String(),
		Time:         blockTime,
		Transactions: make([]ChainTransfer, 0, len(block.Transactions)),
	}
	for _, tx := range block.Transactions {
		solanaTx, err := tx.GetTransaction()
		if err != nil {
			log.Printf("failed to decode solana transaction: %v", err)
			continue
		}

		txHash := ""
		if len(solanaTx.Signatures) > 0 {
			txHash = solanaTx.Signatures[0].String()
		}

		status := TransactionStatusPending
		if tx.Meta != nil {
			if tx.Meta.Err == nil {
				status = TransactionConfirmed
			} else {
				status = TransactionFailed
			}
		}

		transfers := extractSolTransfers(solanaTx, blockHeight)
		if len(transfers) == 0 {
			standardBlock.Transactions = append(standardBlock.Transactions, ChainTransfer{
				TxHash:      txHash,
				LogIndex:    0,
				BlockHeight: blockHeight,
				Chain:       "SOL",
				Symbol:      "SOL",
				Amount:      decimal.Zero,
				Status:      status,
			})
			continue
		}

		for _, transfer := range transfers {
			transfer.TxHash = txHash
			transfer.BlockHeight = blockHeight
			transfer.Status = status
			standardBlock.Transactions = append(standardBlock.Transactions, transfer)
		}
	}

	return standardBlock, nil
}

func extractSolTransfers(tx *solana.Transaction, blockHeight int64) []ChainTransfer {
	transfers := make([]ChainTransfer, 0)

	for _, inst := range tx.Message.Instructions {
		if int(inst.ProgramIDIndex) >= len(tx.Message.AccountKeys) {
			continue
		}

		accounts, err := inst.ResolveInstructionAccounts(&tx.Message)
		if err != nil {
			continue
		}

		programID := tx.Message.AccountKeys[inst.ProgramIDIndex]
		decoded, err := solana.DecodeInstruction(programID, accounts, inst.Data)
		if err != nil {
			continue
		}

		sysInst, ok := decoded.(*system.Instruction)
		if !ok {
			continue
		}

		switch inner := sysInst.Impl.(type) {
		case *system.Transfer:
			if inner.Lamports == nil {
				continue
			}

			transfers = append(transfers, ChainTransfer{
				LogIndex:    0,
				BlockHeight: blockHeight,
				FromAddress: accountMetaString(inner.GetFundingAccount()),
				ToAddress:   accountMetaString(inner.GetRecipientAccount()),
				Chain:       "SOL",
				Symbol:      "SOL",
				Amount:      lamportsToDecimal(*inner.Lamports),
			})
		case *system.TransferWithSeed:
			if inner.Lamports == nil {
				continue
			}

			transfers = append(transfers, ChainTransfer{
				LogIndex:    0,
				BlockHeight: blockHeight,
				FromAddress: accountMetaString(inner.GetFundingAccount()),
				ToAddress:   accountMetaString(inner.GetRecipientAccount()),
				Chain:       "SOL",
				Symbol:      "SOL",
				Amount:      lamportsToDecimal(*inner.Lamports),
			})
		}
	}

	return transfers
}

func lamportsToDecimal(lamports uint64) decimal.Decimal {
	return decimal.NewFromBigInt(new(big.Int).SetUint64(lamports), -9)
}

func accountMetaString(meta *solana.AccountMeta) string {
	if meta == nil {
		return ""
	}
	return meta.PublicKey.String()
}
