package ukc_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudshuttle/drover-code/internal/tools/ukc"
)

func TestParseExecStreamChunk(t *testing.T) {
	input := `id: 1
data: {"stream": "stdout", "line": "hello"}

id: 2
data: {"stream": "stderr", "line": "world"}

id: 3
data: {"done": true, "exit_code": 42}
`

	var out strings.Builder
	var lastID string
	var parsedLines []string

	done, code, err := ukc.ParseExecStreamChunk(strings.NewReader(input), func(line string) {
		parsedLines = append(parsedLines, line)
	}, &out, &lastID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !done {
		t.Errorf("expected done to be true")
	}
	if code != 42 {
		t.Errorf("expected code 42, got %d", code)
	}
	if lastID != "3" {
		t.Errorf("expected lastID 3, got %s", lastID)
	}

	expectedOut := "[stdout] hello\n[stderr] world\n"
	if out.String() != expectedOut {
		t.Errorf("expected out %q, got %q", expectedOut, out.String())
	}

	if len(parsedLines) != 3 {
		t.Errorf("expected 3 parsed lines, got %d", len(parsedLines))
	}
}

func TestReadExecStream_Reconnects(t *testing.T) {
	reqCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		w.Header().Set("Content-Type", "text/event-stream")
		lastID := r.Header.Get("Last-Event-ID")

		if lastID == "" {
			// First request, drop connection after first chunk
			io.WriteString(w, "id: 1\ndata: {\"line\": \"first\"}\n\n")
			w.(http.Flusher).Flush()
			return // Server closes connection
		}

		if lastID == "1" {
			// Second request, finish stream
			io.WriteString(w, "id: 2\ndata: {\"line\": \"second\"}\n\n")
			io.WriteString(w, "id: 3\ndata: {\"done\": true, \"exit_code\": 0}\n\n")
			w.(http.Flusher).Flush()
			return
		}

		t.Errorf("unexpected Last-Event-ID: %s", lastID)
	}))
	defer srv.Close()

	var lines []string
	out, code, err := ukc.ReadExecStream(context.Background(), srv.Client(), srv.URL, "token", func(line string) {
		lines = append(lines, line)
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if reqCount != 2 {
		t.Errorf("expected 2 requests due to reconnect, got %d", reqCount)
	}

	expectedOut := "first\nsecond\n"
	if out != expectedOut {
		t.Errorf("expected %q, got %q", expectedOut, out)
	}
}
