package handler

import (
	"github.com/gin-gonic/gin"
	"gopherex.com/pkg/common"
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

	common.Success(ctx, gin.H{})
}
