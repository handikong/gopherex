package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/pprof"
	"runtime"
	"strings"

	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	sentinels "github.com/alibaba/sentinel-golang/api"
	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/core/flow"
	_ "github.com/go-sql-driver/mysql"
	grpc_prom "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.uber.org/zap"
	"golang.org/x/exp/rand"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
	fundsv1 "gopherex.com/gen/go/fund_service/v1"
	"gopherex.com/internal/funds"
	gmysql "gopherex.com/internal/funds/repo/mysql"
	"gopherex.com/pkg/config"
	"gopherex.com/pkg/interceptor"
	"gopherex.com/pkg/logger"
	"gopherex.com/pkg/metrics"
	"gopherex.com/pkg/register"
	"gopherex.com/pkg/register/etcd"
	"gopherex.com/pkg/trace"
	"gorm.io/driver/mysql"

	//pv "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/protovalidate"
	"gorm.io/gorm"
)

// 构建启动项目
func main() {
	// ========= 0) 全局上下文 & 优雅退出 =========
	// 统一管理服务生命周期：DB/Redis/gRPC/HTTP/OTel 等都挂在这个 ctx 上，
	// 收到 SIGINT/SIGTERM 时自动取消，触发 shutdown。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// 随机种子 为了随机数
	rand.Seed(uint64(time.Now().UnixNano()))
	// 初始化配置文件
	var cfg = &funds.Cfg{}
	_, err := config.LoadAndWatch("funds-service", cfg)
	if err != nil {
		panic(fmt.Sprintf("初始化日志出错%+v", err))
	}
	// 初始化日志
	logger.Init(cfg.Name, "info")
	logger.Info(ctx, "服务开始启动")
	// ========= 3) 构建数据库（连接池） =========
	// 工业要点：
	// - 使用连接池：maxOpen/maxIdle/connMaxLifetime
	// - 使用 PingContext 验证可用
	// - 服务 shutdown 时 Close
	sqlDB, err := newSQLDB(ctx, cfg)
	if err != nil {
		log.Fatalf("init mysql: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	// db的监控
	go func(db *sql.DB) {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for range t.C {
			st := db.Stats()
			metrics.DbPoolOpen.Set(float64(st.OpenConnections))
			metrics.DbPoolIdle.Set(float64(st.Idle))
			metrics.DbPoolInuse.Set(float64(st.InUse))
			metrics.DbPoolWaitCount.Add(float64(st.WaitCount))
			metrics.DbPoolWaitDuration.Add(st.WaitDuration.Seconds())
		}
	}(sqlDB)

	// ========= 4) 构建 Redis（连接池） =========
	// 工业要点：
	// - 设置合理的 PoolSize/MinIdleConns
	// - Ping 验证
	// - 服务 shutdown 时 Close
	rdb, err := newRedis(ctx, cfg)
	if err != nil {
		log.Fatalf("init redis: %v", err)
	}
	defer func() { _ = rdb.Close() }()
	// redis的监控
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for range t.C {
			st := rdb.PoolStats()
			metrics.RedisPoolOpen.Set(float64(st.TotalConns))
			metrics.RedisPoolIdle.Set(float64(st.IdleConns))
			metrics.RedisPoolInuse.Set(float64(st.StaleConns)) // 或 ActiveConns，如果版本支持
			metrics.RedisPoolWaitCount.Add(float64(st.WaitCount))
			metrics.RedisPoolWaitDuration.Add(float64(st.WaitDurationNs))
		}
	}()
	// ========= 5) 构建 Sentinel（限流 / 重试 / 熔断 / 降级） =========
	// 工业要点：
	// - gRPC 层：用 interceptor 做“统一治理”
	// - 业务层：只做必要的不变量保证（资金服务就是 SQL 条件更新/幂等）
	//
	// Sentinel 在 Go 里常见用法：
	// - HTTP：middleware
	// - gRPC：unary/stream interceptor
	// - 资源命名：info.FullMethod 或你自定义的 resource name
	//
	// 这里我给你保留挂载点：buildGovernanceInterceptors(cfg)
	// 你先用空实现也能跑，后面逐个补齐即可。
	if sentinelEnabled(cfg) {
		if err := initSentinel(cfg); err != nil {
			log.Fatalf("init sentinel: %v", err)
		}
	}

	// ========= 7) 构建 RPC 服务（业务对象） =========
	// 工业要点：
	// - 把 repo/cache/service 都在这里组装（依赖注入）
	// - service 本身不创建连接，只消费依赖（便于测试/复用）
	//
	// 你现在只实现 GetBalances：
	// fundsSvc := funds.NewFundsService(repo, cache)
	//
	// 这里先用一个占位函数 buildServices()，你把你自己的 NewFundsService 接进来即可。
	fundsSvc, err := buildServices(ctx, sqlDB, rdb)
	if err != nil {
		log.Fatalf("build services: %v", err)
	}

	// 服务发现
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   cfg.Etcd.Endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Fatalf("connect etcd: %v", err)
	}
	defer cli.Close()

	// 4) 注册服务（lease+keepalive 由你的 reg.Register 内部实现）
	reg := etcd.NewEtcdRegister(cli, cfg.Etcd.ServicePrefix, 10) // TTL=10s（按你原来）
	ID := fmt.Sprintf("%s-%s", cfg.Name, cfg.Addr)
	ins := &register.Instance{
		ID:   ID,       // 建议更稳：hostname+pid 或 uuid；但本地可先用 addr
		Name: cfg.Name, // <- 改成你的真实服务名
		Addr: cfg.Addr,
		MetaData: map[string]string{
			"version": "v1",
			"env":     "dev",
		},
	}
	if err := reg.Register(ctx, ins); err != nil {
		log.Fatalf("register to etcd error: %v", err)
	}
	// 退出时主动注销（即使不注销，ctx cancel 后 lease 过期也会自动删除 key）
	defer func() {
		_ = reg.UnRegister(context.Background(), ins)
	}()

	// ========= 6) 构建 OpenTelemetry（链路追踪） =========
	//	// 工业要点：
	//	// - 设置 ServiceName / Env / Version
	//	// - exporter（OTLP gRPC/HTTP）指向 collector
	//	// - 在 gRPC server 挂 otelgrpc interceptors/StatsHandler
	//	// - shutdown 时 flush
	//	//
	//	// 这里给出“可插拔”入口：initOTel(ctx, cfg)
	if cfg.OTel.Enabled {
		// 如果你 docker 起的 jaeger 在本机，就用 localhost:4317
		// 如果跑在 docker compose 网络里，可能是 jaeger:4317
		shutdownTracer, err := trace.InitTrace(cfg.Name, cfg.OTel.Addr)
		if err != nil {
			logger.Fatal(ctx, "init tracer error", zap.Error(err))
		}
		defer func() {
			// 最多给 5 秒时间 flush trace
			c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := shutdownTracer(c); err != nil {
				logger.Error(ctx, "shutdown tracer error", zap.Error(err))
			}
		}()
	}

	// ========= 8) 构建 RPC 中间件（拦截器链） =========
	// 工业要点（强烈建议统一挂载）：
	// - Recover：防 panic
	// - Timeout：统一超时
	// - RequestID：链路关联
	// - Logging：结构化日志
	// - Metrics：Prometheus 指标
	// - Tracing：OTel
	// - Validation：protovalidate interceptor（你前面要做的“参数不在 handler 校验”）
	// - Governance：Sentinel（限流/熔断/降级/重试）
	grpcServer := newGRPCServer(cfg)

	// 注册你的 gRPC service（这里用占位：你替换成 fundsv1.RegisterFundServiceServer）
	if err := registerGRPCServices(grpcServer, fundsSvc); err != nil {
		log.Fatalf("register grpc services: %v", err)
	}
	// 加入反射 测试环境
	reflection.Register(grpcServer)

	runtime.SetMutexProfileFraction(10) // 1/10 采样（压测可用 5~20）
	runtime.SetBlockProfileRate(10000)  // 纳秒，>0 才开启 block（压测可用 10000~50000）
	// 启动pprof
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	srv := &http.Server{
		Addr:              "127.0.0.1:6555",
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
	}

	go func() {
		log.Printf("pprof listening on %s", srv.Addr)
		if e := srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			log.Printf("pprof listen error: %v", e)
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	// ========= 9) 启动服务（并做优雅关闭） =========
	errCh := make(chan error, 2)
	// 启动 gRPC
	go func() {
		lis, err := net.Listen("tcp", cfg.Addr)
		if err != nil {
			errCh <- err
			return
		}
		log.Printf("gRPC listening on %s", cfg.Addr)
		errCh <- grpcServer.Serve(lis)
	}()
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		srv := &http.Server{
			Addr:    "0.0.0.0:9091", // 选一个空闲端口
			Handler: mux,
		}
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("metrics server error: %v", err)
		}
	}()

	// 等待退出信号或服务错误
	select {
	case <-ctx.Done():
		log.Println("shutdown signal received")
	case err := <-errCh:
		log.Printf("server error: %v", err)
		stop()
	}

	// 优雅关闭：先停接入再等 in-flight 完成
	_, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	grpcServer.GracefulStop()

	log.Println("service stopped")

}

func buildServices(ctx context.Context, sqlDB *sql.DB, rdb *redis.Client) (*funds.FundsService, error) {
	newGorm, err := NewGorm(sqlDB)
	if err != nil {
		return nil, err
	}
	repo := gmysql.NewBalancesRepo(newGorm)
	cache := funds.NewRedisCache(rdb)
	srv := funds.NewFundsService(ctx, repo, cache)
	return srv, nil
}

func NewGorm(sqlDB *sql.DB) (*gorm.DB, error) {
	// 3) 用已存在的 *sql.DB 构造 gorm（关键点：Conn: sqlDB）
	gdb, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      sqlDB, // 复用连接池
		SkipInitializeWithVersion: false, // 需要时可读版本
	}), &gorm.Config{
		// 工业常用：减少默认事务开销（注意：涉及写操作你自己会显式开事务）
		SkipDefaultTransaction: true,
		PrepareStmt:            true,                                         // 可选：高频读写可能收益，但会占用更多内存
		NowFunc:                func() time.Time { return time.Now().UTC() }, // 可选：统一 UTC
	})
	if err != nil {
		return nil, err
	}

	return gdb, nil
}

func newGRPCServer(cfg *funds.Cfg) *grpc.Server {
	// gRPC server 参数：keepalive 防止 NAT/ELB 断流；也能限制客户端太激进的 ping
	kaep := keepalive.EnforcementPolicy{
		MinTime:             10 * time.Second,
		PermitWithoutStream: true,
	}
	kasp := keepalive.ServerParameters{
		Time:    30 * time.Second,
		Timeout: 10 * time.Second,
	}

	var unaryInts []grpc.UnaryServerInterceptor
	var streamInts []grpc.StreamServerInterceptor
	grpc_prom.EnableHandlingTimeHistogram() // 可选：默认关闭，开启后会提供 latency hist
	// 统一在一个链里挂所有 unary 拦截器，避免后续追加时覆盖
	unaryInts = append(unaryInts,
		grpc_prom.UnaryServerInterceptor,
		interceptor.RecoverUnary(),
		interceptor.ErrorUnary(),
		interceptor.RequestIDServerUnary(),
		interceptor.SentinelUnaryServerInterceptor(),
	)
	streamInts = append(streamInts, grpc_prom.StreamServerInterceptor)

	opts := []grpc.ServerOption{
		grpc.KeepaliveEnforcementPolicy(kaep),
		grpc.KeepaliveParams(kasp),
	}
	opts = append(opts, grpc.ChainUnaryInterceptor(unaryInts...))
	opts = append(opts, grpc.ChainStreamInterceptor(streamInts...))
	opts = append(opts, grpc.StatsHandler(otelgrpc.NewServerHandler()))

	gs := grpc.NewServer(opts...)
	grpc_prom.Register(gs) // 将 gRPC 服务器的指标注册进默认 Prometheus registry
	return gs
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
	// 连接池参数（根据你机器/并发调）
	db.SetMaxOpenConns(dbCfg.MaxOpenConns)
	db.SetMaxIdleConns(dbCfg.MaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(dbCfg.ConnMaxLifetimeMinutes) * time.Minute)
	// 链接超时
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil

}

// registerGRPCServices：统一注册 gRPC service（方便多个服务共同复用 main 模板）
func registerGRPCServices(gs *grpc.Server, fundsSvc *funds.FundsService) error {
	fundsv1.RegisterFundServiceServer(gs, fundsSvc)
	return nil
}

func initSentinel(cfg *funds.Cfg) error {
	if !sentinelEnabled(cfg) {
		return nil
	}
	if err := sentinels.InitDefault(); err != nil {
		return fmt.Errorf("init sentinel: %w", err)
	}

	var flowRules []*flow.Rule
	if cfg.Sentinel.Flow.Enabled {
		for _, rule := range cfg.Sentinel.Flow.Rules {
			if rule.Resource == "" {
				continue
			}
			r := &flow.Rule{
				Resource:         rule.Resource,
				Threshold:        rule.Threshold,
				StatIntervalInMs: rule.StatIntervalMs,
			}
			switch strings.ToLower(rule.Strategy) {
			case "direct":
				r.TokenCalculateStrategy = flow.Direct
			case "warmup":
				r.TokenCalculateStrategy = flow.WarmUp
				r.WarmUpPeriodSec = rule.WarmUpSec
				r.WarmUpColdFactor = rule.WarmUpColdFactor
			case "memory_adaptive":
				r.TokenCalculateStrategy = flow.MemoryAdaptive
			default:
				r.TokenCalculateStrategy = flow.Direct
			}

			switch strings.ToLower(rule.Control) {
			case "throttling":
				r.ControlBehavior = flow.Throttling
				r.MaxQueueingTimeMs = rule.MaxQueueWaitMs
			default:
				r.ControlBehavior = flow.Reject
			}

			flowRules = append(flowRules, r)
		}
		if len(flowRules) > 0 {
			if _, err := flow.LoadRules(flowRules); err != nil {
				return fmt.Errorf("load flow rules: %w", err)
			}
		}
	}

	if cfg.Sentinel.Breaker.Enabled {
		var breakerRules []*circuitbreaker.Rule
		for _, rule := range cfg.Sentinel.Breaker.Rules {
			if rule.Resource == "" {
				continue
			}
			r := &circuitbreaker.Rule{
				Resource:         rule.Resource,
				Threshold:        rule.Threshold,
				StatIntervalMs:   rule.StatIntervalMs,
				MinRequestAmount: rule.MinRequestAmount,
				RetryTimeoutMs:   uint32(rule.RetryTimeoutMs),
			}
			switch strings.ToLower(rule.Strategy) {
			case "error_count":
				r.Strategy = circuitbreaker.ErrorCount
			case "slow_request_ratio":
				r.Strategy = circuitbreaker.SlowRequestRatio
			default:
				r.Strategy = circuitbreaker.ErrorRatio
			}
			breakerRules = append(breakerRules, r)
		}
		if len(breakerRules) > 0 {
			if _, err := circuitbreaker.LoadRules(breakerRules); err != nil {
				return fmt.Errorf("load circuit breaker rules: %w", err)
			}
			log.Println("✅ 熔断器已启用（只记录系统错误，不记录业务错误）")
		}
	}

	log.Println("✅ Sentinel 初始化完成，规则已加载")
	return nil
}

func sentinelEnabled(cfg *funds.Cfg) bool {
	return cfg.Sentinel.Enabled || cfg.Sentinel.Flow.Enabled || cfg.Sentinel.Breaker.Enabled
}
