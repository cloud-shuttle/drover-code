package telemetry

import "context"

type contextKey int

const (
	tracerKey contextKey = iota
	traceIDKey
	spanIDKey
)

// WithTracer attaches a Tracer to a context.
func WithTracer(ctx context.Context, t *Tracer) context.Context {
	return context.WithValue(ctx, tracerKey, t)
}

// TracerFrom extracts the Tracer from a context.
// Returns a no-op Tracer if none was attached — call sites never need to nil-check.
func TracerFrom(ctx context.Context) *Tracer {
	if t, ok := ctx.Value(tracerKey).(*Tracer); ok && t != nil {
		return t
	}
	return Noop()
}

// WithTraceID attaches the current trace ID to a context.
func WithTraceID(ctx context.Context, id TraceID) context.Context {
	return context.WithValue(ctx, traceIDKey, id)
}

// TraceIDFrom extracts the current trace ID from a context.
func TraceIDFrom(ctx context.Context) TraceID {
	if id, ok := ctx.Value(traceIDKey).(TraceID); ok {
		return id
	}
	return ""
}

// WithSpanID attaches the current span ID to a context.
func WithSpanID(ctx context.Context, id SpanID) context.Context {
	return context.WithValue(ctx, spanIDKey, id)
}

// SpanIDFrom extracts the current span ID from a context.
func SpanIDFrom(ctx context.Context) SpanID {
	if id, ok := ctx.Value(spanIDKey).(SpanID); ok {
		return id
	}
	return ""
}
