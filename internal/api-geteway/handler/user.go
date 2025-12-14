package handler

import (
	"github.com/gin-gonic/gin"
	userPb "gopherex.com/api/user/v1"
	walletPb "gopherex.com/api/wallet/v1"
	"gopherex.com/internal/api-geteway/rpcclient"
	"gopherex.com/pkg/common"
	"gopherex.com/pkg/logger"
)

type User struct {
}

func (u *User) Login(ctx *gin.Context) {
	ctx.JSON(200, gin.H{
		"code": 200,
		"msg":  "success",
		"data": gin.H{
			"username": "admin",
			"password": "admin",
		},
	})
}

func (u *User) Overview(ctx *gin.Context) {
	userInfoReq := &userPb.GetUserInfoReq{
		Query: &userPb.GetUserInfoReq_UserId{
			UserId: 1,
		},
	}
	info, err := rpcclient.User().GetUserInfo(ctx.Request.Context(), userInfoReq)
	if err != nil {
		common.FailFromGRPC(ctx, err)
		return
	}
	// 调用wallet
	recharListReq := &walletPb.RechargeListReq{
		Uid:    "1",
		Chain:  "BTC",
		Symbol: "BTC",
		Status: 1,
		Page:   1,
		Limit:  10,
	}
	list, err := rpcclient.Wallet().GetRechargeListById(ctx.Request.Context(), recharListReq)
	if err != nil {
		common.FailFromGRPC(ctx, err)
		return
	}
	logger.Info(ctx, "我就看看traceID")
	common.Success(ctx, gin.H{
		"info":     info,
		"recharge": list,
	})
}
