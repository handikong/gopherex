package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"runtime"
	"strings"
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
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"gopherex.com/pkg/config"
	"gopherex.com/pkg/logger"
	"gopherex.com/pkg/register"
	"gopherex.com/pkg/register/etcd"
)

// Deps collects common dependencies that bootstrap can prepare.
type Deps struct {
	DB    *sql.DB
	Redis *redis.Client
}

// EtcdCfg holds service discovery config.
type EtcdCfg struct {
	Endpoints     []string
	ServicePrefix string
}

// SentinelCfg holds rules for governance.
type SentinelCfg struct {
	Enabled bool
	Flow    FlowSection
	Breaker BreakerConfig
}

type FlowSection struct {
	Enabled bool
	Rules   []FlowRule
}

type FlowRule struct {
	Resource         string
	Threshold        float64
	StatIntervalMs   uint32
	Strategy         string
	Control          string
	MaxQueueWaitMs   uint32
	WarmUpSec        uint32
	WarmUpColdFactor uint32
}

type BreakerConfig struct {
	Enabled bool
	Rules   []BreakerRule
}

type BreakerRule struct {
	Resource         string
	Strategy         string
	Threshold        float64
	StatIntervalMs   uint32
	MinRequestAmount uint64
	RetryTimeoutMs   uint64
}

// Options controls the bootstrap process; provide hooks for service-specific bits.
type Options struct {
	// Required: config name and target struct
	ConfigName string
	ConfigPtr  interface{}

	// Required: extract service name and gRPC addr from config
	ServiceName func(cfg interface{}) string
	GRPCAddr    func(cfg interface{}) string

	// Optional: return Etcd config; if nil, skip registration
	EtcdConfig func(cfg interface{}) *EtcdCfg

	// Optional: init tracer, return shutdown func
	InitTracer func(cfg interface{}) (func(context.Context) error, error)

	// Optional: init sentinel governance; only called when provided
	InitSentinel func(cfg interface{}) error

	// Optional builders; nil means skip
	BuildDB    func(ctx context.Context, cfg interface{}) (*sql.DB, error)
	BuildRedis func(ctx context.Context, cfg interface{}) (*redis.Client, error)

	// Optional hooks when deps ready
	OnDBReady    func(*sql.DB)
	OnRedisReady func(*redis.Client)

	// Build service and return a gRPC register function
	BuildServices func(ctx context.Context, cfg interface{}, deps Deps) (func(*grpc.Server) error, error)

	// gRPC interceptors (chained as-is); provide service-specific additions
	UnaryInterceptors  []grpc.UnaryServerInterceptor
	StreamInterceptors []grpc.StreamServerInterceptor
	StatsHandler       grpc.StatsHandler

	// Listen addresses
	MetricsAddr string
	PprofAddr   string
}

// Run boots a microservice with common wiring; callers inject config and service-specific hooks via Options.
func Run(ctx context.Context, opt Options) error {
	if opt.ConfigName == "" || opt.ConfigPtr == nil || opt.ServiceName == nil || opt.GRPCAddr == nil || opt.BuildServices == nil {
		return fmt.Errorf("bootstrap: missing required options")
	}

	if _, err := config.LoadAndWatch(opt.ConfigName, opt.ConfigPtr); err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	svcName := opt.ServiceName(opt.ConfigPtr)
	logger.Init(svcName, "info")

	if opt.InitSentinel != nil {
		if err := opt.InitSentinel(opt.ConfigPtr); err != nil {
			return fmt.Errorf("init sentinel: %w", err)
		}
	}

	var deps Deps
	var err error
	if opt.BuildDB != nil {
		deps.DB, err = opt.BuildDB(ctx, opt.ConfigPtr)
		if err != nil {
			return fmt.Errorf("init db: %w", err)
		}
		defer func() { _ = deps.DB.Close() }()
		if opt.OnDBReady != nil {
			opt.OnDBReady(deps.DB)
		}
	}
	if opt.BuildRedis != nil {
		deps.Redis, err = opt.BuildRedis(ctx, opt.ConfigPtr)
		if err != nil {
			return fmt.Errorf("init redis: %w", err)
		}
		defer func() { _ = deps.Redis.Close() }()
		if opt.OnRedisReady != nil {
			opt.OnRedisReady(deps.Redis)
		}
	}

	var shutdownTracer func(context.Context) error
	if opt.InitTracer != nil {
		shutdownTracer, err = opt.InitTracer(opt.ConfigPtr)
		if err != nil {
			return fmt.Errorf("init tracer: %w", err)
		}
	}

	registerFn, err := opt.BuildServices(ctx, opt.ConfigPtr, deps)
	if err != nil {
		return fmt.Errorf("build services: %w", err)
	}

	grpcServer := newGRPCServer(opt)
	if err := registerFn(grpcServer); err != nil {
		return fmt.Errorf("register grpc: %w", err)
	}

	if opt.PprofAddr != "" {
		startPprof(opt.PprofAddr)
	}
	if opt.MetricsAddr != "" {
		startMetrics(opt.MetricsAddr)
	}

	var reg register.Register
	if ec := opt.EtcdConfig; ec != nil {
		if eCfg := ec(opt.ConfigPtr); eCfg != nil && len(eCfg.Endpoints) > 0 {
			cli, err := clientv3.New(clientv3.Config{
				Endpoints:   eCfg.Endpoints,
				DialTimeout: 5 * time.Second,
			})
			if err != nil {
				return fmt.Errorf("connect etcd: %w", err)
			}
			defer cli.Close()
			reg = etcd.NewEtcdRegister(cli, eCfg.ServicePrefix, 10)
			ins := &register.Instance{
				ID:   fmt.Sprintf("%s-%s", svcName, opt.GRPCAddr(opt.ConfigPtr)),
				Name: svcName,
				Addr: opt.GRPCAddr(opt.ConfigPtr),
				MetaData: map[string]string{
					"version": "v1",
					"env":     "dev",
				},
			}
			if err := reg.Register(ctx, ins); err != nil {
				return fmt.Errorf("register etcd: %w", err)
			}
			defer func() { _ = reg.UnRegister(context.Background(), ins) }()
		}
	}

	errCh := make(chan error, 1)
	go func() {
		lis, err := net.Listen("tcp", opt.GRPCAddr(opt.ConfigPtr))
		if err != nil {
			errCh <- err
			return
		}
		log.Printf("gRPC listening on %s", opt.GRPCAddr(opt.ConfigPtr))
		errCh <- grpcServer.Serve(lis)
	}()

	select {
	case <-ctx.Done():
		log.Println("shutdown signal received")
	case err := <-errCh:
		log.Printf("server error: %v", err)
	}

	if shutdownTracer != nil {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTracer(c)
	}

	grpcServer.GracefulStop()
	log.Println("service stopped")
	return nil
}

func newGRPCServer(opt Options) *grpc.Server {
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
	grpc_prom.EnableHandlingTimeHistogram()
	unaryInts = append(unaryInts, grpc_prom.UnaryServerInterceptor)
	streamInts = append(streamInts, grpc_prom.StreamServerInterceptor)
	unaryInts = append(unaryInts, opt.UnaryInterceptors...)
	streamInts = append(streamInts, opt.StreamInterceptors...)

	opts := []grpc.ServerOption{
		grpc.KeepaliveEnforcementPolicy(kaep),
		grpc.KeepaliveParams(kasp),
	}
	opts = append(opts, grpc.ChainUnaryInterceptor(unaryInts...))
	opts = append(opts, grpc.ChainStreamInterceptor(streamInts...))
	if opt.StatsHandler != nil {
		opts = append(opts, grpc.StatsHandler(opt.StatsHandler))
	}

	gs := grpc.NewServer(opts...)
	grpc_prom.Register(gs)
	return gs
}

func startPprof(addr string) {
	runtime.SetMutexProfileFraction(10)
	runtime.SetBlockProfileRate(10000)

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
	}

	go func() {
		log.Printf("pprof listening on %s", srv.Addr)
		if e := srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			log.Printf("pprof listen error: %v", e)
		}
	}()
}

func startMetrics(addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	go func() {
		log.Printf("metrics listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("metrics server error: %v", err)
		}
	}()
}

// InitSentinelFromCfg builds an InitSentinel hook from a sentinel config.
func InitSentinelFromCfg(getCfg func(cfg interface{}) *SentinelCfg) func(cfg interface{}) error {
	return func(cfg interface{}) error {
		sc := getCfg(cfg)
		if sc == nil || !(sc.Enabled || sc.Flow.Enabled || sc.Breaker.Enabled) {
			return nil
		}
		if err := sentinels.InitDefault(); err != nil {
			return fmt.Errorf("init sentinel: %w", err)
		}

		var flowRules []*flow.Rule
		if sc.Flow.Enabled {
			for _, rule := range sc.Flow.Rules {
				if rule.Resource == "" {
					continue
				}
				r := &flow.Rule{
					Resource:         rule.Resource,
					Threshold:        rule.Threshold,
					StatIntervalInMs: rule.StatIntervalMs,
				}
				switch strings.ToLower(rule.Strategy) {
				case "direct", "":
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
		}

		if len(flowRules) > 0 {
			if _, err := flow.LoadRules(flowRules); err != nil {
				return fmt.Errorf("load flow rules: %w", err)
			}
		}

		if sc.Breaker.Enabled {
			var breakerRules []*circuitbreaker.Rule
			for _, rule := range sc.Breaker.Rules {
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
				log.Println("✅ 熔断器已启用")
			}
		}

		log.Println("✅ Sentinel 初始化完成，规则已加载")
		return nil
	}
}
