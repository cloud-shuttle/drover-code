package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("LANGFUSE_PUBLIC_KEY", "")
	t.Setenv("LANGFUSE_SECRET_KEY", "")
	t.Setenv("LANGFUSE_HOST", "")
	cfg := ConfigFromEnv()
	if !cfg.Disabled {
		t.Fatal("expected disabled without public key")
	}

	t.Setenv("LANGFUSE_PUBLIC_KEY", "pk")
	t.Setenv("LANGFUSE_SECRET_KEY", "sk")
	t.Setenv("LANGFUSE_HOST", "https://example.test")
	cfg = ConfigFromEnv()
	if cfg.Disabled || cfg.Host != "https://example.test" {
		t.Fatalf("%+v", cfg)
	}
}

func TestConfigFromEnv_defaultHostWhenKeysSet(t *testing.T) {
	t.Setenv("LANGFUSE_PUBLIC_KEY", "pk")
	t.Setenv("LANGFUSE_SECRET_KEY", "sk")
	t.Setenv("LANGFUSE_HOST", "")
	cfg := ConfigFromEnv()
	if cfg.Disabled || cfg.Host != "https://cloud.langfuse.com" {
		t.Fatalf("%+v", cfg)
	}
}

func TestNew_DisabledIsSafeToFlush(t *testing.T) {
	tr := New(Config{Disabled: true})
	if !tr.cfg.Disabled {
		t.Fatal("expected disabled config")
	}
	tr.Flush()
	tr.Flush()
}

func TestContextTracer(t *testing.T) {
	ctx := context.Background()
	if TracerFrom(ctx).cfg.Disabled != true {
		t.Fatal("default should be noop")
	}
	tr := New(Config{Disabled: true})
	ctx = WithTracer(ctx, tr)
	if TracerFrom(ctx) != tr {
		t.Fatal("WithTracer")
	}
	if TraceIDFrom(WithTraceID(ctx, "tid")) != "tid" {
		t.Fatal("trace id")
	}
}

func TestTracer_IngestionBatch(t *testing.T) {
	var bodies [][]byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/public/ingestion" {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, b)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	tr := New(Config{
		PublicKey: "pk-test",
		SecretKey: "sk-test",
		Host:      srv.URL,
		Disabled:  false,
	})
	defer tr.Flush()

	tid := tr.StartTrace(TraceParams{Name: "n", Input: "in"})
	tr.EndTrace(tid, "out", map[string]any{"ok": true})
	tr.flush()

	if len(bodies) == 0 {
		t.Fatal("no batches sent")
	}
	var payload map[string]any
	if err := json.Unmarshal(bodies[len(bodies)-1], &payload); err != nil {
		t.Fatal(err)
	}
	batch, _ := json.Marshal(payload["batch"])
	if !strings.Contains(string(batch), "trace-create") {
		t.Fatalf("batch: %s", batch)
	}
}

func TestNoopTracer_methodsDoNotPanic(t *testing.T) {
	tr := Noop()
	tid := tr.StartTrace(TraceParams{Name: "n", Input: "in"})
	tr.EndTrace(tid, "out", map[string]any{"k": 1})
	gid := tr.StartGeneration(GenerationParams{TraceID: tid, Name: "g", Model: "m"})
	tr.EndGeneration(gid, GenerationResult{Output: "o"})
	sid := tr.StartSpan(SpanParams{TraceID: tid, Name: "s"})
	tr.EndSpan(sid, SpanResult{Output: "x"})
	tr.Flush()
	tr.Flush()
}

func TestTracer_flushHTTPErrorDoesNotPanic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/public/ingestion" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	tr := New(Config{
		PublicKey: "pk",
		SecretKey: "sk",
		Host:      srv.URL,
		Disabled:  false,
	})
	defer tr.Flush()

	tid := tr.StartTrace(TraceParams{Name: "t", Input: "i"})
	tr.EndTrace(tid, "o", nil)
	tr.flush()
}
