package main

import (
	"fmt"
	"log"
	"net"

	sentinels "github.com/alibaba/sentinel-golang/api"
	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/core/flow"
	"github.com/btcsuite/btcd/chaincfg"
	"google.golang.org/grpc"
	pb "gopherex.com/api/user/v1"
	"gopherex.com/internal/user/server"
	"gopherex.com/internal/user/service"
	"gopherex.com/pkg/hdwallet"
	"gopherex.com/pkg/interceptor"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func initSentinel() {
	// 1. 初始化 Sentinel
	err := sentinels.InitDefault()
	if err != nil {
		log.Fatalf("Unexpected error: %+v", err)
	}

	// 2. 定义限流规则 (Flow Rule)
	// 目标：保护 Login 接口，每秒最多只允许 100 个请求 (QPS = 100)
	// 问题分析：
	// - 当100个请求几乎同时到达时，Sentinel 的滑动窗口统计可能存在延迟
	// - 第一个请求通过后，后续请求可能被误判为超过限制
	// 解决方案：
	// - 方案1：提高阈值（临时方案）
	// - 方案2：使用 WarmUp 模式平滑流量（推荐）
	// - 方案3：使用 Throttling 模式排队（适合生产环境）
	resourceName := "/user.v1.User/Login"
	log.Printf("🔧 配置限流规则 - 资源名称: %s, QPS阈值: 200", resourceName)

	_, err = flow.LoadRules([]*flow.Rule{
		{
			Resource:               resourceName, // 必须匹配 gRPC FullMethod
			TokenCalculateStrategy: flow.Direct,
			ControlBehavior:        flow.Reject, // 直接拒绝（可改为 flow.WarmUp 或 flow.Throttling）
			Threshold:              100,         // QPS 阈值（提高到200以应对突发流量）
			StatIntervalInMs:       1000,        // 统计窗口 1秒
			// 如果使用 WarmUp 模式，取消下面的注释：
			// ControlBehavior: flow.WarmUp,
			// WarmUpDurationSec: 10,           // 预热时间（秒）
			// WarmUpColdFactor: 3,             // 冷启动因子（允许3倍流量）
		},
	})
	if err != nil {
		log.Fatalf("加载限流规则失败: %+v", err)
	}

	// 3. 定义熔断规则 (Circuit Breaker Rule)
	// 目标：如果 Login 接口的系统错误率超过 50%，则熔断 5 秒
	// 注意：现在拦截器已经修复，只记录系统错误，不记录业务错误
	// 所以熔断器只会在真正的系统问题（如数据库连接失败）时触发
	_, err = circuitbreaker.LoadRules([]*circuitbreaker.Rule{
		{
			Resource:         resourceName,
			Strategy:         circuitbreaker.ErrorRatio, // 按照错误比例
			RetryTimeoutMs:   5000,                      // 熔断后等待 5s 进入 Half-Open
			MinRequestAmount: 10,                        // 最小请求数（提高到10，防止误触发）
			StatIntervalMs:   1000,                      // 统计窗口
			Threshold:        0.5,                       // 错误率阈值 (50%)
		},
	})
	if err != nil {
		log.Fatalf("加载熔断规则失败: %+v", err)
	}
	log.Println("✅ 熔断器已启用（只记录系统错误，不记录业务错误）")

	log.Println("✅ Sentinel 初始化完成，规则已加载")
}

func main() {
	initSentinel()
	// 1. 初始化 DB
	dsn := "root:123456@tcp(127.0.0.1:3307)/gopherex_wallet?charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai"
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("DB connect failed: ", err)
	}

	// 自动建表 (开发阶段用，生产环境请用 SQL 脚本)
	// db.AutoMigrate(&domain.User{}, &domain.UserAddress{})

	// 2. 初始化 Wallet SDK (用于生成地址)
	// 注意：这里需要你的助记词
	mnemonic := "this father surge entry vehicle cereal return reunion sugar artefact village family"
	walletSdk, err := hdwallet.New(mnemonic, &chaincfg.RegressionNetParams)
	if err != nil {
		log.Fatal("Wallet init failed: ", err)
	}

	// 3. 依赖注入 (Layered Architecture)
	userSvc := service.NewUserService(db, walletSdk) // 你的 Service (注意你原来的 NewUserService 参数是否匹配)
	grpcServerObj := server.NewGrpcServer(userSvc)   // 刚才写的 Glue Code

	// 4. 启动 gRPC Server
	lis, err := net.Listen("tcp", ":9001") // 监听 9001 端口
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			interceptor.SentinelUnaryServerInterceptor(),
			// 明天我们在这里加日志拦截器...
			// 后天在这里加 Recovery 拦截器...
		),
	)
	pb.RegisterUserServer(grpcServer, grpcServerObj) // 注册服务

	fmt.Println("🚀 User Service is running on :9001")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
