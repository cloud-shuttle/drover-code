package main

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	libtel "github.com/cloud-shuttle/drover-libs/pkg/telemetry"
)

// version is written as service.version on spans. Release builds set it with
// -ldflags "-X main.version=...".
var version = "dev"

// flushTraces flushes buffered spans. main sets it from setupTraces;
// exitHeadless calls it because os.Exit skips deferred calls.
var flushTraces = func() {}

// setupTraces starts OpenTelemetry tracing through drover-libs and sets
// flushTraces. Export follows libtel.ConfigFromEnv: it is on only when a
// collector is configured, so an interactive session with no collector
// stays quiet. The W3C propagators are installed either way.
//
// When the endpoint comes from OTEL_EXPORTER_OTLP_ENDPOINT and no protocol is
// set, traces use http/protobuf, the protocol telemetry.SetupOTELLogger
// already uses for Warden logs, so one endpoint serves both. A plaintext
// DROVER_OTEL_ENDPOINT keeps the libs default of gRPC.
func setupTraces(ctx context.Context) {
	cfg := libtel.ConfigFromEnv("drover-code", version)
	if cfg.Endpoint == "" && strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")) == "" {
		cfg.Protocol = libtel.ProtocolHTTP
	}
	shutdown, err := libtel.Setup(ctx, cfg)
	if err != nil {
		log.Printf("otel: tracing disabled: %v", err)
		return
	}
	var once sync.Once
	flushTraces = func() {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := shutdown(ctx); err != nil {
				log.Printf("otel: flush failed: %v", err)
			}
		})
	}
}

// endHeadlessRun ends the headless run span and flushes. It is a no-op until
// startHeadlessRun sets it.
var endHeadlessRun = func(status string) {}

// startHeadlessRun opens the invoke_agent span for one headless run. ctx has
// already picked up TRACEPARENT from the parent (Cloud passes it to hosted
// workers), so the span joins the Cloud job's trace. The session log is
// stamped with the span's IDs so session events join otel_traces.
func startHeadlessRun(ctx context.Context, l *sessionLog) context.Context {
	ids := libtel.IDs{
		TenantID: os.Getenv("DROVER_GATEWAY_DIM_CUSTOMER_ID"),
		RunID:    os.Getenv("DROVER_GATEWAY_DIM_AGENT_JOB_ID"),
	}
	if l.sessionID != "" && l.sessionID != "local" {
		ids.SessionID = l.sessionID
	}
	ctx = libtel.WithIDs(ctx, ids)
	ctx, span := libtel.StartAgent(ctx, "drover-code")
	l.traceID = libtel.TraceID(ctx)
	l.spanID = libtel.SpanID(ctx)

	var once sync.Once
	endHeadlessRun = func(status string) {
		once.Do(func() {
			var err error
			if status != "completed" {
				err = errors.New("headless run " + status)
			}
			libtel.End(span, err)
			flushTraces()
		})
	}
	return ctx
}
