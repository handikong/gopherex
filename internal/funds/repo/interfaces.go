package repo

import (
	"context"

	"gopherex.com/internal/funds/repo/model"
)

type BalancesRepo interface {
	GetBalances(ctx context.Context, userID uint64, asset string) ([]model.BalanceRow, error)
}

type Repo interface {
	BalancesRepo
}
