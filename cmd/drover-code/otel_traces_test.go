package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cloud-shuttle/drover-libs/pkg/sessionevent"
	libtel "github.com/cloud-shuttle/drover-libs/pkg/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
)

type keepSpans struct{ *tracetest.InMemoryExporter }

func (keepSpans) Shutdown(context.Context) error { return nil }

// A headless run started with TRACEPARENT must open an invoke_agent span
// under the parent's span, carry the session and job IDs, and stamp its IDs
// on the session events it writes.
func TestHeadlessRunJoinsParentTrace(t *testing.T) {
	const (
		parentTrace = "4bf92f3577b34da6a3ce929d0e0e4736"
		parentSpan  = "00f067aa0ba902b7"
	)
	t.Setenv("TRACEPARENT", "00-"+parentTrace+"-"+parentSpan+"-01")
	t.Setenv("DROVER_GATEWAY_DIM_AGENT_JOB_ID", "aj-7")
	t.Setenv("DROVER_GATEWAY_DIM_CUSTOMER_ID", "cust-3")

	exp := tracetest.NewInMemoryExporter()
	shutdown, err := libtel.Setup(context.Background(), libtel.Config{
		ServiceName: "drover-code-test", Enabled: true, SampleRatio: 0,
		TraceExporter: keepSpans{exp}, MetricReader: sdkmetric.NewManualReader(),
	})
	if err != nil {
		t.Fatal(err)
	}
	prevFlush, prevEnd := flushTraces, endHeadlessRun
	t.Cleanup(func() { flushTraces, endHeadlessRun = prevFlush, prevEnd })
	flushTraces = func() { _ = shutdown(context.Background()) }

	path := filepath.Join(t.TempDir(), "events.jsonl")
	l := sessionLog{path: path, sessionID: "sess-9", envelope: true}
	ctx := startHeadlessRun(libtel.ContextFromEnviron(context.Background()), &l)
	if libtel.TraceID(ctx) != parentTrace {
		t.Fatalf("run trace %s, want parent trace %s", libtel.TraceID(ctx), parentTrace)
	}
	l.begin()
	endHeadlessRun("failed")
	endHeadlessRun("completed") // second call is a no-op

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1 (SampleRatio 0 must still follow a sampled parent)", len(spans))
	}
	s := spans[0]
	if s.Name != "invoke_agent drover-code" || s.Parent.SpanID().String() != parentSpan {
		t.Fatalf("span %q parent %s", s.Name, s.Parent.SpanID())
	}
	if s.Status.Code != codes.Error {
		t.Fatalf("status %v, want Error for a failed run", s.Status.Code)
	}
	attrs := map[string]string{}
	for _, kv := range s.Attributes {
		attrs[string(kv.Key)] = kv.Value.Emit()
	}
	for k, want := range map[string]string{"drover.session.id": "sess-9", "drover.run.id": "aj-7", "drover.tenant.id": "cust-3"} {
		if attrs[k] != want {
			t.Errorf("%s = %q, want %q", k, attrs[k], want)
		}
	}

	evs, err := sessionevent.ReadAll(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) == 0 {
		t.Fatal("no session events written")
	}
	for _, ev := range evs {
		if ev.TraceID != parentTrace || ev.SpanID != s.SpanContext.SpanID().String() {
			t.Fatalf("event %s has trace_id=%q span_id=%q", ev.Type, ev.TraceID, ev.SpanID)
		}
	}
}

// Without TRACEPARENT and without a collector, session events carry no trace
// fields, so local logs stay byte-for-byte what they were.
func TestHeadlessRunWithoutTraceLeavesEventsPlain(t *testing.T) {
	t.Setenv("TRACEPARENT", "")
	if _, err := libtel.Setup(context.Background(), libtel.Config{ServiceName: "drover-code-test"}); err != nil {
		t.Fatal(err)
	}
	// Setup with export off leaves the global provider alone; reset it to
	// the no-op default an un-exporting process has.
	prevTP := otel.GetTracerProvider()
	otel.SetTracerProvider(noop.NewTracerProvider())
	t.Cleanup(func() { otel.SetTracerProvider(prevTP) })
	prevEnd := endHeadlessRun
	t.Cleanup(func() { endHeadlessRun = prevEnd })

	path := filepath.Join(t.TempDir(), "events.jsonl")
	l := sessionLog{path: path, sessionID: "local", envelope: true}
	startHeadlessRun(libtel.ContextFromEnviron(context.Background()), &l)
	l.begin()
	endHeadlessRun("completed")

	evs, err := sessionevent.ReadAll(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if ev.TraceID != "" || ev.SpanID != "" {
			t.Fatalf("event %s has trace fields %q/%q", ev.Type, ev.TraceID, ev.SpanID)
		}
	}
}
