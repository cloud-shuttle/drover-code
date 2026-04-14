package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudshuttle/drover-code/internal/api"
	"github.com/cloudshuttle/drover-code/internal/convo"
	"github.com/cloudshuttle/drover-code/internal/permissions"
	"github.com/cloudshuttle/drover-code/internal/tools"
	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

type sleepTool struct{}

func (t *sleepTool) Name() string        { return "sleep" }
func (t *sleepTool) Description() string { return "sleep for ms" }
func (t *sleepTool) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("ms", toolutil.NewSchema("integer")).
		Required("ms").
		Build()
}
func (t *sleepTool) NeedsPermission(_ json.RawMessage) bool { return false }
func (t *sleepTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var v struct {
		MS int `json:"ms"`
	}
	_ = json.Unmarshal(input, &v)
	select {
	case <-time.After(time.Duration(v.MS) * time.Millisecond):
	case <-ctx.Done():
		return "", ctx.Err()
	}
	return "slept", nil
}

type needsPermTool struct {
	calls atomic.Int64
}

func (t *needsPermTool) Name() string        { return "needs_perm" }
func (t *needsPermTool) Description() string { return "test tool" }
func (t *needsPermTool) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").Build()
}
func (t *needsPermTool) NeedsPermission(_ json.RawMessage) bool { return true }
func (t *needsPermTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	t.calls.Add(1)
	return "ok", nil
}

func TestLoop_ExecutesToolThenCompletes(t *testing.T) {
	var reqCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		n := reqCount.Add(1)

		if n == 1 {
			// First response: a single tool_use (sleep 1ms), then stop.
			io.WriteString(w, "event: content_block_start\n")
			io.WriteString(w, `data: {"index":0,"content_block":{"type":"tool_use","id":"tu_1","name":"sleep","input":{}}}`+"\n\n")
			io.WriteString(w, "event: content_block_delta\n")
			io.WriteString(w, `data: {"index":0,"delta":{"type":"input_json_delta","partial_json":"{\"ms\":1}"}}`+"\n\n")
			io.WriteString(w, "event: content_block_stop\n")
			io.WriteString(w, `data: {"index":0}`+"\n\n")
			io.WriteString(w, "event: message_delta\n")
			io.WriteString(w, `data: {"delta":{"stop_reason":"tool_use"},"usage":{"input_tokens":1,"output_tokens":1}}`+"\n\n")
			io.WriteString(w, "event: message_stop\n")
			io.WriteString(w, "data: {}\n\n")
			return
		}

		// Second response: final text.
		io.WriteString(w, "event: content_block_start\n")
		io.WriteString(w, `data: {"index":0,"content_block":{"type":"text","text":""}}`+"\n\n")
		io.WriteString(w, "event: content_block_delta\n")
		io.WriteString(w, `data: {"index":0,"delta":{"type":"text_delta","text":"done"}}`+"\n\n")
		io.WriteString(w, "event: content_block_stop\n")
		io.WriteString(w, `data: {"index":0}`+"\n\n")
		io.WriteString(w, "event: message_delta\n")
		io.WriteString(w, `data: {"delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":1,"output_tokens":1}}`+"\n\n")
		io.WriteString(w, "event: message_stop\n")
		io.WriteString(w, "data: {}\n\n")
	}))
	defer srv.Close()

	client := api.NewClient("k", "m")
	client.SetBaseURL(srv.URL)

	mgr := convo.NewManagerWithSystem("sys")
	reg := tools.NewRegistry()
	reg.Register(&sleepTool{})

	events := make(chan Event, 256)
	eng := permissions.NewEngine(permissions.ModeBypass, nil, nil, "", tools.AllowAll)
	loop := NewLoop(client, mgr, reg, eng, events)

	var toolStarts, toolDones, done int
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for ev := range events {
			switch ev.(type) {
			case ToolStartEvent:
				toolStarts++
			case ToolDoneEvent:
				toolDones++
			case DoneEvent:
				done++
			}
		}
	}()

	if err := loop.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	close(events)
	<-drainDone

	if toolStarts != 1 || toolDones != 1 {
		t.Fatalf("expected 1 tool start/done, got starts=%d dones=%d", toolStarts, toolDones)
	}
	if done != 1 {
		t.Fatalf("expected DoneEvent, got %d", done)
	}
	if loop.LastRunTurns() != 2 {
		t.Fatalf("expected 2 agentic turns (tool + final text), got %d", loop.LastRunTurns())
	}
}

// TestLoop_TwoSequentialUserTurns covers the same agent/convo stack headless uses
// for multiple stdin lines (doc 11 / doc 13).
func TestLoop_TwoSequentialUserTurns(t *testing.T) {
	var reqN atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		n := reqN.Add(1)
		text := "first-reply"
		if n == 2 {
			text = "second-reply"
		}
		writeSSETextResponse(w, text)
	}))
	defer srv.Close()

	client := api.NewClient("k", "m")
	client.SetBaseURL(srv.URL)

	mgr := convo.NewManagerWithSystem("sys")
	reg := tools.NewRegistry()
	events := make(chan Event, 256)
	eng := permissions.NewEngine(permissions.ModeBypass, nil, nil, "", tools.AllowAll)
	loop := NewLoop(client, mgr, reg, eng, events)

	go func() {
		for range events {
		}
	}()

	if err := loop.Run(context.Background(), "line one"); err != nil {
		t.Fatalf("Run 1: %v", err)
	}
	if err := loop.Run(context.Background(), "line two"); err != nil {
		t.Fatalf("Run 2: %v", err)
	}
	close(events)

	msgs := mgr.Messages()
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages (user, asst, user, asst), got %d", len(msgs))
	}
	if msgs[0].Role != api.RoleUser || msgs[1].Role != api.RoleAssistant ||
		msgs[2].Role != api.RoleUser || msgs[3].Role != api.RoleAssistant {
		t.Fatalf("role sequence: %+v", msgs)
	}
	if reqN.Load() != 2 {
		t.Fatalf("expected 2 API calls, got %d", reqN.Load())
	}
}

func writeSSETextResponse(w io.Writer, text string) {
	enc, _ := json.Marshal(text)
	io.WriteString(w, "event: content_block_start\n")
	io.WriteString(w, `data: {"index":0,"content_block":{"type":"text","text":""}}`+"\n\n")
	io.WriteString(w, "event: content_block_delta\n")
	io.WriteString(w, `data: {"index":0,"delta":{"type":"text_delta","text":`+string(enc)+`}}`+"\n\n")
	io.WriteString(w, "event: content_block_stop\n")
	io.WriteString(w, `data: {"index":0}`+"\n\n")
	io.WriteString(w, "event: message_delta\n")
	io.WriteString(w, `data: {"delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":1,"output_tokens":1}}`+"\n\n")
	io.WriteString(w, "event: message_stop\n")
	io.WriteString(w, "data: {}\n\n")
}

func TestLoop_TokenBudgetExceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: content_block_start\n")
		io.WriteString(w, `data: {"index":0,"content_block":{"type":"text","text":""}}`+"\n\n")
		io.WriteString(w, "event: content_block_delta\n")
		io.WriteString(w, `data: {"index":0,"delta":{"type":"text_delta","text":"x"}}`+"\n\n")
		io.WriteString(w, "event: content_block_stop\n")
		io.WriteString(w, `data: {"index":0}`+"\n\n")
		io.WriteString(w, "event: message_delta\n")
		io.WriteString(w, `data: {"delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":5000,"output_tokens":60}}`+"\n\n")
		io.WriteString(w, "event: message_stop\n")
		io.WriteString(w, "data: {}\n\n")
	}))
	defer srv.Close()

	client := api.NewClient("k", "m")
	client.SetBaseURL(srv.URL)
	mgr := convo.NewManagerWithSystem("sys")
	reg := tools.NewRegistry()
	events := make(chan Event, 256)
	eng := permissions.NewEngine(permissions.ModeBypass, nil, nil, "", tools.AllowAll)
	loop := NewLoop(client, mgr, reg, eng, events)
	loop.SetMaxSessionTokens(50)
	go func() {
		for range events {
		}
	}()

	err := loop.Run(context.Background(), "go")
	close(events)
	if !errors.Is(err, ErrTokenBudgetExceeded) {
		t.Fatalf("expected ErrTokenBudgetExceeded, got %v", err)
	}
}

func TestLoop_ContextDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		<-r.Context().Done()
	}))
	defer srv.Close()

	client := api.NewClient("k", "m")
	client.SetBaseURL(srv.URL)
	mgr := convo.NewManagerWithSystem("sys")
	reg := tools.NewRegistry()
	events := make(chan Event, 256)
	eng := permissions.NewEngine(permissions.ModeBypass, nil, nil, "", tools.AllowAll)
	loop := NewLoop(client, mgr, reg, eng, events)
	go func() {
		for range events {
		}
	}()

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	err := loop.Run(ctx, "go")
	close(events)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}

func TestLoop_HeartbeatDuringSlowTool(t *testing.T) {
	t.Setenv("DROVER_CODE_HEARTBEAT_INTERVAL_SECS", "1")

	var reqCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		n := reqCount.Add(1)

		if n == 1 {
			io.WriteString(w, "event: content_block_start\n")
			io.WriteString(w, `data: {"index":0,"content_block":{"type":"tool_use","id":"tu_1","name":"sleep","input":{}}}`+"\n\n")
			io.WriteString(w, "event: content_block_delta\n")
			io.WriteString(w, `data: {"index":0,"delta":{"type":"input_json_delta","partial_json":"{\"ms\":2500}"}}`+"\n\n")
			io.WriteString(w, "event: content_block_stop\n")
			io.WriteString(w, `data: {"index":0}`+"\n\n")
			io.WriteString(w, "event: message_delta\n")
			io.WriteString(w, `data: {"delta":{"stop_reason":"tool_use"},"usage":{"input_tokens":1,"output_tokens":1}}`+"\n\n")
			io.WriteString(w, "event: message_stop\n")
			io.WriteString(w, "data: {}\n\n")
			return
		}

		io.WriteString(w, "event: content_block_start\n")
		io.WriteString(w, `data: {"index":0,"content_block":{"type":"text","text":""}}`+"\n\n")
		io.WriteString(w, "event: content_block_delta\n")
		io.WriteString(w, `data: {"index":0,"delta":{"type":"text_delta","text":"done"}}`+"\n\n")
		io.WriteString(w, "event: content_block_stop\n")
		io.WriteString(w, `data: {"index":0}`+"\n\n")
		io.WriteString(w, "event: message_delta\n")
		io.WriteString(w, `data: {"delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":1,"output_tokens":1}}`+"\n\n")
		io.WriteString(w, "event: message_stop\n")
		io.WriteString(w, "data: {}\n\n")
	}))
	defer srv.Close()

	client := api.NewClient("k", "m")
	client.SetBaseURL(srv.URL)

	mgr := convo.NewManagerWithSystem("sys")
	reg := tools.NewRegistry()
	reg.Register(&sleepTool{})

	events := make(chan Event, 256)
	eng := permissions.NewEngine(permissions.ModeBypass, nil, nil, "", tools.AllowAll)
	loop := NewLoop(client, mgr, reg, eng, events)

	var heartbeats int
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for ev := range events {
			if _, ok := ev.(HeartbeatEvent); ok {
				heartbeats++
			}
		}
	}()

	if err := loop.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	close(events)
	<-drainDone

	if heartbeats < 1 {
		t.Fatalf("expected at least 1 HeartbeatEvent during slow tool, got %d", heartbeats)
	}
}

func TestLoop_ExecutesToolsInParallel(t *testing.T) {
	var reqCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		n := reqCount.Add(1)

		if n == 1 {
			// Two tool calls in one assistant response.
			io.WriteString(w, "event: content_block_start\n")
			io.WriteString(w, `data: {"index":0,"content_block":{"type":"tool_use","id":"tu_1","name":"sleep","input":{}}}`+"\n\n")
			io.WriteString(w, "event: content_block_delta\n")
			io.WriteString(w, `data: {"index":0,"delta":{"type":"input_json_delta","partial_json":"{\"ms\":120}"}}`+"\n\n")
			io.WriteString(w, "event: content_block_stop\n")
			io.WriteString(w, `data: {"index":0}`+"\n\n")

			io.WriteString(w, "event: content_block_start\n")
			io.WriteString(w, `data: {"index":1,"content_block":{"type":"tool_use","id":"tu_2","name":"sleep","input":{}}}`+"\n\n")
			io.WriteString(w, "event: content_block_delta\n")
			io.WriteString(w, `data: {"index":1,"delta":{"type":"input_json_delta","partial_json":"{\"ms\":120}"}}`+"\n\n")
			io.WriteString(w, "event: content_block_stop\n")
			io.WriteString(w, `data: {"index":1}`+"\n\n")

			io.WriteString(w, "event: message_stop\n")
			io.WriteString(w, "data: {}\n\n")
			return
		}

		// Final response.
		io.WriteString(w, "event: content_block_start\n")
		io.WriteString(w, `data: {"index":0,"content_block":{"type":"text","text":""}}`+"\n\n")
		io.WriteString(w, "event: content_block_delta\n")
		io.WriteString(w, `data: {"index":0,"delta":{"type":"text_delta","text":"ok"}}`+"\n\n")
		io.WriteString(w, "event: content_block_stop\n")
		io.WriteString(w, `data: {"index":0}`+"\n\n")
		io.WriteString(w, "event: message_stop\n")
		io.WriteString(w, "data: {}\n\n")
	}))
	defer srv.Close()

	client := api.NewClient("k", "m")
	client.SetBaseURL(srv.URL)

	mgr := convo.NewManagerWithSystem("sys")
	reg := tools.NewRegistry()
	reg.Register(&sleepTool{})

	events := make(chan Event, 256)
	eng := permissions.NewEngine(permissions.ModeBypass, nil, nil, "", tools.AllowAll)
	loop := NewLoop(client, mgr, reg, eng, events)
	go func() {
		for range events {
		}
	}()

	start := time.Now()
	if err := loop.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	elapsed := time.Since(start)
	close(events)

	// If sequential, we'd expect ~240ms+. With parallel, should be closer to ~120ms.
	if elapsed > 210*time.Millisecond {
		t.Fatalf("expected parallel execution, took %s", elapsed)
	}
}

func TestLoop_ModePlan_BatchAllow(t *testing.T) {
	var reqCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		n := reqCount.Add(1)

		if n == 1 {
			io.WriteString(w, "event: content_block_start\n")
			io.WriteString(w, `data: {"index":0,"content_block":{"type":"tool_use","id":"tu_1","name":"needs_perm","input":{}}}`+"\n\n")
			io.WriteString(w, "event: content_block_stop\n")
			io.WriteString(w, `data: {"index":0}`+"\n\n")
			io.WriteString(w, "event: message_stop\n")
			io.WriteString(w, "data: {}\n\n")
			return
		}

		io.WriteString(w, "event: content_block_start\n")
		io.WriteString(w, `data: {"index":0,"content_block":{"type":"text","text":""}}`+"\n\n")
		io.WriteString(w, "event: content_block_delta\n")
		io.WriteString(w, `data: {"index":0,"delta":{"type":"text_delta","text":"done"}}`+"\n\n")
		io.WriteString(w, "event: content_block_stop\n")
		io.WriteString(w, `data: {"index":0}`+"\n\n")
		io.WriteString(w, "event: message_stop\n")
		io.WriteString(w, "data: {}\n\n")
	}))
	defer srv.Close()

	client := api.NewClient("k", "m")
	client.SetBaseURL(srv.URL)

	mgr := convo.NewManagerWithSystem("sys")
	reg := tools.NewRegistry()
	tool := &needsPermTool{}
	reg.Register(tool)

	events := make(chan Event, 256)
	eng := permissions.NewEngine(
		permissions.ModePlan,
		nil,
		nil,
		"",
		func(context.Context, tools.PermissionRequest) tools.Decision {
			t.Fatalf("unexpected per-tool prompt in plan mode")
			return tools.Deny
		},
	)
	loop := NewLoop(client, mgr, reg, eng, events)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ev := range events {
			if br, ok := ev.(PermissionBatchRequestEvent); ok {
				br.DecisionCh <- PermAllow
			}
		}
	}()

	if err := loop.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	close(events)
	wg.Wait()

	if tool.calls.Load() != 1 {
		t.Fatalf("expected tool to execute once, got %d", tool.calls.Load())
	}
}

func TestLoop_ModePlan_BatchDeny(t *testing.T) {
	var reqCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		n := reqCount.Add(1)

		if n == 1 {
			io.WriteString(w, "event: content_block_start\n")
			io.WriteString(w, `data: {"index":0,"content_block":{"type":"tool_use","id":"tu_1","name":"needs_perm","input":{}}}`+"\n\n")
			io.WriteString(w, "event: content_block_stop\n")
			io.WriteString(w, `data: {"index":0}`+"\n\n")
			io.WriteString(w, "event: message_stop\n")
			io.WriteString(w, "data: {}\n\n")
			return
		}

		io.WriteString(w, "event: content_block_start\n")
		io.WriteString(w, `data: {"index":0,"content_block":{"type":"text","text":""}}`+"\n\n")
		io.WriteString(w, "event: content_block_delta\n")
		io.WriteString(w, `data: {"index":0,"delta":{"type":"text_delta","text":"done"}}`+"\n\n")
		io.WriteString(w, "event: content_block_stop\n")
		io.WriteString(w, `data: {"index":0}`+"\n\n")
		io.WriteString(w, "event: message_stop\n")
		io.WriteString(w, "data: {}\n\n")
	}))
	defer srv.Close()

	client := api.NewClient("k", "m")
	client.SetBaseURL(srv.URL)

	mgr := convo.NewManagerWithSystem("sys")
	reg := tools.NewRegistry()
	tool := &needsPermTool{}
	reg.Register(tool)

	events := make(chan Event, 256)
	eng := permissions.NewEngine(
		permissions.ModePlan,
		nil,
		nil,
		"",
		func(context.Context, tools.PermissionRequest) tools.Decision {
			t.Fatalf("unexpected per-tool prompt in plan mode")
			return tools.Deny
		},
	)
	loop := NewLoop(client, mgr, reg, eng, events)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ev := range events {
			if br, ok := ev.(PermissionBatchRequestEvent); ok {
				br.DecisionCh <- PermDeny
			}
		}
	}()

	if err := loop.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	close(events)
	wg.Wait()

	if tool.calls.Load() != 0 {
		t.Fatalf("expected tool not to execute, got %d", tool.calls.Load())
	}
}

func textStreamResponse(w io.Writer, text string) {
	io.WriteString(w, "event: content_block_start\n")
	io.WriteString(w, `data: {"index":0,"content_block":{"type":"text","text":""}}`+"\n\n")
	io.WriteString(w, "event: content_block_delta\n")
	io.WriteString(w, `data: {"index":0,"delta":{"type":"text_delta","text":`+jsonQuote(text)+`}}`+"\n\n")
	io.WriteString(w, "event: content_block_stop\n")
	io.WriteString(w, `data: {"index":0}`+"\n\n")
	io.WriteString(w, "event: message_delta\n")
	io.WriteString(w, `data: {"delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":1,"output_tokens":1}}`+"\n\n")
	io.WriteString(w, "event: message_stop\n")
	io.WriteString(w, "data: {}\n\n")
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestLoop_CompactContext(t *testing.T) {
	var reqCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		n := reqCount.Add(1)
		if n != 1 {
			t.Fatalf("unexpected extra request %d", n)
		}
		body, _ := io.ReadAll(r.Body)
		if bytes.Contains(body, []byte(`"tools"`)) {
			t.Fatalf("compaction request should not include tools")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		textStreamResponse(w, "- bullet summary of earlier work")
	}))
	defer srv.Close()

	client := api.NewClient("k", "m")
	client.SetBaseURL(srv.URL)

	mgr := convo.NewManagerWithSystem("sys")
	for i := 0; i < 12; i++ {
		mgr.Append(api.UserMessage("msg"))
	}
	reg := tools.NewRegistry()
	reg.Register(&sleepTool{})
	events := make(chan Event, 64)
	eng := permissions.NewEngine(permissions.ModeBypass, nil, nil, "", tools.AllowAll)
	loop := NewLoop(client, mgr, reg, eng, events)

	if err := loop.CompactContext(context.Background()); err != nil {
		t.Fatalf("CompactContext: %v", err)
	}
	msgs := mgr.Messages()
	if len(msgs) != 9 {
		t.Fatalf("expected 9 messages after compact (1 summary + 8 tail), got %d", len(msgs))
	}
	first := msgs[0].Content[0].(api.TextBlock).Text
	if !strings.Contains(first, "Earlier conversation summary") || !strings.Contains(first, "bullet summary") {
		t.Fatalf("unexpected first message: %q", first)
	}
}

func TestLoop_AutoCompactionBeforeAgentTurn(t *testing.T) {
	var reqCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		n := reqCount.Add(1)
		switch n {
		case 1:
			body, _ := io.ReadAll(r.Body)
			if bytes.Contains(body, []byte(`"tools"`)) {
				t.Fatalf("compaction request should not include tools")
			}
			textStreamResponse(w, "compacted summary")
		default:
			io.WriteString(w, "event: content_block_start\n")
			io.WriteString(w, `data: {"index":0,"content_block":{"type":"text","text":""}}`+"\n\n")
			io.WriteString(w, "event: content_block_delta\n")
			io.WriteString(w, `data: {"index":0,"delta":{"type":"text_delta","text":"ok"}}`+"\n\n")
			io.WriteString(w, "event: content_block_stop\n")
			io.WriteString(w, `data: {"index":0}`+"\n\n")
			io.WriteString(w, "event: message_delta\n")
			io.WriteString(w, `data: {"delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":1,"output_tokens":1}}`+"\n\n")
			io.WriteString(w, "event: message_stop\n")
			io.WriteString(w, "data: {}\n\n")
		}
	}))
	defer srv.Close()

	client := api.NewClient("k", "m")
	client.SetBaseURL(srv.URL)

// Short turns so that after one compaction (summary + 8 tail) we're under the limit.
	mgr := convo.NewManagerWithSystem(strings.Repeat("s", 500))
	mgr.SetContextLimit(800)
	for i := 0; i < 15; i++ {
		mgr.Append(api.UserMessage(strings.Repeat("x", 220)))
	}

	reg := tools.NewRegistry()
	reg.Register(&sleepTool{})
	events := make(chan Event, 256)
	eng := permissions.NewEngine(permissions.ModeBypass, nil, nil, "", tools.AllowAll)
	loop := NewLoop(client, mgr, reg, eng, events)

	go func() {
		for range events {
		}
	}()

	if err := loop.Run(context.Background(), "final ask"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reqCount.Load() < 2 {
		t.Fatalf("expected at least 2 API calls (compaction + agent), got %d", reqCount.Load())
	}
}

func TestLoop_SkipAutoCompactionWhenDisabled(t *testing.T) {
	var reqCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		n := reqCount.Add(1)
		if n != 1 {
			t.Fatalf("unexpected extra request %d (auto-compaction should be off)", n)
		}
		body, _ := io.ReadAll(r.Body)
		if !bytes.Contains(body, []byte(`"tools"`)) {
			t.Fatalf("expected main agent stream with tools in body")
		}
		io.WriteString(w, "event: content_block_start\n")
		io.WriteString(w, `data: {"index":0,"content_block":{"type":"text","text":""}}`+"\n\n")
		io.WriteString(w, "event: content_block_delta\n")
		io.WriteString(w, `data: {"index":0,"delta":{"type":"text_delta","text":"ok"}}`+"\n\n")
		io.WriteString(w, "event: content_block_stop\n")
		io.WriteString(w, `data: {"index":0}`+"\n\n")
		io.WriteString(w, "event: message_delta\n")
		io.WriteString(w, `data: {"delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":1,"output_tokens":1}}`+"\n\n")
		io.WriteString(w, "event: message_stop\n")
		io.WriteString(w, "data: {}\n\n")
	}))
	defer srv.Close()

	client := api.NewClient("k", "m")
	client.SetBaseURL(srv.URL)

	mgr := convo.NewManagerWithSystem(strings.Repeat("s", 500))
	mgr.SetContextLimit(800)
	for i := 0; i < 15; i++ {
		mgr.Append(api.UserMessage(strings.Repeat("x", 220)))
	}

	reg := tools.NewRegistry()
	reg.Register(&sleepTool{})
	events := make(chan Event, 256)
	eng := permissions.NewEngine(permissions.ModeBypass, nil, nil, "", tools.AllowAll)
	loop := NewLoop(client, mgr, reg, eng, events)
	loop.SetAutoCompaction(false)

	go func() {
		for range events {
		}
	}()

	if err := loop.Run(context.Background(), "final ask"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reqCount.Load() != 1 {
		t.Fatalf("expected 1 API call with auto-compaction disabled, got %d", reqCount.Load())
	}
}
