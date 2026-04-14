package telemetry

import (
	"context"
	"testing"
)

func TestSpanIDFrom(t *testing.T) {
	ctx := context.Background()
	if SpanIDFrom(ctx) != "" {
		t.Fatalf("empty ctx: %q", SpanIDFrom(ctx))
	}
	ctx = WithSpanID(WithSpanID(ctx, "outer"), "inner")
	if SpanIDFrom(ctx) != "inner" {
		t.Fatalf("got %q", SpanIDFrom(ctx))
	}
}

func TestWithTracer_nilFallsBackToNoop(t *testing.T) {
	ctx := WithTracer(context.Background(), nil)
	if TracerFrom(ctx).cfg.Disabled != true {
		t.Fatal("expected noop tracer")
	}
}
