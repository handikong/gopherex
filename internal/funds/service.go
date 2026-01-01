package funds

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	fundsv1 "gopherex.com/gen/go/fund_service/v1"
	"gopherex.com/internal/funds/repo"
)

type FundsService struct {
	fundsv1.UnimplementedFundServiceServer
	ctx   context.Context
	cache Cache
	repo  repo.Repo
	sf    singleflight.Group
	ttl   time.Duration
}

func NewFundsService(context context.Context, repo repo.BalancesRepo, cache Cache) *FundsService {
	return &FundsService{
		ctx:   context,
		cache: cache,
		repo:  repo,
		ttl:   2 * time.Hour,
	}
}

func (f *FundsService) GetBalances(ctx context.Context, req *fundsv1.GetBalancesReq) (*fundsv1.GetBalancesRes, error) {
	userID := req.GetUserId()
	asset := req.GetAsset()
	if res, ok, err := f.cache.GetBalances(ctx, userID, asset); err == nil && ok {
		return res, nil
	}
	// 2) singleflight 防击穿
	key := fmt.Sprintf("%d:%s", userID, asset)
	v, err, _ := f.sf.Do(key, func() (interface{}, error) {
		rows, err := f.repo.GetBalances(ctx, userID, asset)
		if err != nil {
			return nil, err
		}
		res := &fundsv1.GetBalancesRes{Balances: make([]*fundsv1.Balance, 0, len(rows))}
		for _, r := range rows {
			res.Balances = append(res.Balances, &fundsv1.Balance{
				UserId: r.OwnerID,
				Asset:  r.Asset,
				Bucket: r.Bucket,
				Amount: r.Amount,
			})
		}
		_ = f.cache.SetBalances(ctx, userID, asset, res, f.ttl)
		cloneRes := cloneGetBalancesRes(res)
		return cloneRes, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return v.(*fundsv1.GetBalancesRes), nil
}

func (f *FundsService) Reserve(ctx context.Context, req *fundsv1.ReserveReq) (*fundsv1.ReserveResp, error) {
	//TODO implement me
	panic("implement me")
}

func (f *FundsService) Release(ctx context.Context, req *fundsv1.ReleaseReq) (*fundsv1.ReleaseResp, error) {
	//TODO implement me
	panic("implement me")
}

func (f *FundsService) SettleTrade(ctx context.Context, req *fundsv1.SettleTradeReq) (*fundsv1.SettleTradeResp, error) {
	//TODO implement me
	panic("implement me")
}
