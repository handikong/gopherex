package app

import (
	"context"
	"database/sql"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"golang.org/x/exp/rand"
	"google.golang.org/grpc"
	fundsv1 "gopherex.com/gen/go/fund_service/v1"
	"gopherex.com/internal/funds"
	gmysql "gopherex.com/internal/funds/repo/mysql"
	"gopherex.com/pkg/bootstrap"
	"gopherex.com/pkg/interceptor"
	"gopherex.com/pkg/metrics"
	"gopherex.com/pkg/trace"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// Run 启动资金服务：外层只需传入 ctx 即可。
func Run(ctx context.Context) error {
	rand.Seed(uint64(time.Now().UnixNano()))

	cfg := &funds.Cfg{}

	return bootstrap.Run(ctx, bootstrap.Options{
		ConfigName: "funds-service",
		ConfigPtr:  cfg,
		ServiceName: func(_ interface{}) string {
			return cfg.Name
		},
		GRPCAddr: func(_ interface{}) string {
			return cfg.Addr
		},
		EtcdConfig: func(_ interface{}) *bootstrap.EtcdCfg {
			return &bootstrap.EtcdCfg{
				Endpoints:     cfg.Etcd.Endpoints,
				ServicePrefix: cfg.Etcd.ServicePrefix,
			}
		},
		InitTracer: func(_ interface{}) (func(context.Context) error, error) {
			if !cfg.OTel.Enabled {
				return nil, nil
			}
			return trace.InitTrace(cfg.Name, cfg.OTel.Addr)
		},
		InitSentinel: bootstrap.InitSentinelFromCfg(func(_ interface{}) *bootstrap.SentinelCfg {
			return sentinelCfgFromFunds(cfg)
		}),
		BuildDB: func(c context.Context, _ interface{}) (*sql.DB, error) {
			return newSQLDB(c, cfg)
		},
		BuildRedis: func(c context.Context, _ interface{}) (*redis.Client, error) {
			return newRedis(c, cfg)
		},
		OnDBReady:    observeDBStats,
		OnRedisReady: observeRedisStats,
		BuildServices: func(c context.Context, _ interface{}, deps bootstrap.Deps) (func(*grpc.Server) error, error) {
			newGorm, err := NewGorm(deps.DB)
			if err != nil {
				return nil, err
			}
			repo := gmysql.NewBalancesRepo(newGorm)
			cache := funds.NewRedisCache(deps.Redis)
			srv := funds.NewFundsService(c, repo, cache)
			return func(gs *grpc.Server) error {
				fundsv1.RegisterFundServiceServer(gs, srv)
				return nil
			}, nil
		},
		UnaryInterceptors: []grpc.UnaryServerInterceptor{
			interceptor.RecoverUnary(),
			interceptor.ErrorUnary(),
			interceptor.RequestIDServerUnary(),
			interceptor.SentinelUnaryServerInterceptor(),
		},
		StatsHandler: otelgrpc.NewServerHandler(),
		MetricsAddr:  "0.0.0.0:9091",
		PprofAddr:    "127.0.0.1:6555",
	})
}

// observeDBStats 采集 DB 连接池指标。
func observeDBStats(db *sql.DB) {
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		var lastWaitCount int64
		var lastWaitDuration time.Duration
		for range t.C {
			st := db.Stats()
			metrics.DbPoolOpen.Set(float64(st.OpenConnections))
			metrics.DbPoolIdle.Set(float64(st.Idle))
			metrics.DbPoolInuse.Set(float64(st.InUse))

			deltaWait := st.WaitCount - lastWaitCount
			if deltaWait > 0 {
				metrics.DbPoolWaitCount.Add(float64(deltaWait))
				lastWaitCount = st.WaitCount
			}
			deltaDur := st.WaitDuration - lastWaitDuration
			if deltaDur > 0 {
				metrics.DbPoolWaitDuration.Add(deltaDur.Seconds())
				lastWaitDuration = st.WaitDuration
			}
		}
	}()
}

// observeRedisStats 采集 Redis 连接池指标。
func observeRedisStats(rdb *redis.Client) {
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		var lastWaitCount int64
		var lastWaitDuration time.Duration
		for range t.C {
			st := rdb.PoolStats()
			metrics.RedisPoolOpen.Set(float64(st.TotalConns))
			metrics.RedisPoolIdle.Set(float64(st.IdleConns))
			metrics.RedisPoolInuse.Set(float64(st.StaleConns))

			deltaWait := st.WaitCount - lastWaitCount
			if deltaWait > 0 {
				metrics.RedisPoolWaitCount.Add(float64(deltaWait))
				lastWaitCount = st.WaitCount
			}
			deltaDur := st.WaitDuration - lastWaitDuration
			if deltaDur > 0 {
				metrics.RedisPoolWaitDuration.Add(deltaDur.Seconds())
				lastWaitDuration = st.WaitDuration
			}
		}
	}()
}

func NewGorm(sqlDB *sql.DB) (*gorm.DB, error) {
	gdb, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: false,
	}), &gorm.Config{
		SkipDefaultTransaction: true,
		PrepareStmt:            true,
		NowFunc:                func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, err
	}

	return gdb, nil
}

func newRedis(ctx context.Context, cfg *funds.Cfg) (*redis.Client, error) {
	rcfg := cfg.Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:         rcfg.Addr,
		Password:     "",
		DB:           rcfg.Database,
		PoolSize:     rcfg.PoolSize,
		MinIdleConns: rcfg.MinIdleConns,
	})
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		_ = rdb.Close()
		return nil, err
	}
	return rdb, nil
}

func newSQLDB(ctx context.Context, cfg *funds.Cfg) (*sql.DB, error) {
	dbCfg := cfg.Db
	db, err := sql.Open(cfg.Db.Type, dbCfg.SourceName)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(dbCfg.MaxOpenConns)
	db.SetMaxIdleConns(dbCfg.MaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(dbCfg.ConnMaxLifetimeMinutes) * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func sentinelCfgFromFunds(cfg *funds.Cfg) *bootstrap.SentinelCfg {
	return &bootstrap.SentinelCfg{
		Enabled: cfg.Sentinel.Enabled,
		Flow: bootstrap.FlowSection{
			Enabled: cfg.Sentinel.Flow.Enabled,
			Rules: func() []bootstrap.FlowRule {
				var out []bootstrap.FlowRule
				for _, r := range cfg.Sentinel.Flow.Rules {
					out = append(out, bootstrap.FlowRule{
						Resource:         r.Resource,
						Threshold:        r.Threshold,
						StatIntervalMs:   r.StatIntervalMs,
						Strategy:         r.Strategy,
						Control:          r.Control,
						MaxQueueWaitMs:   r.MaxQueueWaitMs,
						WarmUpSec:        r.WarmUpSec,
						WarmUpColdFactor: r.WarmUpColdFactor,
					})
				}
				return out
			}(),
		},
		Breaker: bootstrap.BreakerConfig{
			Enabled: cfg.Sentinel.Breaker.Enabled,
			Rules: func() []bootstrap.BreakerRule {
				var out []bootstrap.BreakerRule
				for _, r := range cfg.Sentinel.Breaker.Rules {
					out = append(out, bootstrap.BreakerRule{
						Resource:         r.Resource,
						Strategy:         r.Strategy,
						Threshold:        r.Threshold,
						StatIntervalMs:   r.StatIntervalMs,
						MinRequestAmount: r.MinRequestAmount,
						RetryTimeoutMs:   r.RetryTimeoutMs,
					})
				}
				return out
			}(),
		},
	}
}
