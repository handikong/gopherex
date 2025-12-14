package router

import (
	"github.com/gin-gonic/gin"
	"gopherex.com/internal/api-geteway/handler"
)

func Waller(api *gin.RouterGroup) {
	// 这里可以复用 UserHandler 里的 Balance；
	userHandler := handler.User{}
	wallet := api.Group("/wallet")
	{
		wallet.GET("/balance", userHandler.Login)
		// 将来可以加 /recharge /withdraw /records ...
	}
}
