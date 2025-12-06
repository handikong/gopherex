package logger

import (
	"context"
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// 定义 TraceID 在 Context 中的 Key (后续接入 Go-Zero/OpenTelemetry 时可替换)
const TraceIdKey = "trace_id"

// 全局 Logger 实例
var Log *zap.Logger

// Init 初始化日志组件
// serviceName: 当前微服务的名称 (例如 "wallet-service")
// level: 日志级别 (debug, info, warn, error)
func Init(serviceName string, level string) {
	InitWithFile(serviceName, level, "")
}

// InitWithFile 初始化日志组件，支持指定日志文件路径
// serviceName: 当前微服务的名称 (例如 "wallet-service")
// level: 日志级别 (debug, info, warn, error)
// logFile: 日志文件路径，如果为空则使用默认路径 logs/{serviceName}.log
func InitWithFile(serviceName string, level string, logFile string) {
	// 1. 配置日志级别
	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		zapLevel = zap.InfoLevel // 默认 Info
	}

	// 2. 配置编码器 (生产环境强制用 JSON)
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder   // 时间格式: 2023-11-23T...
	encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder // 级别格式: INFO, ERROR
	encoderConfig.MessageKey = "msg"                        // 消息字段名

	// 3. 准备写入目标：控制台 + 文件
	writeSyncers := []zapcore.WriteSyncer{
		zapcore.AddSync(os.Stdout), // 输出到控制台 (容器化标准)
	}

	// 4. 如果指定了日志文件，则同时写入文件
	if logFile == "" {
		// 默认日志文件路径：logs/{serviceName}.log
		logFile = filepath.Join("logs", serviceName+".log")
	}

	// 确保日志目录存在
	logDir := filepath.Dir(logFile)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		// 如果创建目录失败，只输出到控制台
		_ = err
	} else {
		// 打开日志文件（追加模式）
		file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			writeSyncers = append(writeSyncers, zapcore.AddSync(file))
		}
		// 如果打开文件失败，只输出到控制台，不中断程序
	}

	// 5. 创建多写入器（同时写入控制台和文件）
	multiWriter := zapcore.NewMultiWriteSyncer(writeSyncers...)

	// 6. 创建 Core
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig), // JSON 格式方便 ELK 收集
		multiWriter,
		zapLevel,
	)

	// 7. 构建 Logger
	// AddCaller: 显示文件名和行号
	// AddCallerSkip: 因为我们要封装一层函数，所以 Skip 1，否则行号永远指向 logger.go
	Log = zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))

	// 8. 注入全局字段 (如服务名)
	Log = Log.With(zap.String("service", serviceName))
}

// ---------------------------------------------------------
// 核心封装：带 Context 的日志方法
// ---------------------------------------------------------

// Info 打印 Info 级别日志
func Info(ctx context.Context, msg string, fields ...zap.Field) {
	extractTrace(ctx, &fields)
	Log.Info(msg, fields...)
}

// Error 打印 Error 级别日志
func Error(ctx context.Context, msg string, fields ...zap.Field) {
	extractTrace(ctx, &fields)
	Log.Error(msg, fields...)
}

// Warn 打印 Warn 级别日志
func Warn(ctx context.Context, msg string, fields ...zap.Field) {
	extractTrace(ctx, &fields)
	Log.Warn(msg, fields...)
}

// Debug 打印 Debug 级别日志
func Debug(ctx context.Context, msg string, fields ...zap.Field) {
	extractTrace(ctx, &fields)
	Log.Debug(msg, fields...)
}

// Fatal 打印 Fatal 级别日志 (会调用 os.Exit)
func Fatal(ctx context.Context, msg string, fields ...zap.Field) {
	extractTrace(ctx, &fields)
	Log.Fatal(msg, fields...)
}

// extractTrace 私有方法：从 Context 中提取 TraceID 并追加到 fields
func extractTrace(ctx context.Context, fields *[]zap.Field) {
	if ctx == nil {
		return
	}

	// 尝试获取 TraceID
	// 在 Go-Zero 中，这里会改成 trace.TraceIDFromContext(ctx)
	if traceID, ok := ctx.Value(TraceIdKey).(string); ok && traceID != "" {
		*fields = append(*fields, zap.String("trace_id", traceID))
	}
}

// Sync 刷新缓冲区 (建议在 main 函数 defer 中调用)
func Sync() {
	if Log != nil {
		_ = Log.Sync()
	}
}
