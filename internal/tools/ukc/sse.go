package ukc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ReadExecStream reads SSE events from the agent until done or ctx ends.
func ReadExecStream(ctx context.Context, client *http.Client, streamURL, token string, onLine func(string)) (string, int, error) {
	if client == nil {
		client = http.DefaultClient
	}

	var out strings.Builder
	code := 0
	lastEventID := "-1"

	retries := 0
	maxRetries := 5

	for {
		if err := ctx.Err(); err != nil {
			return out.String(), code, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
		if err != nil {
			return out.String(), code, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "text/event-stream")
		if lastEventID != "-1" {
			req.Header.Set("Last-Event-ID", lastEventID)
		}

		resp, err := client.Do(req)
		if err != nil {
			if retries < maxRetries {
				retries++
				time.Sleep(2 * time.Second)
				continue
			}
			return out.String(), code, err
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
			resp.Body.Close()
			return out.String(), code, fmt.Errorf("stream: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
		}

		done, newCode, err := ParseExecStreamChunk(resp.Body, onLine, &out, &lastEventID)
		resp.Body.Close()

		if done {
			return out.String(), newCode, nil
		}

		if retries < maxRetries {
			retries++
			time.Sleep(2 * time.Second)
			continue
		}
		return out.String(), code, err
	}
}

// ParseExecStreamChunk parses SSE events from an io.Reader and appends to out.
func ParseExecStreamChunk(r io.Reader, onLine func(string), out *strings.Builder, lastEventID *string) (bool, int, error) {
	code := 0
	sc := bufio.NewScanner(r)
	// Large lines for command output
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)

	for sc.Scan() {
		line := sc.Text()
		
		if strings.HasPrefix(line, "id:") {
			*lastEventID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
			continue
		}

		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if onLine != nil {
			onLine(payload)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(payload), &m); err != nil {
			continue
		}
		if done, _ := m["done"].(bool); done {
			switch v := m["exit_code"].(type) {
			case float64:
				code = int(v)
			case int:
				code = v
			}
			return true, code, nil
		}
		stream, _ := m["stream"].(string)
		msg, _ := m["line"].(string)
		if stream != "" {
			fmt.Fprintf(out, "[%s] %s\n", stream, msg)
		} else if msg != "" {
			out.WriteString(msg)
			out.WriteByte('\n')
		}
	}
	if err := sc.Err(); err != nil {
		return false, 0, err
	}
	return false, 0, io.EOF
}
