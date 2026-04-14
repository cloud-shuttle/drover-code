package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBridge_SendFramesWithCorrectLength(t *testing.T) {
	var out bytes.Buffer
	b := NewBridge(strings.NewReader(""), &out)

	id := int64(1)
	b.Send(Message{ID: &id, Method: "ping", Params: json.RawMessage(`{"x":1}`)})

	s := out.String()
	if !strings.HasPrefix(s, "Content-Length: ") {
		t.Fatalf("expected Content-Length prefix, got: %q", s[:min(len(s), 40)])
	}
	parts := strings.SplitN(s, "\r\n\r\n", 2)
	if len(parts) != 2 {
		t.Fatalf("expected header/body split, got %q", s)
	}
	body := parts[1]
	if !strings.Contains(parts[0], "Content-Length: ") {
		t.Fatalf("missing Content-Length header: %q", parts[0])
	}
	// Parse the header number and verify equals body length.
	var n int
	_, err := fmtSscanf(parts[0], "Content-Length: %d", &n)
	if err != nil {
		t.Fatalf("parse Content-Length: %v", err)
	}
	if n != len([]byte(body)) {
		t.Fatalf("Content-Length mismatch: header=%d body=%d", n, len([]byte(body)))
	}
}

func TestBridge_readMessage_missingContentLength(t *testing.T) {
	in := "\r\n"
	var out bytes.Buffer
	b := NewBridge(strings.NewReader(in), &out)
	_, err := b.readMessage(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Content-Length") {
		t.Fatalf("got %v", err)
	}
}

func TestBridge_Run_ReturnsContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()
	var out bytes.Buffer
	b := NewBridge(pr, &out)
	err := b.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: got %v want %v", err, context.Canceled)
	}
}

func TestBridge_Run_ReturnsDeadlineExceeded(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Minute))
	defer cancel()
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()
	var out bytes.Buffer
	b := NewBridge(pr, &out)
	err := b.Run(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run: got %v want %v", err, context.DeadlineExceeded)
	}
}

func TestBridge_Run_ContextCanceledWhileBlockedOnRead(t *testing.T) {
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()

	var out bytes.Buffer
	b := NewBridge(pr, &out)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- b.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run: got %v want %v", err, context.Canceled)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for Run")
	}
}

func TestBridge_Run_ReturnsNilWhenWriterClosedWhileBlocked(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()

	var out bytes.Buffer
	b := NewBridge(pr, &out)
	done := make(chan error, 1)
	go func() { done <- b.Run(context.Background()) }()

	time.Sleep(50 * time.Millisecond)
	if err := pw.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for Run")
	}
}

func TestBridge_Run_ErrorWhenWriterClosedMidBody(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()

	var out bytes.Buffer
	b := NewBridge(pr, &out)
	done := make(chan error, 1)
	go func() { done <- b.Run(context.Background()) }()

	hdr := "Content-Length: 100\r\n\r\nxx"
	if _, err := pw.Write([]byte(hdr)); err != nil {
		t.Fatal(err)
	}
	if err := pw.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error on truncated body")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for Run")
	}
}

func TestBridge_readMessage_contentLengthLongerThanBody(t *testing.T) {
	in := "Content-Length: 50\r\n\r\nab"
	var out bytes.Buffer
	b := NewBridge(strings.NewReader(in), &out)
	_, err := b.readMessage(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "read body") {
		t.Fatalf("got %v", err)
	}
}

func TestBridge_readMessage_badJSONBody(t *testing.T) {
	msg := `not-json`
	in := "Content-Length: " + itoa(len(msg)) + "\r\n\r\n" + msg
	var out bytes.Buffer
	b := NewBridge(strings.NewReader(in), &out)
	_, err := b.readMessage(context.Background())
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("got %v", err)
	}
}

func TestBridge_readMessageParsesFramedJSON(t *testing.T) {
	msg := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"a":1}}`
	in := "Content-Length: " + itoa(len([]byte(msg))) + "\r\n\r\n" + msg

	var out bytes.Buffer
	b := NewBridge(strings.NewReader(in), &out)

	got, err := b.readMessage(context.Background())
	if err != nil {
		t.Fatalf("readMessage error: %v", err)
	}
	if got.Method != "ping" {
		t.Fatalf("expected method ping, got %q", got.Method)
	}
	if got.ID == nil || *got.ID != 1 {
		t.Fatalf("expected id=1, got %+v", got.ID)
	}
	var params map[string]any
	_ = json.Unmarshal(got.Params, &params)
	if params["a"] != float64(1) {
		t.Fatalf("expected params.a=1, got %v", params["a"])
	}
}

// Minimal helpers (avoid extra deps in tests)

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// fmtSscanf / itoa are tiny wrappers to keep imports local.
// (Using fmt and strconv in-file would be fine too, but this keeps the test focused.)
func fmtSscanf(s, format string, n *int) (int, error) {
	// simple parse for "Content-Length: %d"
	s = strings.TrimSpace(strings.TrimPrefix(s, "Content-Length:"))
	var val int
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		val = val*10 + int(r-'0')
	}
	*n = val
	return 1, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [32]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func TestBridge_Run_unknownMethodSendsError(t *testing.T) {
	msg := `{"jsonrpc":"2.0","id":7,"method":"no_such_method","params":{}}`
	in := "Content-Length: " + itoa(len([]byte(msg))) + "\r\n\r\n" + msg

	var out bytes.Buffer
	b := NewBridge(strings.NewReader(in), &out)
	if err := b.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "-32601") || !strings.Contains(s, "no_such_method") {
		t.Fatalf("expected method not found in output: %s", s)
	}
}

func TestBridge_Run_pingHandler(t *testing.T) {
	pr, pw := io.Pipe()
	var out bytes.Buffer
	b := NewBridge(pr, &out)
	RegisterStandardHandlers(b, func(ctx context.Context, input string) (string, error) {
		return "", fmt.Errorf("agent should not run for ping")
	})

	done := make(chan error, 1)
	go func() { done <- b.Run(context.Background()) }()

	ping := `{"jsonrpc":"2.0","id":1,"method":"ping","params":null}`
	if _, err := fmt.Fprintf(pw, "Content-Length: %d\r\n\r\n%s", len(ping), ping); err != nil {
		t.Fatal(err)
	}
	_ = pw.Close()

	deadline := time.After(3 * time.Second)
	for !strings.Contains(out.String(), "pong") {
		select {
		case <-deadline:
			t.Fatalf("timeout, out=%q", out.String())
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
	_ = pr.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRegisterStandardHandlers_droverExecute(t *testing.T) {
	pr, pw := io.Pipe()
	var out bytes.Buffer
	b := NewBridge(pr, &out)
	RegisterStandardHandlers(b, func(ctx context.Context, input string) (string, error) {
		return "echo:" + input, nil
	})

	done := make(chan error, 1)
	go func() { done <- b.Run(context.Background()) }()

	req := `{"jsonrpc":"2.0","id":42,"method":"drover/execute","params":{"input":"hello"}}`
	if _, err := fmt.Fprintf(pw, "Content-Length: %d\r\n\r\n%s", len(req), req); err != nil {
		t.Fatal(err)
	}
	_ = pw.Close()

	deadline := time.After(3 * time.Second)
	for !strings.Contains(out.String(), "echo:hello") {
		select {
		case <-deadline:
			t.Fatalf("timeout, out=%q", out.String())
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
	_ = pr.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRegisterStandardHandlers_initializeThenExecute(t *testing.T) {
	pr, pw := io.Pipe()
	var out bytes.Buffer
	b := NewBridge(pr, &out)
	RegisterStandardHandlers(b, func(ctx context.Context, input string) (string, error) {
		return "echo:" + input, nil
	})

	done := make(chan error, 1)
	go func() { done <- b.Run(context.Background()) }()

	go func() {
		init := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
		if _, err := fmt.Fprintf(pw, "Content-Length: %d\r\n\r\n%s", len(init), init); err != nil {
			t.Errorf("write init: %v", err)
			_ = pw.CloseWithError(err)
			return
		}
		exec := `{"jsonrpc":"2.0","id":2,"method":"drover/execute","params":{"input":"multi"}}`
		if _, err := fmt.Fprintf(pw, "Content-Length: %d\r\n\r\n%s", len(exec), exec); err != nil {
			t.Errorf("write exec: %v", err)
			_ = pw.CloseWithError(err)
			return
		}
		_ = pw.Close()
	}()

	deadline := time.After(3 * time.Second)
	for {
		s := out.String()
		if strings.Contains(s, "capabilities") && strings.Contains(s, "echo:multi") {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timeout, out=%q", s)
		default:
			time.Sleep(3 * time.Millisecond)
		}
	}
	_ = pr.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRegisterStandardHandlers_threeSequentialExecutes(t *testing.T) {
	pr, pw := io.Pipe()
	var out bytes.Buffer
	b := NewBridge(pr, &out)
	var calls atomic.Int32
	RegisterStandardHandlers(b, func(ctx context.Context, input string) (string, error) {
		calls.Add(1)
		return input + "!", nil
	})

	done := make(chan error, 1)
	go func() { done <- b.Run(context.Background()) }()

	go func() {
		for _, id := range []int64{1, 2, 3} {
			req := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"drover/execute","params":{"input":"t%d"}}`, id, id)
			if _, err := fmt.Fprintf(pw, "Content-Length: %d\r\n\r\n%s", len(req), req); err != nil {
				t.Errorf("write: %v", err)
				_ = pw.CloseWithError(err)
				return
			}
		}
		_ = pw.Close()
	}()

	deadline := time.After(3 * time.Second)
	for calls.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("calls=%d out=%q", calls.Load(), out.String())
		default:
			time.Sleep(3 * time.Millisecond)
		}
	}
	s := out.String()
	if !strings.Contains(s, "t1!") || !strings.Contains(s, "t2!") || !strings.Contains(s, "t3!") {
		t.Fatalf("missing echoes in %q", s)
	}
	_ = pr.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRegisterStandardHandlers_droverExecuteInvalidParams(t *testing.T) {
	pr, pw := io.Pipe()
	var out bytes.Buffer
	b := NewBridge(pr, &out)
	RegisterStandardHandlers(b, func(ctx context.Context, input string) (string, error) {
		t.Error("agent should not run")
		return "", errors.New("unexpected")
	})

	done := make(chan error, 1)
	go func() { done <- b.Run(context.Background()) }()

	req := `{"jsonrpc":"2.0","id":5,"method":"drover/execute","params":[]}`
	if _, err := fmt.Fprintf(pw, "Content-Length: %d\r\n\r\n%s", len(req), req); err != nil {
		t.Fatal(err)
	}
	_ = pw.Close()

	deadline := time.After(3 * time.Second)
	for !strings.Contains(out.String(), "-32602") {
		select {
		case <-deadline:
			t.Fatalf("timeout, out=%q", out.String())
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
	_ = pr.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
