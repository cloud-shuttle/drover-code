package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStreamReplay_LastEventID(t *testing.T) {
	jr := newJobRunner("")
	id := randomID()
	
	// Create a dummy job with history
	j := &job{
		id:        id,
		history:   make([]map[string]any, 0, 64),
		listeners: make([]chan struct{}, 0),
	}
	jr.jobs[id] = j
	
	j.trySend(map[string]any{"line": "hello"})
	j.trySend(map[string]any{"line": "world"})
	j.trySend(map[string]any{"done": true, "exit_code": 0})

	// Test requesting from event 1 (skipping "hello", fetching "world" and "done")
	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	req.Header.Set("Last-Event-ID", "0")
	rec := httptest.NewRecorder()

	err := jr.streamSSE(rec, req, id)
	if err != nil {
		t.Fatalf("streamSSE error: %v", err)
	}

	out := rec.Body.String()
	if strings.Contains(out, "hello") {
		t.Fatalf("expected hello to be skipped, got: %s", out)
	}
	if !strings.Contains(out, "world") {
		t.Fatalf("expected world, got: %s", out)
	}
}

func TestStreamReplay_GapWarning(t *testing.T) {
	jr := newJobRunner("")
	id := randomID()
	
	j := &job{
		id:        id,
		history:   make([]map[string]any, 0, 64),
		listeners: make([]chan struct{}, 0),
		baseIndex: 50, // simulate that events 0-49 were evicted
	}
	jr.jobs[id] = j
	
	j.trySend(map[string]any{"line": "late event"})
	j.trySend(map[string]any{"done": true, "exit_code": 0})

	// Requesting from event 0, but base is 50, so gap warning should be emitted
	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	req.Header.Set("Last-Event-ID", "0")
	rec := httptest.NewRecorder()

	err := jr.streamSSE(rec, req, id)
	if err != nil {
		t.Fatalf("streamSSE error: %v", err)
	}

	out := rec.Body.String()
	if !strings.Contains(out, "stream replay gap") {
		t.Fatalf("expected gap warning, got: %s", out)
	}
	if !strings.Contains(out, "late event") {
		t.Fatalf("expected late event, got: %s", out)
	}
}

func TestStreamReplay_LiveReconnection(t *testing.T) {
	jr := newJobRunner("")
	tmpDir := t.TempDir()
	t.Setenv("UKC_AGENT_WORKSPACE", tmpDir)
	id := jr.start(context.Background(), "echo 'first'; sleep 0.1; echo 'second'")

	// First connection: read "first", then disconnect
	ctx, cancel1 := context.WithCancel(context.Background())
	req1, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/stream", nil)
	rec1 := httptest.NewRecorder()
	
	// We want to stream until we see "first", then cancel the context to simulate disconnect
	go func() {
		// Read body manually instead of streamSSE to interleave properly
		time.Sleep(50 * time.Millisecond)
		cancel1()
	}()
	
	_ = jr.streamSSE(rec1, req1, id) // this will return context.Canceled

	// Second connection: read the rest
	req2 := httptest.NewRequest(http.MethodGet, "/stream", nil)
	rec2 := httptest.NewRecorder()
	_ = jr.streamSSE(rec2, req2, id)

	out := rec2.Body.String()
	
	// We might get "first" again if we didn't use Last-Event-ID, but we should definitely get "second"
	if !strings.Contains(out, "second") {
		t.Fatalf("expected second to arrive in the stream: %s", out)
	}
}
