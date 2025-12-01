package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestLogger_Info_WithTraceID(t *testing.T) {
	// 1. 劫持日志输出到内存 Buffer
	buffer := &bytes.Buffer{}

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.MessageKey = "msg"

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.AddSync(buffer), // 关键点：写入 buffer 而不是控制台
		zap.InfoLevel,
	)

	// 2. 替换全局 Log 变量 (模拟 Init)
	// 注意：我们要测试的是 pkg/logger 包内部的方法，所以可以直接修改包级变量 Log
	Log = zap.New(core)

	// 3. 准备带有 TraceID 的 Context
	// 使用我们在 logger.go 里定义的常量 TraceIdKey
	traceVal := "test-trace-12345"
	ctx := context.WithValue(context.Background(), TraceIdKey, traceVal)

	// 4. 调用封装的 Info 方法
	Info(ctx, "测试充值日志", zap.String("user", "Alice"), zap.Float64("amount", 100.5))

	// 5. 解析输出结果
	// 输出应该是 JSON 格式的一行字符串
	var logEntry map[string]interface{}
	err := json.Unmarshal(buffer.Bytes(), &logEntry)
	assert.NoError(t, err, "日志输出必须是合法的 JSON")

	// 6. 断言验证
	assert.Equal(t, "info", logEntry["level"])
	assert.Equal(t, "测试充值日志", logEntry["msg"])
	assert.Equal(t, "Alice", logEntry["user"])
	assert.Equal(t, 100.5, logEntry["amount"])

	// 🔥 核心验证：确保 TraceID 被自动注入了
	assert.Equal(t, traceVal, logEntry["trace_id"], "TraceID 未能自动注入到日志中")
}

func TestLogger_Error_NoTraceID(t *testing.T) {
	// 1. 再次劫持输出 (清空环境)
	buffer := &bytes.Buffer{}
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(buffer),
		zap.InfoLevel,
	)
	Log = zap.New(core)

	// 2. 传入空 Context (不带 TraceID)
	Error(context.Background(), "数据库连接失败", zap.String("db", "mysql"))

	// 3. 解析结果
	var logEntry map[string]interface{}
	_ = json.Unmarshal(buffer.Bytes(), &logEntry)

	// 4. 验证 trace_id 字段不存在
	_, exists := logEntry["trace_id"]
	assert.False(t, exists, "没有 TraceID 的 Context 不应该输出 trace_id 字段")
	assert.Equal(t, "error", logEntry["level"])
}
