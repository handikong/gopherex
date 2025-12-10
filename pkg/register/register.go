package register

import "context"

// 定义注册中心的原信息
type Instance struct {
	ID       string            // 简单的用ip:port的方式
	Name     string            // 服务名称 eg:"wallet-service"
	Addr     string            // ip:port
	MetaData map[string]string // 一些其他信息
}

// 接口需要的参数
type Register interface {
	Register(ctx context.Context, ins *Instance) error
	UnRegister(ctx context.Context, ins *Instance) error
}
