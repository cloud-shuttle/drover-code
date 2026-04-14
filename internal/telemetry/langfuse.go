// Package telemetry provides LLM observability via Langfuse.
//
// It implements the Langfuse ingestion API directly — no SDK dependency.
// All methods are fire-and-forget: they enqueue events and flush them
// asynchronously. The agent loop is never blocked by telemetry.
//
// Usage:
//
//	tracer := telemetry.New(telemetry.Config{
//	    PublicKey: os.Getenv("LANGFUSE_PUBLIC_KEY"),
//	    SecretKey: os.Getenv("LANGFUSE_SECRET_KEY"),
//	    Host:      "https://cloud.langfuse.com", // or self-hosted
//	})
//	defer tracer.Flush()
package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// Config holds Langfuse credentials and options.
type Config struct {
	PublicKey string
	SecretKey string
	// Host is the Langfuse server URL.
	// Defaults to https://cloud.langfuse.com
	Host string
	// Debug logs every event that would be sent.
	Debug bool
	// Disabled turns the tracer into a no-op without changing call sites.
	Disabled bool
}

// ConfigFromEnv reads configuration from standard environment variables:
//
//	LANGFUSE_PUBLIC_KEY
//	LANGFUSE_SECRET_KEY
//	LANGFUSE_HOST  (optional, defaults to cloud.langfuse.com)
func ConfigFromEnv() Config {
	host := os.Getenv("LANGFUSE_HOST")
	if host == "" {
		host = "https://cloud.langfuse.com"
	}
	return Config{
		PublicKey: os.Getenv("LANGFUSE_PUBLIC_KEY"),
		SecretKey: os.Getenv("LANGFUSE_SECRET_KEY"),
		Host:      host,
		Disabled:  os.Getenv("LANGFUSE_PUBLIC_KEY") == "",
	}
}

// TraceID is a unique identifier for a trace (one agent Run() call).
type TraceID = string

// SpanID is a unique identifier for a span or generation.
type SpanID = string

// Tracer is the main entry point. Create one per process, reuse everywhere.
type Tracer struct {
	cfg       Config
	client    *http.Client
	mu        sync.Mutex
	queue     []event
	done      chan struct{}
	flushOnce sync.Once
}

type event struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Body      any       `json:"body"`
}

// New creates a Tracer. Call defer tracer.Flush() after creation.
// When LANGFUSE_PUBLIC_KEY is unset (ConfigFromEnv), returns Noop().
func New(cfg Config) *Tracer {
	if cfg.Disabled {
		return Noop()
	}
	t := &Tracer{
		cfg: cfg,
		done: make(chan struct{}),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
	go t.flushLoop()
	return t
}

// TraceParams configures a new trace.
type TraceParams struct {
	// ID is the trace identifier. If empty, one is generated.
	ID string
	// Name is a human-readable name for the trace, e.g. "agent-run".
	Name string
	// UserID associates the trace with a user (e.g. GitHub username).
	UserID string
	// SessionID groups traces from the same conversation session.
	SessionID string
	// Input is the user's message.
	Input any
	// Tags for filtering in the Langfuse UI.
	Tags []string
	// Metadata is arbitrary JSON attached to the trace.
	Metadata map[string]any
}

// StartTrace records the start of an agent run and returns the TraceID.
// Call EndTrace when the run finishes.
func (t *Tracer) StartTrace(params TraceParams) TraceID {
	if t.cfg.Disabled {
		return params.ID
	}
	if params.ID == "" {
		params.ID = newID()
	}
	t.enqueue("trace-create", params.ID, map[string]any{
		"id":        params.ID,
		"name":      params.Name,
		"userId":    params.UserID,
		"sessionId": params.SessionID,
		"input":     params.Input,
		"tags":      params.Tags,
		"metadata":  params.Metadata,
		"timestamp": time.Now(),
	})
	return params.ID
}

// EndTrace records the completion of a trace with its final output.
func (t *Tracer) EndTrace(traceID TraceID, output any, metadata map[string]any) {
	if t.cfg.Disabled {
		return
	}
	t.enqueue("trace-update", traceID, map[string]any{
		"id":       traceID,
		"output":   output,
		"metadata": metadata,
	})
}

// GenerationParams configures a new generation event.
type GenerationParams struct {
	TraceID  TraceID
	ParentID SpanID // optional: nest inside a span
	Name     string // e.g. "stream-response-turn-3"
	Model    string // Anthropic Messages API model id
	Input    any    // the messages sent to the API
	System   string // system prompt
	MaxTokens int
}

// StartGeneration records the start of an API call and returns a SpanID.
// Call EndGeneration when the call completes.
func (t *Tracer) StartGeneration(params GenerationParams) SpanID {
	if t.cfg.Disabled {
		return ""
	}
	id := newID()
	t.enqueue("generation-create", id, map[string]any{
		"id":                  id,
		"traceId":           params.TraceID,
		"parentObservationId": params.ParentID,
		"name":              params.Name,
		"model":             params.Model,
		"startTime":         time.Now(),
		"input":             params.Input,
		"metadata": map[string]any{
			"systemPromptLen": len(params.System),
			"maxTokens":       params.MaxTokens,
		},
	})
	return id
}

// GenerationResult carries the outcome of a generation for EndGeneration.
type GenerationResult struct {
	Output       string
	InputTokens  int
	OutputTokens int
	StopReason   string
	Error        error
}

// EndGeneration records the completion of an API call with token usage.
func (t *Tracer) EndGeneration(spanID SpanID, result GenerationResult) {
	if t.cfg.Disabled || spanID == "" {
		return
	}
	body := map[string]any{
		"id":      spanID,
		"endTime": time.Now(),
		"output":  result.Output,
		"usage": map[string]any{
			"input":  result.InputTokens,
			"output": result.OutputTokens,
			"total":  result.InputTokens + result.OutputTokens,
			"unit":   "TOKENS",
		},
		"metadata": map[string]any{
			"stopReason": result.StopReason,
		},
	}
	if result.Error != nil {
		body["level"] = "ERROR"
		body["statusMessage"] = result.Error.Error()
	}
	t.enqueue("generation-update", spanID, body)
}

// SpanParams configures a new span event.
type SpanParams struct {
	TraceID  TraceID
	ParentID SpanID // optional: nest inside another span or generation
	Name     string // tool name, e.g. "bash"
	Input    any    // tool input
	Metadata map[string]any
}

// StartSpan records the start of a tool call and returns a SpanID.
func (t *Tracer) StartSpan(params SpanParams) SpanID {
	if t.cfg.Disabled {
		return ""
	}
	id := newID()
	t.enqueue("span-create", id, map[string]any{
		"id":                  id,
		"traceId":           params.TraceID,
		"parentObservationId": params.ParentID,
		"name":              params.Name,
		"startTime":         time.Now(),
		"input":             params.Input,
		"metadata":          params.Metadata,
	})
	return id
}

// SpanResult carries the outcome of a span for EndSpan.
type SpanResult struct {
	Output  string
	IsError bool
	Error   error
}

// EndSpan records the completion of a tool execution.
func (t *Tracer) EndSpan(spanID SpanID, result SpanResult) {
	if t.cfg.Disabled || spanID == "" {
		return
	}
	level := "DEFAULT"
	if result.IsError {
		level = "ERROR"
	}
	body := map[string]any{
		"id":      spanID,
		"endTime": time.Now(),
		"output":  result.Output,
		"level":   level,
	}
	if result.Error != nil {
		body["statusMessage"] = result.Error.Error()
	}
	t.enqueue("span-update", spanID, body)
}

// ScoreSource indicates who or what produced the score.
type ScoreSource string

const (
	ScoreSourceHuman ScoreSource = "HUMAN"
	ScoreSourceModel ScoreSource = "MODEL"
	ScoreSourceAPI   ScoreSource = "API"
)

// Score attaches an evaluation result to a trace.
func (t *Tracer) Score(traceID TraceID, name string, value float64, source ScoreSource, comment string) {
	if t.cfg.Disabled {
		return
	}
	eid := newID()
	t.enqueue("score-create", eid, map[string]any{
		"id":        eid,
		"traceId":   traceID,
		"name":      name,
		"value":     value,
		"source":    string(source),
		"comment":   comment,
		"timestamp": time.Now(),
	})
}

// Flush sends all queued events. Call at program shutdown:
//
//	tracer := telemetry.New(cfg)
//	defer tracer.Flush()
func (t *Tracer) Flush() {
	t.flushOnce.Do(func() {
		close(t.done)
		t.flush()
	})
}

func (t *Tracer) flushLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			t.flush()
		case <-t.done:
			return
		}
	}
}

func (t *Tracer) flush() {
	t.mu.Lock()
	if len(t.queue) == 0 {
		t.mu.Unlock()
		return
	}
	batch := t.queue
	t.queue = nil
	t.mu.Unlock()

	if t.cfg.Debug {
		for _, e := range batch {
			log.Printf("[langfuse] %s id=%s", e.Type, e.ID)
		}
	}

	payload := map[string]any{"batch": batch}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("langfuse: marshal: %v", err)
		return
	}

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		t.cfg.Host+"/api/public/ingestion",
		bytes.NewReader(body),
	)
	if err != nil {
		log.Printf("langfuse: create request: %v", err)
		return
	}
	req.SetBasicAuth(t.cfg.PublicKey, t.cfg.SecretKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		log.Printf("langfuse: send batch: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		log.Printf("langfuse: ingestion HTTP %d", resp.StatusCode)
	}
}

func (t *Tracer) enqueue(typ, id string, body any) {
	t.mu.Lock()
	t.queue = append(t.queue, event{
		ID:        id,
		Type:      typ,
		Timestamp: time.Now(),
		Body:      body,
	})
	t.mu.Unlock()
}

func newID() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())
}

// Noop returns a disabled Tracer that records nothing.
func Noop() *Tracer {
	return &Tracer{cfg: Config{Disabled: true}, done: make(chan struct{})}
}
