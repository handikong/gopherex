package server

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	pb "gopherex.com/api/user/v1"
	"gopherex.com/internal/user/domain"
	"gopherex.com/internal/user/service"
	"gopherex.com/pkg/xerr"
)

// GrpcServer 是 gRPC 的接入层
type GrpcServer struct {
	// 必须嵌入这个，这是 protoc 生成的默认实现（向前兼容）
	pb.UnimplementedUserServer

	// 注入你的业务逻辑 Service
	userSvc *service.UserService
}

// NewGrpcServer 构造函数
func NewGrpcServer(userSvc *service.UserService) *GrpcServer {
	return &GrpcServer{
		userSvc: userSvc,
	}
}

// Register 实现注册接口
func (s *GrpcServer) Register(ctx context.Context, req *pb.RegisterReq) (*pb.RegisterResp, error) {
	var a = []int{1, 2, 3}
	fmt.Println(a[4])
	// 1. 调用业务 Service
	user, err := s.userSvc.CreateUser(ctx, req.Username, req.Email, req.Phone, req.Password)
	if err != nil {
		return nil, s.convertError(err)
	}

	// 2. 获取包含地址的用户信息
	userWithAddr, err := s.userSvc.GetUserByID(ctx, user.ID)
	if err != nil {
		return nil, s.convertError(err)
	}

	// 3. 转换数据
	return &pb.RegisterResp{
		User: s.convertUserToProto(userWithAddr),
	}, nil
}

// Login 实现登录接口
func (s *GrpcServer) Login(ctx context.Context, req *pb.LoginReq) (*pb.LoginResp, error) {
	userWithAddr, err := s.userSvc.Login(ctx, req.Account, req.Password, req.Ip)
	if err != nil {
		return nil, s.convertError(err)
	}

	return &pb.LoginResp{
		User: s.convertUserToProto(userWithAddr),
	}, nil
}

// UpdatePassword 实现修改密码接口
func (s *GrpcServer) UpdatePassword(ctx context.Context, req *pb.UpdatePasswordReq) (*pb.UpdatePasswordResp, error) {
	err := s.userSvc.UpdatePassword(ctx, req.UserId, req.OldPassword, req.NewPassword)
	if err != nil {
		return nil, s.convertError(err)
	}

	return &pb.UpdatePasswordResp{
		Success: true,
	}, nil
}

// GetUserInfo 实现获取用户信息接口
func (s *GrpcServer) GetUserInfo(ctx context.Context, req *pb.GetUserInfoReq) (*pb.GetUserInfoResp, error) {
	var userWithAddr *service.UserWithAddresses
	var err error

	// 根据 oneof 查询类型调用不同的 service 方法
	switch q := req.Query.(type) {
	case *pb.GetUserInfoReq_UserId:
		userWithAddr, err = s.userSvc.GetUserByID(ctx, q.UserId)
	case *pb.GetUserInfoReq_Username:
		userWithAddr, err = s.userSvc.GetUserByUsername(ctx, q.Username)
	case *pb.GetUserInfoReq_Email:
		userWithAddr, err = s.userSvc.GetUserByEmail(ctx, q.Email)
	case *pb.GetUserInfoReq_Phone:
		userWithAddr, err = s.userSvc.GetUserByPhone(ctx, q.Phone)
	default:
		return nil, status.Error(codes.InvalidArgument, "query field is required")
	}

	if err != nil {
		return nil, s.convertError(err)
	}

	return &pb.GetUserInfoResp{
		User: s.convertUserToProto(userWithAddr),
	}, nil
}

// UpdateUserStatus 实现更新用户状态接口
func (s *GrpcServer) UpdateUserStatus(ctx context.Context, req *pb.UpdateUserStatusReq) (*pb.UpdateUserStatusResp, error) {
	var err error

	switch req.Status {
	case pb.UserStatus_USER_STATUS_ENABLED:
		err = s.userSvc.EnableUser(ctx, req.UserId)
	case pb.UserStatus_USER_STATUS_DISABLED:
		err = s.userSvc.DisableUser(ctx, req.UserId)
	default:
		return nil, status.Error(codes.InvalidArgument, "invalid user status")
	}

	if err != nil {
		return nil, s.convertError(err)
	}

	return &pb.UpdateUserStatusResp{
		Success: true,
	}, nil
}

// UpdateUserProfile 实现更新用户信息接口
func (s *GrpcServer) UpdateUserProfile(ctx context.Context, req *pb.UpdateUserProfileReq) (*pb.UpdateUserProfileResp, error) {
	// 1. 先获取用户信息
	userWithAddr, err := s.userSvc.GetUserByID(ctx, req.UserId)
	if err != nil {
		return nil, s.convertError(err)
	}

	// 2. 更新需要修改的字段
	user := userWithAddr.User
	if req.Email != "" {
		user.Email = req.Email
	}
	if req.Phone != "" {
		user.Phone = req.Phone
	}

	// 3. 调用更新服务
	err = s.userSvc.UpdateUser(ctx, user)
	if err != nil {
		return nil, s.convertError(err)
	}

	return &pb.UpdateUserProfileResp{
		Success: true,
	}, nil
}

// GetUserByAddress 实现根据地址获取用户ID接口（内部服务调用）
func (s *GrpcServer) GetUserByAddress(ctx context.Context, req *pb.GetUserByAddressReq) (*pb.GetUserByAddressResp, error) {
	userID, err := s.userSvc.GetUserIDByAddress(ctx, req.Address)
	if err != nil {
		return nil, s.convertError(err)
	}

	return &pb.GetUserByAddressResp{
		UserId: userID,
	}, nil
}

// 辅助方法：把你的 UserWithAddresses 转换成 Proto 定义的 UserProfile
func (s *GrpcServer) convertUserToProto(u *service.UserWithAddresses) *pb.UserProfile {
	if u == nil || u.User == nil {
		return nil
	}

	// 转换地址列表
	var protoAddrs []*pb.AddressInfo
	for _, addr := range u.Addresses {
		protoAddrs = append(protoAddrs, &pb.AddressInfo{
			Chain:   addr.Chain,
			Address: addr.Address,
		})
	}

	// 转换状态
	var status pb.UserStatus
	if u.Status == domain.UserStatusEnabled {
		status = pb.UserStatus_USER_STATUS_ENABLED
	} else {
		status = pb.UserStatus_USER_STATUS_DISABLED
	}

	profile := &pb.UserProfile{
		Id:        u.ID,
		Username:  u.Username,
		Email:     u.Email,
		Phone:     u.Phone,
		Status:    status,
		CreatedAt: u.CreatedAt.Unix(),
		Addresses: protoAddrs,
	}

	// 设置最后登录时间（如果有）
	if u.LastLoginAt != nil {
		profile.LastLoginAt = u.LastLoginAt.Unix()
	}

	return profile
}

// convertError 将 xerr 错误转换为 gRPC status 错误
func (s *GrpcServer) convertError(err error) error {
	if err == nil {
		return nil
	}

	// 检查是否是 xerr.CodeError
	codeErr, ok := err.(*xerr.CodeError)
	if !ok {
		// 如果不是 CodeError，检查错误消息
		errMsg := err.Error()
		if strings.Contains(errMsg, "不存在") || strings.Contains(errMsg, "无对应") {
			return status.Error(codes.NotFound, errMsg)
		}
		if strings.Contains(errMsg, "密码") || strings.Contains(errMsg, "账号") {
			return status.Error(codes.Unauthenticated, errMsg)
		}
		// 默认返回 Internal 错误
		return status.Error(codes.Internal, errMsg)
	}

	// 根据错误码映射 gRPC status code
	var grpcCode codes.Code
	switch codeErr.Code {
	case xerr.RequestParamsError:
		grpcCode = codes.InvalidArgument
	case xerr.RecordNotFound:
		grpcCode = codes.NotFound
	case xerr.ServerCommonError, xerr.DbError:
		grpcCode = codes.Internal
	default:
		grpcCode = codes.Internal
	}

	return status.Error(grpcCode, codeErr.Msg)
}
