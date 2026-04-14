package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL   = "https://api.anthropic.com"
	anthropicVersion = "2023-06-01"
	defaultMaxTokens = 8096
	// maxStream429Attempts is total HTTP attempts on rate limit (429) before giving up.
	maxStream429Attempts = 6
)

// waitBefore429Retry blocks until delay elapses or ctx is cancelled (overridable in tests).
var waitBefore429Retry = func(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// retryDelay429 returns wait duration after a 429. Uses Retry-After when it is a
// positive integer (seconds); otherwise exponential backoff from 5s up to 90s.
func retryDelay429(retryAfterHeader string, attemptIndex int) time.Duration {
	if retryAfterHeader != "" {
		if sec, err := strconv.Atoi(strings.TrimSpace(retryAfterHeader)); err == nil && sec > 0 {
			return time.Duration(sec) * time.Second
		}
	}
	const base = 5 * time.Second
	mult := 1 << uint(minInt(attemptIndex, 4))
	d := base * time.Duration(mult)
	if d > 90*time.Second {
		d = 90 * time.Second
	}
	return d
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Client sends requests to the Anthropic Messages API.
// Use NewClient to construct one.
type Client struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

// SetBaseURL overrides the API base URL (primarily for tests).
func (c *Client) SetBaseURL(baseURL string) {
	c.baseURL = strings.TrimRight(baseURL, "/")
}

// NewClient creates a Client for the given API key and model.
// model should be the full Anthropic Messages API model identifier.
func NewClient(apiKey, model string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: defaultBaseURL,
		model:   model,
		// Generous timeout: streaming responses for large tasks can be long.
		httpClient: &http.Client{Timeout: 10 * time.Minute},
	}
}

// Model returns the configured Anthropic model identifier.
func (c *Client) Model() string { return c.model }

// StreamRequest carries the parameters for a single API call.
type StreamRequest struct {
	// System is the system prompt. Empty string omits the field.
	System string
	// Messages is the full conversation history including the new user turn.
	Messages []Message
	// Tools is the set of tools the model may call. Nil omits the field.
	Tools []ToolDefinition
	// MaxTokens caps the response length. Zero uses defaultMaxTokens.
	MaxTokens int
}

// StreamMessage sends a streaming request and returns a Stream the caller
// must read and Close. The HTTP connection stays open until the stream is
// fully consumed or closed.
func (c *Client) StreamMessage(ctx context.Context, req StreamRequest) (*Stream, error) {
	if req.MaxTokens == 0 {
		req.MaxTokens = defaultMaxTokens
	}

	body, err := c.buildRequestBody(req)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	var lastErr error
	for attempt := 0; ; attempt++ {
		httpReq, err := http.NewRequestWithContext(
			ctx, http.MethodPost,
			c.baseURL+"/v1/messages",
			bytes.NewReader(body),
		)
		if err != nil {
			return nil, fmt.Errorf("create http request: %w", err)
		}

		httpReq.Header.Set("x-api-key", c.apiKey)
		httpReq.Header.Set("anthropic-version", anthropicVersion)
		httpReq.Header.Set("content-type", "application/json")
		httpReq.Header.Set("accept", "text/event-stream")

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("do request: %w", err)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("api error %d: %s", resp.StatusCode, errBody)
			if attempt >= maxStream429Attempts-1 {
				return nil, lastErr
			}
			delay := retryDelay429(resp.Header.Get("Retry-After"), attempt)
			if err := waitBefore429Retry(ctx, delay); err != nil {
				return nil, err
			}
			continue
		}

		if resp.StatusCode != http.StatusOK {
			defer resp.Body.Close()
			errBody, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("api error %d: %s", resp.StatusCode, errBody)
		}

		return newStream(resp.Body), nil
	}
}

// buildRequestBody marshals the request into the JSON wire format.
func (c *Client) buildRequestBody(req StreamRequest) ([]byte, error) {
	body := map[string]any{
		"model":      c.model,
		"max_tokens": req.MaxTokens,
		"stream":     true,
		"messages":   marshalMessages(req.Messages),
	}
	if req.System != "" {
		body["system"] = req.System
	}
	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
	}
	return json.Marshal(body)
}

// marshalMessages converts our typed Message slice into the wire format.
// We marshal manually (rather than json.Marshal on our structs) so we can
// control the exact field names and omit zero-value fields.
func marshalMessages(msgs []Message) []map[string]any {
	out := make([]map[string]any, len(msgs))
	for i, m := range msgs {
		role := "user"
		if m.Role == RoleAssistant {
			role = "assistant"
		}

		content := make([]map[string]any, len(m.Content))
		for j, block := range m.Content {
			switch b := block.(type) {
			case TextBlock:
				content[j] = map[string]any{
					"type": "text",
					"text": b.Text,
				}
			case ToolUseBlock:
				// Tool use blocks appear in assistant messages only.
				// Input is already valid JSON from when we accumulated it.
				content[j] = map[string]any{
					"type":  "tool_use",
					"id":    b.ID,
					"name":  b.Name,
					"input": b.Input,
				}
			case ToolResultBlock:
				// Tool result blocks appear in user messages only.
				content[j] = map[string]any{
					"type":        "tool_result",
					"tool_use_id": b.ToolUseID,
					"content":     b.Content,
					"is_error":    b.IsError,
				}
			}
		}

		out[i] = map[string]any{
			"role":    role,
			"content": content,
		}
	}
	return out
}
