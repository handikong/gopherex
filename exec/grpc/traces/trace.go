package traces

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

func TraceId(ctx context.Context) string {
	tc := trace.SpanContextFromContext(ctx)

	if !tc.IsValid() {
		return ""
	}
	return tc.TraceID().String()
}

func SpanID(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.SpanID().String()
}
