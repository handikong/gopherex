package safe

import (
	"context"
	"fmt"
	"runtime/debug"

	"go.uber.org/zap"
	"gopherex.com/pkg/logger" // 引用刚才写的 logger
)

// Go 安全启动协程
func Go(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				stack := string(debug.Stack())

				// 如果 logger 已初始化，用 logger 记；否则打印到标准输出
				if logger.Log != nil {
					logger.Error(context.Background(), "🚨 GOROUTINE PANIC RECOVERED",
						zap.Any("panic", r),
						zap.String("stack", stack),
					)
				} else {
					fmt.Printf("🚨 GOROUTINE PANIC: %v\nStack: %s\n", r, stack)
				}
			}
		}()

		fn()
	}()
}
