// Package telemetry provides observability: Langfuse direct ingestion (existing)
// plus optional OTEL SDK wiring for structured logs (Warden decisions etc.).
// OTEL path is the long-term unified route into ClickStack/HyperDX via OTLP collector.
package telemetry

import (
	"context"
	"os"
	stdlog "log"
	"strings"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/log/global"
	otelsdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
)

// SetupOTELLogger initializes the global OTEL LoggerProvider with an OTLP/HTTP
// exporter (auto-configured from standard OTEL_EXPORTER_OTLP_* and OTEL_RESOURCE_*
// environment variables).
//
// Warden's emitWardenDecisionAsOTELLog uses otellog.GetLogger which reads this global.
//
// Call very early in process start (before any agent loop or tool calls that may
// trigger Warden checks). It is best-effort and never fatal:
//
//   - missing/misconfigured endpoint → warning, returns no-op shutdown, Warden falls back to CH+Langfuse
//   - success → OTEL logs for drover.warden.* attributes flow to collector → ClickHouse (via MV or direct)
//
// The returned func is always safe to defer-call (even if it does nothing).
func SetupOTELLogger(ctx context.Context) func(context.Context) error {
	// Only attempt real OTLP log export if the user has explicitly configured an endpoint.
	// This prevents noisy "connection refused" spam to stderr when running the TUI
	// interactively with no collector present (the common case).
	endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if endpoint == "" {
		// Best-effort no-op: Warden will fall back to its other telemetry paths (ClickHouse + Langfuse).
		return func(context.Context) error { return nil }
	}

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			semconv.ServiceName("drover-code"),
			semconv.ServiceVersion("dev"),
		),
	)
	if err != nil {
		stdlog.Printf("otel: resource creation failed (continuing without OTEL log export): %v", err)
		return func(context.Context) error { return nil }
	}

	exp, err := otlploghttp.New(ctx)
	if err != nil {
		stdlog.Printf("otel: OTLP/HTTP log exporter creation failed (check OTEL_EXPORTER_OTLP_ENDPOINT etc; continuing without OTEL): %v", err)
		return func(context.Context) error { return nil }
	}

	bp := otelsdklog.NewBatchProcessor(exp)
	lp := otelsdklog.NewLoggerProvider(
		otelsdklog.WithResource(res),
		otelsdklog.WithProcessor(bp),
	)

	global.SetLoggerProvider(lp)
	stdlog.Printf("otel: LoggerProvider initialized; drover.warden OTEL logs enabled (endpoint=%s)", endpoint)
	return lp.Shutdown
}
