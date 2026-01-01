package funds

type Cfg struct {
	Name     string   `yaml:"name" mapstructure:"name"`
	Addr     string   `yaml:"addr" mapstructure:"addr"`
	Db       DBConfig `yaml:"db" mapstructure:"db"`
	Redis    Redis    `yaml:"redis" mapstructure:"redis"`
	OTel     OTel     `yaml:"otel" mapstructure:"otel"`
	Etcd     Etcd     `yaml:"etcd" mapstructure:"etcd"`
	Sentinel Sentinel `yaml:"sentinel" mapstructure:"sentinel"`
}

type DBConfig struct {
	Type                   string `yaml:"type" mapstructure:"type"`
	SourceName             string `yaml:"source_name" mapstructure:"source_name"`
	MaxOpenConns           int    `yaml:"max_open_conns" mapstructure:"max_open_conns"`
	MaxIdleConns           int    `yaml:"max_idle_conns" mapstructure:"max_idle_conns"`
	ConnMaxLifetimeMinutes int    `yaml:"conn_max_lifetime_minutes" mapstructure:"conn_max_lifetime_minutes"`
}

type Redis struct {
	Addr         string `yaml:"addr" mapstructure:"addr"`
	Database     int    `yaml:"db" mapstructure:"db"`
	Auth         string `yaml:"auth" mapstructure:"auth"`
	PoolSize     int    `yaml:"pool_size" mapstructure:"pool_size"`
	MinIdleConns int    `yaml:"min_idle_conns" mapstructure:"min_idle_conns"`
}

type OTel struct {
	Enabled bool   `yaml:"enabled" mapstructure:"enabled"`
	Addr    string `yaml:"addr" mapstructure:"addr"`
}

type Etcd struct {
	Endpoints     []string `yaml:"endpoints" mapstructure:"endpoints"`
	ServicePrefix string   `yaml:"service_prexix" mapstructure:"service_prefix"`
}

type Sentinel struct {
	Enabled bool          `yaml:"enabled" mapstructure:"enabled"`
	Flow    Flow          `yaml:"flow" mapstructure:"flow"`
	Breaker BreakerConfig `yaml:"breaker" mapstructure:"breaker"`
}

type Flow struct {
	Enabled bool       `yaml:"enabled" mapstructure:"enabled"`
	Rules   []FlowRule `yaml:"rules" mapstructure:"rules"`
}
type FlowRule struct {
	Resource         string  `yaml:"resource" mapstructure:"resource"`
	Threshold        float64 `yaml:"threshold" mapstructure:"threshold"`
	StatIntervalMs   uint32  `yaml:"stat_interval_ms" mapstructure:"stat_interval_ms"`
	Strategy         string  `yaml:"strategy" mapstructure:"strategy"`
	Control          string  `yaml:"control" mapstructure:"control"`
	MaxQueueWaitMs   uint32  `yaml:"max_queue_wait_ms" mapstructure:"max_queue_wait_ms"`
	WarmUpSec        uint32  `yaml:"warmup_sec" mapstructure:"warmup_sec"`
	WarmUpColdFactor uint32  `yaml:"warmup_cold_factor" mapstructure:"warmup_cold_factor"`
}

type BreakerConfig struct {
	Enabled bool          `yaml:"enabled" mapstructure:"enabled"`
	Rules   []BreakerRule `yaml:"rules" mapstructure:"rules"`
}

type BreakerRule struct {
	Resource         string  `yaml:"resource" mapstructure:"resource"`
	Strategy         string  `yaml:"strategy" mapstructure:"strategy"`
	Threshold        float64 `yaml:"threshold" mapstructure:"threshold"`
	StatIntervalMs   uint32  `yaml:"stat_interval_ms" mapstructure:"stat_interval_ms"`
	MinRequestAmount uint64  `yaml:"min_request_amount" mapstructure:"min_request_amount"`
	RetryTimeoutMs   uint64  `yaml:"retry_timeout_ms" mapstructure:"retry_timeout_ms"`
}
