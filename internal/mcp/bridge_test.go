package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"
)

func TestMCPTool_Execute(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	c := &Client{
		stdin:   inWriter,
		stdout:  outReader,
		pending: make(map[uint64]chan Response),
	}

	go c.readLoop()

	go func() {
		scanner := bufio.NewScanner(inReader)
		for scanner.Scan() {
			var req Request
			if err := json.Unmarshal(scanner.Bytes(), &req); err == nil {
				// Mock success response
				res := Response{
					JSONRPC: "2.0",
					ID:      req.ID,
				}
				if req.Method == "tools/call" {
					res.Result = json.RawMessage(`{"content":[{"type":"text","text":"hello world"}]}`)
				}
				b, _ := json.Marshal(res)
				fmt.Fprintf(outWriter, "%s\n", b)
			}
		}
	}()

	tool := &MCPTool{
		client:      c,
		name:        "mcp_test_tool",
		description: "Test tool",
		inputSchema: json.RawMessage(`{}`),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := tool.Execute(ctx, json.RawMessage(`{"arg":"value"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out != "hello world" {
		t.Errorf("expected 'hello world', got %q", out)
	}
}

func TestMCPTool_ExecuteError(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	c := &Client{
		stdin:   inWriter,
		stdout:  outReader,
		pending: make(map[uint64]chan Response),
	}

	go c.readLoop()

	go func() {
		scanner := bufio.NewScanner(inReader)
		for scanner.Scan() {
			var req Request
			if err := json.Unmarshal(scanner.Bytes(), &req); err == nil {
				// Mock error content response
				res := Response{
					JSONRPC: "2.0",
					ID:      req.ID,
				}
				if req.Method == "tools/call" {
					res.Result = json.RawMessage(`{"content":[{"type":"text","text":"something failed"}], "isError": true}`)
				}
				b, _ := json.Marshal(res)
				fmt.Fprintf(outWriter, "%s\n", b)
			}
		}
	}()

	tool := &MCPTool{client: c, name: "mcp_test_tool"}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := tool.Execute(ctx, json.RawMessage(`{}`))
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if out != "something failed" {
		t.Errorf("expected out 'something failed', got %q", out)
	}
}
