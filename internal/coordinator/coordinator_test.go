package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudshuttle/drover-code/internal/agent"
	"github.com/cloudshuttle/drover-code/internal/api"
	"github.com/cloudshuttle/drover-code/internal/config"
	"github.com/cloudshuttle/drover-code/internal/tools"
)

func TestParseSubtaskDescriptionsJSON(t *testing.T) {
	fb := "fallback task"
	if got := ParseSubtaskDescriptionsJSON(`[" a  ", "b"]`, fb); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %#v", got)
	}
	if got := ParseSubtaskDescriptionsJSON(`["x", 1, null, "y"]`, fb); len(got) != 2 || got[0] != "x" || got[1] != "y" {
		t.Fatalf("got %#v", got)
	}
	raw := `[`
	for i := 0; i < 12; i++ {
		if i > 0 {
			raw += ","
		}
		raw += fmt.Sprintf(`"t%d"`, i)
	}
	raw += `]`
	if got := ParseSubtaskDescriptionsJSON(raw, fb); len(got) != maxCoordinatorSubtasks {
		t.Fatalf("cap: len=%d want %d", len(got), maxCoordinatorSubtasks)
	}
	// No valid strings -> fallback
	if got := ParseSubtaskDescriptionsJSON(`[1, 2, 3]`, fb); len(got) != 1 || got[0] != fb {
		t.Fatalf("got %#v", got)
	}
	if got := ParseSubtaskDescriptionsJSON(`[]`, fb); len(got) != 1 || got[0] != fb {
		t.Fatalf("got %#v", got)
	}
	if got := ParseSubtaskDescriptionsJSON(`{not json`, fb); len(got) != 1 || got[0] != fb {
		t.Fatalf("malformed: %#v", got)
	}
}

func TestIsolatedWorkDir_CreatesWorkerDir(t *testing.T) {
	base := t.TempDir()
	dir, err := IsolatedWorkDir(base, 2)
	if err != nil {
		t.Fatal(err)
	}
	wantSuffix := filepath.Join(".drover-code-workers", "worker-2")
	if !strings.HasSuffix(dir, wantSuffix) {
		t.Fatalf("dir %q should end with %q", dir, wantSuffix)
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		t.Fatalf("stat: %v isDir=%v", err, fi != nil && fi.IsDir())
	}
}

func TestCoordinator_Execute_ContextCanceledBeforeStart(t *testing.T) {
	called := atomic.Bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := api.NewClient("k", "m")
	client.SetBaseURL(srv.URL)
	reg := tools.NewRegistry()
	events := make(chan agent.Event, 8)
	go func() {
		for range events {
		}
	}()

	c := New(client, reg, t.TempDir(), events, config.Settings{})
	_, err := c.Execute(ctx, "parent task")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if called.Load() {
		t.Fatal("HTTP server should not run when ctx is already canceled")
	}
}

func TestCoordinator_Execute_ContextCanceledDuringSynthesis(t *testing.T) {
	var synthEntered atomic.Bool
	unblockSynth := make(chan struct{})
	var req atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		w.Header().Set("Content-Type", "text/event-stream")
		n := req.Add(1)

		switch {
		case strings.Contains(s, "You are a coordinator agent"):
			writeTextSSE(t, w, `["w1","w2"]`)
		case strings.Contains(s, "You are a worker agent"):
			writeTextSSE(t, w, "worker done")
		case strings.Contains(s, "Synthesise the above results"):
			synthEntered.Store(true)
			<-unblockSynth
			writeTextSSE(t, w, "final")
		default:
			t.Fatalf("unexpected req %d: %.120s", n, s)
		}
	}))
	defer srv.Close()

	client := api.NewClient("k", "m")
	client.SetBaseURL(srv.URL)
	reg := tools.NewRegistry()
	events := make(chan agent.Event, 256)
	go func() {
		for range events {
		}
	}()

	c := New(client, reg, t.TempDir(), events, config.Settings{})
	c.MaxWorkers = 2

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := c.Execute(ctx, "parent task")
		errCh <- err
	}()

	deadline := time.After(8 * time.Second)
	for !synthEntered.Load() {
		select {
		case <-deadline:
			t.Fatal("synthesis not started")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Execute did not return")
	}
	close(unblockSynth)
}

func TestCoordinator_Execute_ContextCanceledDuringWorker(t *testing.T) {
	var w1Entered atomic.Bool
	unblockWorker := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		w.Header().Set("Content-Type", "text/event-stream")

		switch {
		case strings.Contains(s, "You are a coordinator agent"):
			writeTextSSE(t, w, `["w1","w2"]`)
		case strings.Contains(s, "You are a worker agent"):
			if strings.Contains(s, "w1") {
				w1Entered.Store(true)
				<-unblockWorker
			}
			writeTextSSE(t, w, "worker out")
		case strings.Contains(s, "Synthesise the above results"):
			t.Error("synthesis should not run after worker cancel")
			writeTextSSE(t, w, "bad")
		default:
			t.Fatalf("unexpected body: %.200s", s)
		}
	}))
	defer srv.Close()

	client := api.NewClient("k", "m")
	client.SetBaseURL(srv.URL)
	reg := tools.NewRegistry()
	events := make(chan agent.Event, 256)
	go func() {
		for range events {
		}
	}()

	c := New(client, reg, t.TempDir(), events, config.Settings{})
	c.MaxWorkers = 1

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := c.Execute(ctx, "parent task")
		errCh <- err
	}()

	deadline := time.After(8 * time.Second)
	for !w1Entered.Load() {
		select {
		case <-deadline:
			t.Fatal("worker w1 did not start")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	cancel()
	close(unblockWorker)

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Execute did not return")
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{`here ["a","b"] trailing`, `["a","b"]`},
		{"\n```json\n[\"x\"]\n```\n", `["x"]`},
		{`no array`, `[]`},
	}
	for _, tc := range tests {
		got := extractJSON(tc.in)
		if got != tc.want {
			t.Errorf("extractJSON(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestCoordinator_Execute_EndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		w.Header().Set("Content-Type", "text/event-stream")

		switch {
		case strings.Contains(s, "coordinator agent"):
			writeTextSSE(t, w, `["worker task one","worker task two"]`)
		case strings.Contains(s, "You are a worker agent"):
			writeTextSSE(t, w, "worker output")
		case strings.Contains(s, "Synthesise the above results"):
			writeTextSSE(t, w, "synthesised answer")
		default:
			t.Fatalf("unexpected request body prefix: %.200s", s)
		}
	}))
	defer srv.Close()

	client := api.NewClient("k", "m")
	client.SetBaseURL(srv.URL)

	reg := tools.NewRegistry()
	events := make(chan agent.Event, 256)
	go func() {
		for range events {
		}
	}()

	c := New(client, reg, t.TempDir(), events, config.Settings{})
	c.MaxWorkers = 2

	out, err := c.Execute(context.Background(), "parent task")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "synthesised answer") {
		t.Fatalf("output: %q", out)
	}

	ex, err := c.ExecuteWithResults(context.Background(), "parent task")
	if err != nil {
		t.Fatalf("ExecuteWithResults: %v", err)
	}
	if ex.Summary != out {
		t.Fatalf("ExecuteWithResults summary mismatch")
	}
	if len(ex.Workers) != 2 {
		t.Fatalf("workers: %d", len(ex.Workers))
	}
}

// When the model returns JSON that does not unmarshal into []string (e.g. numbers),
// decompose falls back to a single subtask equal to the parent task.
func TestCoordinator_Execute_decomposeInvalidElementTypeFallsBack(t *testing.T) {
	var req atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		w.Header().Set("Content-Type", "text/event-stream")
		n := req.Add(1)

		switch {
		case strings.Contains(s, "You are a coordinator agent"):
			if n != 1 {
				t.Fatalf("expected decompose first, got req %d", n)
			}
			writeTextSSE(t, w, `[1, 2, 3]`)
		case strings.Contains(s, "You are a worker agent"):
			if !strings.Contains(s, "parent task line") {
				t.Fatalf("worker should receive fallback task: %.200s", s)
			}
			writeTextSSE(t, w, "worker ok")
		case strings.Contains(s, "Synthesise the above results"):
			writeTextSSE(t, w, "synthesised fallback")
		default:
			t.Fatalf("unexpected request %d: %.200s", n, s)
		}
	}))
	defer srv.Close()

	client := api.NewClient("k", "m")
	client.SetBaseURL(srv.URL)

	reg := tools.NewRegistry()
	events := make(chan agent.Event, 256)
	go func() {
		for range events {
		}
	}()

	c := New(client, reg, t.TempDir(), events, config.Settings{})
	c.MaxWorkers = 2

	out, err := c.Execute(context.Background(), "parent task line")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "synthesised fallback") {
		t.Fatalf("output: %q", out)
	}
}

func writeTextSSE(t *testing.T, w http.ResponseWriter, text string) {
	t.Helper()
	enc, err := json.Marshal(text)
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(w, "event: content_block_start\n")
	io.WriteString(w, `data: {"index":0,"content_block":{"type":"text","text":""}}`+"\n\n")
	io.WriteString(w, "event: content_block_delta\n")
	io.WriteString(w, `data: {"index":0,"delta":{"type":"text_delta","text":`+string(enc)+`}}`+"\n\n")
	io.WriteString(w, "event: content_block_stop\n")
	io.WriteString(w, `data: {"index":0}`+"\n\n")
	io.WriteString(w, "event: message_delta\n")
	io.WriteString(w, `data: {"delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":1,"output_tokens":1}}`+"\n\n")
	io.WriteString(w, "event: message_stop\n")
	io.WriteString(w, "data: {}\n\n")
}
