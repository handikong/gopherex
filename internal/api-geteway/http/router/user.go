package router

import (
	"github.com/gin-gonic/gin"
	"gopherex.com/internal/api-geteway/handler"
)

func User(api *gin.RouterGroup) {
	userHandler := handler.User{}
	user := api.Group("/user")
	{
		user.GET("/login", userHandler.Login)
		user.GET("/overview", userHandler.Overview)
		// 将来可以加 /profile /logout ...
	}
}
