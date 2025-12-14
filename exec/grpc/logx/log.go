package logx

import (
	"context"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopherex.com/exec/grpc/traces"
)

func New() (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	cfg.EncoderConfig.TimeKey = "ts"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	return cfg.Build()
}

// FromContext 返回带 trace_id/span_id 的 logger（如果 ctx 里有的话）
func FromContext(ctx context.Context, base *zap.Logger) *zap.Logger {
	tid := traces.TraceId(ctx)
	if tid == "" {
		return base
	}
	sid := traces.SpanID(ctx)
	return base.With(
		zap.String("trace_id", tid),
		zap.String("span_id", sid),
	)
}
