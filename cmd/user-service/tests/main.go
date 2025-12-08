package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	pb "gopherex.com/api/user/v1"
)

func main() {
	// 暴力测试user service服务
	conn, err := grpc.NewClient("localhost:9001", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("无法连接到服务器: %v", err)
	}
	defer conn.Close()

	client := pb.NewUserClient(conn)
	var wg sync.WaitGroup
	var failureCount int64
	totalRequests := 10000

	// 添加延迟，避免所有请求同时到达导致 Sentinel 限流误判
	// 每个请求间隔 10ms，100个请求会在约1秒内均匀分布
	// 如果设置为 0，则所有请求同时发送（用于测试限流器）
	// requestInterval := 10 * time.Millisecond
	// log.Printf("测试配置: 总请求数=%d, 请求间隔=%v", totalRequests, requestInterval)

	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// 添加延迟，让请求间隔发送（避免同时到达）
			// 第一个请求立即发送，后续请求按 id * interval 延迟
			// if id > 0 {
			// 	time.Sleep(time.Duration(id) * requestInterval)
			// }

			// 构造请求 (根据你的 LoginRequest 定义进行调整)
			req := &pb.LoginReq{
				Account:  fmt.Sprintf("user%d@example.com", id),
				Password: "password123",
				Ip:       "127.0.0.1",
			}

			// 发起 RPC 调用
			// 设置10秒超时，防止长时间阻塞
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, err := client.Login(ctx, req)
			if err != nil {
				// 检查是否是账号或密码错误（InvalidArgument），这种情况认为是成功的请求
				st, ok := status.FromError(err)
				if ok && st.Code() == codes.InvalidArgument {
					// 业务逻辑错误，但服务正常响应，算作成功
					return
				}
				// 其他错误（超时、连接失败等）算作失败
				atomic.AddInt64(&failureCount, 1)
				log.Printf("登录失败 [id=%d]: %v", id, err)
				return
			}
		}(i)
	}
	wg.Wait()

	successCount := totalRequests - int(atomic.LoadInt64(&failureCount))
	log.Printf("\n========== 测试统计 ==========")
	log.Printf("总请求数: %d", totalRequests)
	log.Printf("成功数: %d", successCount)
	log.Printf("失败数: %d", atomic.LoadInt64(&failureCount))
	log.Printf("成功率: %.2f%%", float64(successCount)/float64(totalRequests)*100)
	log.Printf("=============================")
}
