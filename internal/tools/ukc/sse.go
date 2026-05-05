package ukc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ReadExecStream reads SSE events from the agent until done or ctx ends.
func ReadExecStream(ctx context.Context, client *http.Client, streamURL, token string, onLine func(string)) (string, int, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return "", 0, fmt.Errorf("stream: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	return ParseExecStream(resp.Body, onLine)
}

// ParseExecStream parses SSE events from an io.Reader.
func ParseExecStream(r io.Reader, onLine func(string)) (string, int, error) {
	var out strings.Builder
	code := 0
	sc := bufio.NewScanner(r)
	// Large lines for command output
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)

	for sc.Scan() {
		line := sc.Text()
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
			return out.String(), code, nil
		}
		stream, _ := m["stream"].(string)
		msg, _ := m["line"].(string)
		if stream != "" {
			fmt.Fprintf(&out, "[%s] %s\n", stream, msg)
		} else if msg != "" {
			out.WriteString(msg)
			out.WriteByte('\n')
		}
	}
	if err := sc.Err(); err != nil {
		return out.String(), code, err
	}
	return out.String(), code, nil
}
