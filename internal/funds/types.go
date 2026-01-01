package funds

import fundsv1 "gopherex.com/gen/go/fund_service/v1"

const (
	OwnerUser   = uint8(1)
	OwnerSystem = uint8(2)
)

// clone 避免上层修改返回对象影响缓存/并发
func cloneGetBalancesRes(in *fundsv1.GetBalancesRes) *fundsv1.GetBalancesRes {
	if in == nil {
		return &fundsv1.GetBalancesRes{}
	}
	out := &fundsv1.GetBalancesRes{
		Balances: make([]*fundsv1.Balance, 0, len(in.GetBalances())),
	}
	for _, b := range in.GetBalances() {
		if b == nil {
			continue
		}
		out.Balances = append(out.Balances, &fundsv1.Balance{
			UserId: b.GetUserId(),
			Asset:  b.GetAsset(),
			Bucket: b.GetBucket(),
			Amount: b.GetAmount(),
		})
	}
	return out
}
