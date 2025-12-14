package trace

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// InitTracer 初始化 OpenTelemetry TracerProvider
// serviceName: 当前服务名，例如 "user-service"
// endpoint: OTLP gRPC 地址，比如 "localhost:4317" (docker 起的 jaeger)

func InitTrace(serviceName string, endpoint string) (func(context.Context) error, error) {
	ctx := context.Background()
	clientOpts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(), // 没有tls
	}
	otlpClient := otlptracegrpc.NewClient(clientOpts...)

	exporter, err := otlptrace.New(ctx, otlpClient)
	if err != nil {
		return nil, fmt.Errorf("create otlp exporter: %w", err)
	}
	// 2. 资源信息：service.name 等
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}
	// 3. 创建 TracerProvider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	// 4. 设置全局 Provider 和 Propagator
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// 返回一个关闭函数，服务退出时调用
	return tp.Shutdown, nil

}
