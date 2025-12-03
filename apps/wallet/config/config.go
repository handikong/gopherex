package config

// Config 对应 etc/wallet.yaml 的内容
type Config struct {
	// 基础配置 (go-zero 需要)
	Name string

	// MySQL 配置
	Mysql struct {
		DataSource  string // DSN: "user:pass@tcp(ip:port)/db..."
		MaxIdle     int    `json:",default=10"` // 默认值
		MaxOpen     int    `json:",default=100"`
		MaxLifetime int    `json:",default=3600"` // 秒
	}

	// Redis 配置
	Redis struct {
		Addr     string // "ip:port"
		Password string `json:",optional"` // 可选
		DB       int    `json:",default=0"`
	}

	// 新增 Bitcoin 配置
	Bitcoin struct {
		Host   string
		User   string
		Pass   string
		EthUrl string
	}
}
