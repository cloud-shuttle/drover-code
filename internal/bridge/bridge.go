// Package bridge implements the IDE bridge: JSON-RPC over stdio.
package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Handler func(ctx context.Context, msg Message, send func(Message))

type Bridge struct {
	r        *bufio.Reader
	w        io.Writer
	wMu      sync.Mutex
	handlers map[string]Handler
	nextID   atomic.Int64

	pendingMu sync.Mutex
	pending   map[int64]chan Message
}

func NewBridge(r io.Reader, w io.Writer) *Bridge {
	return &Bridge{
		r:        bufio.NewReaderSize(r, 1024*1024),
		w:        w,
		handlers: make(map[string]Handler),
		pending:  make(map[int64]chan Message),
	}
}

func NewStdioBridge() *Bridge { return NewBridge(os.Stdin, os.Stdout) }

func (b *Bridge) Handle(method string, h Handler) { b.handlers[method] = h }

func (b *Bridge) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		msg, err := b.readMessage(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("bridge: read: %w", err)
		}

		if msg.ID != nil && msg.Method == "" {
			b.pendingMu.Lock()
			ch, ok := b.pending[*msg.ID]
			b.pendingMu.Unlock()
			if ok {
				ch <- msg
				continue
			}
		}

		h, ok := b.handlers[msg.Method]
		if !ok {
			if msg.ID != nil {
				b.SendError(*msg.ID, -32601, "method not found: "+msg.Method)
			}
			continue
		}

		go h(ctx, msg, b.Send)
	}
}

func (b *Bridge) Send(msg Message) {
	if msg.JSONRPC == "" {
		msg.JSONRPC = "2.0"
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	b.wMu.Lock()
	defer b.wMu.Unlock()
	fmt.Fprintf(b.w, "Content-Length: %d\r\n\r\n", len(data))
	_, _ = b.w.Write(data)
}

func (b *Bridge) Notify(method string, params any) {
	p, _ := json.Marshal(params)
	b.Send(Message{Method: method, Params: p})
}

func (b *Bridge) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := b.nextID.Add(1)
	p, _ := json.Marshal(params)

	respCh := make(chan Message, 1)
	b.pendingMu.Lock()
	b.pending[id] = respCh
	b.pendingMu.Unlock()

	defer func() {
		b.pendingMu.Lock()
		delete(b.pending, id)
		b.pendingMu.Unlock()
	}()

	b.Send(Message{ID: &id, Method: method, Params: p})

	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return nil, fmt.Errorf("bridge: rpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (b *Bridge) SendResult(id int64, result any) {
	r, _ := json.Marshal(result)
	b.Send(Message{ID: &id, Result: r})
}

func (b *Bridge) SendError(id int64, code int, message string) {
	b.Send(Message{ID: &id, Error: &RPCError{Code: code, Message: message}})
}

func (b *Bridge) readMessage(ctx context.Context) (Message, error) {
	type result struct {
		msg Message
		err error
	}
	ch := make(chan result, 1)
	go func() {
		msg, err := b.readMessageSync()
		ch <- result{msg, err}
	}()
	select {
	case res := <-ch:
		return res.msg, res.err
	case <-ctx.Done():
		go func() { <-ch }()
		return Message{}, ctx.Err()
	}
}

func (b *Bridge) readMessageSync() (Message, error) {
	var contentLength int
	for {
		line, err := b.r.ReadString('\n')
		if err != nil {
			return Message{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length:") {
			n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:")))
			if err != nil {
				return Message{}, fmt.Errorf("bad Content-Length: %w", err)
			}
			contentLength = n
		}
	}

	if contentLength == 0 {
		return Message{}, fmt.Errorf("missing Content-Length")
	}

	body := make([]byte, contentLength)
	if _, err := io.ReadFull(b.r, body); err != nil {
		return Message{}, fmt.Errorf("read body: %w", err)
	}

	var msg Message
	if err := json.Unmarshal(body, &msg); err != nil {
		return Message{}, fmt.Errorf("parse message: %w", err)
	}
	return msg, nil
}

func RegisterStandardHandlers(b *Bridge, agentFn func(ctx context.Context, input string) (string, error)) {
	b.Handle("initialize", func(ctx context.Context, msg Message, send func(Message)) {
		if msg.ID == nil {
			return
		}
		send(Message{
			ID: msg.ID,
			Result: mustMarshal(map[string]any{
				"capabilities": map[string]any{
					"execute":      true,
					"streamTokens": true,
				},
				"serverInfo": map[string]any{
					"name":    "drover-code",
					"version": "0.1.0",
				},
			}),
		})
	})

	b.Handle("drover/execute", func(ctx context.Context, msg Message, send func(Message)) {
		if msg.ID == nil {
			return
		}
		var params struct {
			Input string `json:"input"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			b.SendError(*msg.ID, -32602, "invalid params: "+err.Error())
			return
		}

		result, err := agentFn(ctx, params.Input)
		if err != nil {
			b.SendError(*msg.ID, -32603, err.Error())
			return
		}
		send(Message{ID: msg.ID, Result: mustMarshal(map[string]any{"output": result})})
	})

	b.Handle("ping", func(ctx context.Context, msg Message, send func(Message)) {
		if msg.ID != nil {
			send(Message{ID: msg.ID, Result: mustMarshal("pong")})
		}
	})
}

func mustMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

