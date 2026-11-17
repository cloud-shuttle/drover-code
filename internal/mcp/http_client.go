package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// HTTPClient speaks streamable HTTP MCP: initialize, tools/list, and tools/call.
type HTTPClient struct {
	url     string
	headers map[string]string
	http    *http.Client
	nextID  atomic.Uint64
	session string
}

func NewHTTPClient(url string, headers map[string]string) *HTTPClient {
	return &HTTPClient{
		url:     strings.TrimSpace(url),
		headers: headers,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *HTTPClient) Start(ctx context.Context) error {
	var result map[string]any
	if err := c.Call(ctx, "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"clientInfo": map[string]string{
			"name":    "drover-code",
			"version": "1.0.0",
		},
		"capabilities": map[string]any{},
	}, &result); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	return c.Notify(ctx, "notifications/initialized", nil)
}

func (c *HTTPClient) Stop() error { return nil }

func (c *HTTPClient) Notify(ctx context.Context, method string, params any) error {
	body := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		body["params"] = params
	}
	_, err := c.post(ctx, body)
	return err
}

func (c *HTTPClient) Call(ctx context.Context, method string, params any, result any) error {
	id := c.nextID.Add(1)
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		body["params"] = params
	}
	raw, err := c.post(ctx, body)
	if err != nil {
		return err
	}
	msg, err := parseRPC(raw, id)
	if err != nil {
		return err
	}
	if msg.Error != nil {
		return fmt.Errorf("mcp error: %s (code %d)", msg.Error.Message, msg.Error.Code)
	}
	if result != nil && len(msg.Result) > 0 {
		return json.Unmarshal(msg.Result, result)
	}
	return nil
}

func (c *HTTPClient) post(ctx context.Context, body any) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2025-03-26")
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.session = sid
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("mcp http %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	return raw, nil
}

func parseRPC(raw []byte, id uint64) (Response, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return Response{}, fmt.Errorf("mcp: empty response")
	}
	if raw[0] == '{' {
		var msg Response
		if err := json.Unmarshal(raw, &msg); err != nil {
			return Response{}, err
		}
		return msg, nil
	}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 64*1024), 8<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var msg Response
		if err := json.Unmarshal([]byte(payload), &msg); err != nil {
			continue
		}
		if msg.ID == id || msg.Error != nil {
			return msg, nil
		}
	}
	return Response{}, fmt.Errorf("mcp: no json-rpc response in stream")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
