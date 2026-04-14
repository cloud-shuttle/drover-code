package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetryDelay429_retryAfterHeader(t *testing.T) {
	if d := retryDelay429("42", 99); d != 42*time.Second {
		t.Fatalf("got %v", d)
	}
	if d := retryDelay429(" 3 ", 0); d != 3*time.Second {
		t.Fatalf("got %v", d)
	}
}

func TestRetryDelay429_exponentialFallback(t *testing.T) {
	if d := retryDelay429("", 0); d != 5*time.Second {
		t.Fatalf("attempt 0: %v", d)
	}
	if d := retryDelay429("0", 1); d != 10*time.Second {
		t.Fatalf("attempt 1: %v", d)
	}
	if d := retryDelay429("nope", 4); d != 80*time.Second {
		t.Fatalf("attempt 4: %v", d)
	}
	// attemptIndex is capped at 4 for multiplier → 5s * 16 = 80s (below 90s cap).
	if d := retryDelay429("", 10); d != 80*time.Second {
		t.Fatalf("high attempt plateau: %v", d)
	}
}

func TestStreamMessage_retries429(t *testing.T) {
	oldWait := waitBefore429Retry
	waitBefore429Retry = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { waitBefore429Retry = oldWait })

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n <= 2 {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {}\n\n"))
	}))
	t.Cleanup(srv.Close)

	c := NewClient("k", "m")
	c.SetBaseURL(srv.URL)
	stream, err := c.StreamMessage(context.Background(), StreamRequest{
		Messages: []Message{UserMessage("hi")},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if calls.Load() != 3 {
		t.Fatalf("expected 3 HTTP calls, got %d", calls.Load())
	}
	// Drain stream
	for stream.Next() {
	}
	if stream.Err() != nil && stream.Err() != io.EOF {
		t.Fatal(stream.Err())
	}
}
