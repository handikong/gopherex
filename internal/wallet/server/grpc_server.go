package server

import (
	"context"

	pb "gopherex.com/api/wallet/v1"
	"gopherex.com/internal/wallet/domain"
	"gopherex.com/internal/wallet/service"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// 将service和grpc进行绑定
type GrpcServer struct {
	pb.UnimplementedWalletServer
	rechargeSrv *service.RechargeService
	withdrawSrv *service.WithdrawService
}

// 构造函数
func NewGrpcServer(r *service.RechargeService, w *service.WithdrawService) *GrpcServer {
	return &GrpcServer{
		rechargeSrv: r,
		withdrawSrv: w,
	}
}

func (s *GrpcServer) GetRechargeListById(ctx context.Context, req *pb.RechargeListReq) (*pb.RechargeListResp, error) {
	lists, err := s.rechargeSrv.GetListById(ctx, req.Uid, req.Chain,
		req.Symbol, toDomainRechargeStatus(req.Status), int(req.Page), int(req.Limit))

	if err != nil {
		return nil, err
	}

	items := make([]*pb.RechargeListItem, 0, len(lists))
	for _, r := range lists {
		item := &pb.RechargeListItem{
			TxHash:      r.TxHash,
			FromAddress: r.FromAddress,
			ToAddress:   r.ToAddress,
			ToUid:       int64(r.ToUid),
			Chain:       r.Chain,
			Symbol:      r.Symbol,
			Amount:      r.Amount.String(),
			Status:      toPBRechargeStatus(r.Status),
			ErrorMsg:    r.ErrorMsg,
			BlockHeight: r.BlockHeight,
			LogIndex:    int32(r.LogIndex),
			CreatedAt:   timestamppb.New(r.CreatedAt),
		}
		items = append(items, item)
	}

	return &pb.RechargeListResp{
		Items: items,
		Total: int64(len(items)),
	}, nil

}

func (s *GrpcServer) ApplyWithdraw(ctx context.Context, req *pb.ApplyWithdrawReq) (*pb.ApplyWithdrawResp, error) {
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil {
		return nil, err
	}

	if err := s.withdrawSrv.ApplyWithdraw(ctx, req.Uid, req.Chain, req.Symbol, req.ToAddress, amount, req.RequestId); err != nil {
		return nil, err
	}

	return &pb.ApplyWithdrawResp{
		Success: true,
	}, nil
}

// toDomainRechargeStatus 将 proto 枚举转成领域枚举
func toDomainRechargeStatus(status pb.RechargeStatus) domain.RechargeType {
	switch status {
	case pb.RechargeStatus_RECHARGE_STATUS_CONFIRMED:
		return domain.RechargeStatusConfirmed
	case pb.RechargeStatus_RECHARGE_STATUS_FAILED:
		return domain.RechargeStatusFailed
	default:
		return domain.RechargeStatusPending
	}
}

// toPBRechargeStatus 将领域枚举转成 proto 枚举
func toPBRechargeStatus(status domain.RechargeType) pb.RechargeStatus {
	switch status {
	case domain.RechargeStatusConfirmed:
		return pb.RechargeStatus_RECHARGE_STATUS_CONFIRMED
	case domain.RechargeStatusFailed:
		return pb.RechargeStatus_RECHARGE_STATUS_FAILED
	case domain.RechargeStatusPending:
		return pb.RechargeStatus_RECHARGE_STATUS_PENDING
	default:
		return pb.RechargeStatus_RECHARGE_STATUS_UNSPECIFIED
	}
}
