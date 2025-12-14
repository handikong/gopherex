package config

// 总配置
type GatewayConfig struct {
	Name        string         `mapstructure:"name" json:"name" yaml:"name"`
	HTTP        HTTPConfig     `mapstructure:"http" json:"http" yaml:"http"`
	Etcd        EtcdConfig     `mapstructure:"etcd" json:"etcd" yaml:"etcd"`
	Trace       TraceConfig    `mapstructure:"trace" json:"trace" yaml:"trace"`
	RPCServices RpcServiceBase `mapstructure:"rpcServices" json:"rpc_services" yaml:"RPCServices"`
}
type RpcServiceBase struct {
	BasePath      string `mapstructure:"basePath" yaml:"basePath" json:"basePath"`
	UserService   string `mapstructure:"userService" yaml:"userService" json:"userService"`
	WalletService string `mapstructure:"walletService" yaml:"walletService"`
}

// HTTP 配置
type HTTPConfig struct {
	Addr string `mapstructure:"addr" yaml:"addr"`
}

// etcd 配置
type EtcdConfig struct {
	Endpoints         []string `mapstructure:"endpoints" yaml:"endpoints"`
	DialTimeoutSecond int      `mapstructure:"dial_timeout_seconds" yaml:"dialTimeoutSecond"`
}

type TraceConfig struct {
	Host string `mapstructure:"host" yaml:"host"`
}
