package rpcclient

import (
	"google.golang.org/grpc"
	userpb "gopherex.com/api/user/v1"
	walletpb "gopherex.com/api/wallet/v1"
)

var (
	userClient   userpb.UserClient
	walletClient walletpb.WalletClient
	inited       bool
)

// Init 在网关启动时调用一次，初始化全局单例。
// 约定：只能在 main/app.New 阶段调用，且必须先于任何请求处理。
func Init(userConn, walletConn *grpc.ClientConn) {
	if inited {
		// 启动阶段双重初始化说明写错了，直接 panic 出来方便你发现问题
		panic("rpcclient.Init called more than once")
	}
	userClient = userpb.NewUserClient(userConn)
	walletClient = walletpb.NewWalletClient(walletConn)
	inited = true
}

func User() userpb.UserClient {
	if !inited {
		panic("rpcclient.User called before Init")
	}
	return userClient
}

func Wallet() walletpb.WalletClient {
	if !inited {
		panic("rpcclient.Wallet called before Init")
	}
	return walletClient
}
