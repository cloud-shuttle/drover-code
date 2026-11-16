package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

type Client struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	mu      sync.Mutex
	nextID  atomic.Uint64
	pending map[uint64]chan Response
}

type Request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ResponseError  `json:"error,omitempty"`
}

type ResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type Notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

func NewClient(command []string) *Client {
	return &Client{
		cmd:     exec.Command(command[0], command[1:]...),
		pending: make(map[uint64]chan Response),
	}
}

func (c *Client) Start(ctx context.Context) error {
	var err error
	if c.stdin, err = c.cmd.StdinPipe(); err != nil {
		return err
	}
	if c.stdout, err = c.cmd.StdoutPipe(); err != nil {
		return err
	}
	if err := c.cmd.Start(); err != nil {
		return err
	}
	go c.readLoop()

	// Initialize
	var initRes map[string]any
	if err := c.Call(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"clientInfo": map[string]string{
			"name":    "drover-code",
			"version": "1.0.0",
		},
		"capabilities": map[string]any{},
	}, &initRes); err != nil {
		return fmt.Errorf("mcp initialize: %w", err)
	}

	if err := c.Notify(ctx, "notifications/initialized", nil); err != nil {
		return fmt.Errorf("mcp initialized: %w", err)
	}

	return nil
}

func (c *Client) Stop() error {
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.cmd.Process != nil {
		return c.cmd.Process.Kill()
	}
	return nil
}

func (c *Client) readLoop() {
	scanner := bufio.NewScanner(c.stdout)
	for scanner.Scan() {
		line := scanner.Bytes()
		var res Response
		if err := json.Unmarshal(line, &res); err == nil && res.ID != 0 {
			c.mu.Lock()
			ch, ok := c.pending[res.ID]
			if ok {
				delete(c.pending, res.ID)
			}
			c.mu.Unlock()
			if ok {
				ch <- res
			}
		}
	}
}

func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	id := c.nextID.Add(1)
	req := Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	b = append(b, '\n')

	ch := make(chan Response, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if _, err := c.stdin.Write(b); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case res := <-ch:
		if res.Error != nil {
			return fmt.Errorf("mcp error: %s (code %d)", res.Error.Message, res.Error.Code)
		}
		if result != nil && res.Result != nil {
			return json.Unmarshal(res.Result, result)
		}
		return nil
	}
}

func (c *Client) Notify(ctx context.Context, method string, params any) error {
	notif := Notification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	b, err := json.Marshal(notif)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = c.stdin.Write(b)
	return err
}
