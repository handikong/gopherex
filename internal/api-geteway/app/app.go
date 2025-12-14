package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	clientV3 "go.etcd.io/etcd/client/v3"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/resolver"
	gateWayConfig "gopherex.com/internal/api-geteway/config"
	ghttp "gopherex.com/internal/api-geteway/http"
	"gopherex.com/internal/api-geteway/rpcclient"
	vipConfig "gopherex.com/pkg/config"
	"gopherex.com/pkg/interceptor"
	"gopherex.com/pkg/logger"
	"gopherex.com/pkg/register/etcd"
	"gopherex.com/pkg/trace"
)

type App struct {
	etcdClientV3 *clientV3.Client
	ctx          context.Context
	cfg          gateWayConfig.GatewayConfig
	userConn     *grpc.ClientConn
	walletConn   *grpc.ClientConn
	treeShutDown func(context.Context) error
}

func New(configName string) (*App, error) {
	// 加载配置
	var cfg = &gateWayConfig.GatewayConfig{}
	if configName != "" {
		configName = "api-gateway"
	}
	if _, err := vipConfig.LoadAndWatch(configName, &cfg); err != nil {
		panic(err)
	}
	app := &App{
		cfg: *cfg,
	}
	return app, nil
}
func (app *App) StartService(ctx context.Context, serviceName string) func() {
	if serviceName == "" {
		panic(fmt.Sprintf("serviceName不存在"))
	}
	logger.Init(serviceName, "info")

	app.ctx = ctx
	// 启动trace
	app.startTrace()
	// 启动etcd
	app.startEtcd()
	// 启动grpc
	app.startGrpc()
	// 返回需要关闭的链接
	var cleanUp = func() {
		_ = app.etcdClientV3.Close()
		_ = app.userConn.Close()
		_ = app.walletConn.Close()
		_ = app.treeShutDown(ctx)

		logger.Sync()
	}
	return cleanUp
}

func (app *App) StartHttp() *http.Server {
	return ghttp.NewRouter(app.cfg.HTTP.Addr)
}

func (app *App) startTrace() {
	// 3. Tracer
	tracerShutdown, err := trace.InitTrace(app.cfg.Name, app.cfg.Trace.Host)
	if err != nil {
		log.Fatal("init tracer error", err)
	}
	app.treeShutDown = tracerShutdown

}

func (app *App) startEtcd() {
	var diaTimeOut = app.cfg.Etcd.DialTimeoutSecond
	cli, err := clientV3.New(clientV3.Config{
		Endpoints:   app.cfg.Etcd.Endpoints,
		DialTimeout: time.Duration(diaTimeOut) * time.Second,
	})
	if err != nil {
		log.Fatalf("connect etcd: %v", err)
	}
	app.etcdClientV3 = cli
}

func (app *App) startGrpc() {
	// 链接到客户端
	bashPath := app.cfg.RPCServices.BasePath
	if bashPath == "" {
		panic("bashPath address is empty")
	}
	resolver.Register(etcd.NewBuilder(app.etcdClientV3, bashPath))
	userTarget := app.cfg.RPCServices.UserService
	if userTarget == "" {
		panic("userService address is empty")
	}
	walletTarget := app.cfg.RPCServices.WalletService
	if walletTarget == "" {
		panic("walletService address is empty")
	}
	userConn, err := app.grpcConn(userTarget)
	if err != nil {
		log.Fatalf("connect grpc: %v", err)
	}
	app.userConn = userConn
	walletConn, err := app.grpcConn(walletTarget)
	if err != nil {
		log.Fatalf("connect grpc: %v", err)
	}
	app.walletConn = walletConn
	// 单例rpc链接
	rpcclient.Init(userConn, walletConn)

}

func (app *App) grpcConn(targetName string) (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient(
		targetName,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()), // client
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
		grpc.WithChainUnaryInterceptor(
			interceptor.RequestIDUnary(),
			interceptor.TimeOutInterceptor(3*time.Second),
		),
	)
	return conn, err
}
