package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/cloudshuttle/drover-code/internal/api"
	"github.com/cloudshuttle/drover-code/internal/convo"
	"github.com/cloudshuttle/drover-code/pkg/outcomesignal"
	"github.com/cloudshuttle/drover-code/internal/telemetry"
	"github.com/cloudshuttle/drover-code/internal/tools"
	"github.com/cloudshuttle/drover-code/internal/warden"
	droverwarden "github.com/cloud-shuttle/drover-warden/warden"
)

const (
	compactionKeepTail      = 8
	maxCompactionBodyRunes  = 400_000
	maxCompactionToolResult = 2_000
	maxCompactionRounds     = 3
)

const compactionSystemPrompt = `You summarize prior conversation for continuing work as background context.
Output concise bullet points only. Preserve important file paths, shell commands, errors, and decisions.
Do not ask questions or refuse.`

// Loop is the agentic execution engine.
//
// It implements the plan → act → observe cycle:
//  1. Send current conversation to the API (streaming).
//  2. Collect the response — text blocks and/or tool call blocks.
//  3. If the response contains tool calls, execute them (in parallel),
//     append results to context, go to 1.
//  4. If the response is pure text, emit DoneEvent and return.
//
// The Loop sends Events on EventCh as things happen. The consumer
// (TUI or simple CLI printer) should drain EventCh on a separate goroutine.
//
// A Loop is NOT safe for concurrent use — run one input at a time.
// Coordinator mode achieves parallelism by creating separate Loop instances
// per worker agent, each with its own convo.Manager.
type Loop struct {
	driver   InferenceDriver
	convo    *convo.Manager
	executor ToolExecutor
	registry *tools.Registry
	eventCh  chan<- Event

	// cumulative token counters for the session
	totalInput  int
	totalOutput int

	// lastRunTurns is the agentic turn count from the most recent Run (API rounds).
	lastRunTurns int

	// maxSessionOutputTokens, when > 0, caps cumulative OutputTokens (assistant
	// side) across the loop; input/context size is excluded. Exceeded → ErrTokenBudgetExceeded.
	maxSessionOutputTokens int

	// heartbeatTurn is the current turn (1-based) for HeartbeatEvent; updated each iteration.
	heartbeatTurn atomic.Uint32

	// autoCompaction when false skips ensureContextCompacted (API may still 400 on huge context).
	autoCompaction bool

	// lastTraceID stores the ID of the most recent trace created by this loop.
	lastTraceID string
}

// LastTraceID returns the TraceID of the most recent run, useful for attaching feedback.
func (l *Loop) LastTraceID() string {
	return l.lastTraceID
}

// NewLoop constructs a Loop.
//
//   - driver: inference backend (typically Anthropic via Gateway)
//   - mgr: conversation manager (one per loop — NOT shared)
//   - executor: tool runner (permissions, Warden, registry execute)
//   - reg: tool registry (shared; tools must be goroutine-safe)
//   - eventCh: channel the loop writes events to. Must have a buffer or a
//     dedicated draining goroutine to avoid blocking the loop.
func NewLoop(
	driver InferenceDriver,
	mgr *convo.Manager,
	executor ToolExecutor,
	reg *tools.Registry,
	eventCh chan<- Event,
) *Loop {
	return &Loop{
		driver:         driver,
		convo:          mgr,
		executor:       executor,
		registry:       reg,
		eventCh:        eventCh,
		autoCompaction: true,
	}
}

// SetAutoCompaction controls whether the loop runs budget compaction before each
// model turn. When false, only manual /compact (or a fresh session) shrinks context.
func (l *Loop) SetAutoCompaction(enabled bool) {
	l.autoCompaction = enabled
}

// SetDriver overrides the inference driver for this loop.
func (l *Loop) SetDriver(d InferenceDriver) {
	if d != nil {
		l.driver = d
	}
}

// SetClient overrides the API client by wrapping a new Anthropic driver.
func (l *Loop) SetClient(c *api.Client) {
	if c != nil {
		l.driver = NewAnthropicInferenceDriver(c)
	}
}

// ApplyWorkflowSettings applies optional loop behavior from merged settings.
func (l *Loop) ApplyWorkflowSettings(disableAutoCompaction bool) {
	if disableAutoCompaction {
		l.SetAutoCompaction(false)
	}
}

// Run processes a single user input, driving the agentic loop until the
// model produces a final text response with no tool calls.
//
// Run is synchronous — it returns after DoneEvent (or ErrorEvent) has been
// sent. The caller should not send another input until Run returns.
func (l *Loop) Run(ctx context.Context, input string) error {
	tracer := telemetry.TracerFrom(ctx)
	traceID := telemetry.TraceIDFrom(ctx)
	traceOwnedByLoop := traceID == ""
	if traceOwnedByLoop {
		traceID = tracer.StartTrace(telemetry.TraceParams{
			Name:      "agent-run",
			Input:     input,
			SessionID: telemetry.SessionIDFrom(ctx),
			Tags:      []string{"drover-code"},
		})
		ctx = telemetry.WithTraceID(ctx, traceID)
		hookLinearTrace(ctx, traceID)
	}
	l.lastTraceID = traceID

	var finalOutput string
	var runErr error
	defer func() {
		if !traceOwnedByLoop {
			return
		}
		meta := map[string]any{"ok": runErr == nil}
		if runErr != nil {
			meta["error"] = runErr.Error()
		}
		tracer.EndTrace(traceID, finalOutput, meta)
		_ = outcomesignal.WriteBYOCSpanIfConfigured(traceID, outcomesignal.FromRunError(runErr), input, finalOutput)
	}()

	l.convo.Append(api.UserMessage(input))

	// Input Guard (Warden) — run on the latest user message for prompt injection / policy violations
	lastUser := input
	idec := warden.CheckInput(ctx, &droverwarden.GuardRequest{
		TenantID: os.Getenv("DROVER_TENANT_ID"),
		Input:    lastUser,
		Context: map[string]any{
			"agent_id": os.Getenv("DROVER_AGENT_ID"),
		},
	})
	if !idec.Allowed {
		return fmt.Errorf("input blocked by Warden: %s", idec.Result.Reason)
	}

	var wg sync.WaitGroup
	stopHeartbeat := make(chan struct{})
	if d := heartbeatInterval(); d > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.runHeartbeat(d, stopHeartbeat)
		}()
	}
	defer func() {
		close(stopHeartbeat)
		wg.Wait()
	}()

	var turn int
	defer func() { l.lastRunTurns = turn }()

	for {
		turn++
		l.heartbeatTurn.Store(uint32(turn))
		if err := l.ensureContextCompacted(ctx); err != nil {
			runErr = err
			l.emit(ErrorEvent{Err: err})
			return err
		}

		estBefore := l.convo.EstimatedTokens()
		genID, blocks, usage, err := l.streamResponse(ctx, turn)
		if err != nil {
			runErr = err
			l.emit(ErrorEvent{Err: err})
			return err
		}
		if usage.InputTokens > 0 {
			l.convo.RecordAPICalibration(estBefore, usage.InputTokens)
		}

		// Track cumulative token usage and tell the consumer.
		l.totalInput += usage.InputTokens
		l.totalOutput += usage.OutputTokens
		if l.maxSessionOutputTokens > 0 && l.totalOutput > l.maxSessionOutputTokens {
			runErr = ErrTokenBudgetExceeded
			l.emit(ErrorEvent{Err: runErr})
			return runErr
		}
		l.emit(UsageEvent{
			Usage:             usage,
			TotalInputTokens:  l.totalInput,
			TotalOutputTokens: l.totalOutput,
		})

		// Record the assistant's response in conversation history.
		l.convo.Append(api.AssistantMessage(blocks))

		// Collect all tool calls from the response.
		var calls []api.ToolUseBlock
		for _, b := range blocks {
			if tc, ok := b.(api.ToolUseBlock); ok {
				calls = append(calls, tc)
			}
		}

		// Output Guard (Warden) after model response (post collection for accurate context).
		// Runs for every assistant turn; text-only final answers and interleaved tool-use turns.
		// Guards generated content before DoneEvent or tool execution.
		// (Action-level guards still run per-call inside execute path.)
		textOut := concatTextFromBlocks(blocks)
		odec := warden.CheckOutput(ctx, &droverwarden.GuardRequest{
			TenantID: os.Getenv("DROVER_TENANT_ID"),
			Output:   textOut,
			Context: map[string]any{
				"agent_id":  os.Getenv("DROVER_AGENT_ID"),
				"turn":      turn,
				"has_tools": len(calls) > 0,
			},
		})
		if !odec.Allowed {
			runErr = fmt.Errorf("output blocked by Warden: %s", odec.Result.Reason)
			l.emit(ErrorEvent{Err: runErr})
			return runErr
		}

		// No tool calls → the model is done; return to the user.
		if len(calls) == 0 {
			finalOutput = concatTextFromBlocks(blocks)
			l.emit(DoneEvent{})
			return nil
		}

		// Tool spans nest under this API round-trip's generation observation.
		ctxTools := telemetry.WithSpanID(ctx, genID)
		results, err := l.executeTools(ctxTools, calls)
		if err != nil {
			runErr = err
			l.emit(ErrorEvent{Err: err})
			return err
		}

		l.convo.Append(api.ToolResultMessage(results))
	}
}

// ----------------------------------------------------------------------------
// Streaming
// ----------------------------------------------------------------------------

// streamResponse opens a streaming API call, emits text delta events as
// tokens arrive, accumulates the full response, and returns the completed
// content blocks along with token usage.
func (l *Loop) streamResponse(ctx context.Context, turn int) (telemetry.SpanID, []api.ContentBlock, api.Usage, error) {
	if l.driver == nil {
		return "", nil, api.Usage{}, fmt.Errorf("inference driver not configured")
	}
	_ = turn

	ch, genID, err := l.driver.Generate(ctx, l.convo, l.registry.Definitions())
	if err != nil {
		return "", nil, api.Usage{}, err
	}

	var blocks []api.ContentBlock
	var usage api.Usage

	for ev := range ch {
		switch e := ev.(type) {
		case TextDeltaYielded:
			l.emit(TextDeltaEvent{Text: e.Text})
		case TextYielded:
			blocks = append(blocks, api.TextBlock{Text: e.Text})
		case ToolCallRequested:
			blocks = append(blocks, api.ToolUseBlock{
				ID:    e.ID,
				Name:  e.Name,
				Input: e.Input,
			})
		case TurnCompleted:
			usage = e.Usage
		case InferenceError:
			return genID, nil, api.Usage{}, e.Err
		}
	}

	return genID, blocks, usage, nil
}

// CompactContext runs one summarisation round: replaces all but the last
// compactionKeepTail messages with a summary. Used by /compact regardless of
// the estimated token budget.
func (l *Loop) CompactContext(ctx context.Context) error {
	msgs := l.convo.Messages()
	if _, err := convo.CompactionCutPoint(msgs, compactionKeepTail); err != nil {
		return err
	}
	before := l.convo.EstimatedTokens()
	l.emit(CompactionStartEvent{Round: 1, MaxRounds: 1, EstimatedTokensBefore: before})
	compactionDebugf("manual compact start est=%d msgs=%d", before, len(msgs))
	t0 := time.Now()
	err := l.runCompactionRound(ctx, msgs)
	after := l.convo.EstimatedTokens()
	d := time.Since(t0)
	l.emit(CompactionDoneEvent{Round: 1, EstimatedTokensAfter: after, Duration: d, Err: err})
	if err != nil {
		compactionDebugf("manual compact failed after %v: %v", d, err)
		return err
	}
	compactionDebugf("manual compact done in %v est=%d->%d", d, before, after)
	return nil
}

func (l *Loop) ensureContextCompacted(ctx context.Context) error {
	if !l.autoCompaction {
		return nil
	}
	for round := 0; round < maxCompactionRounds; round++ {
		if !l.convo.NeedsCompaction() {
			return nil
		}
		msgs := l.convo.Messages()
		if len(msgs) <= compactionKeepTail {
			if l.convo.NeedsCompaction() {
				return fmt.Errorf("context over limit but not enough turns to compact; use /clear")
			}
			return nil
		}
		r := round + 1
		before := l.convo.EstimatedTokens()
		l.emit(CompactionStartEvent{Round: r, MaxRounds: maxCompactionRounds, EstimatedTokensBefore: before})
		compactionDebugf("auto round %d/%d start est=%d msgs=%d", r, maxCompactionRounds, before, len(msgs))
		t0 := time.Now()
		if err := l.runCompactionRound(ctx, msgs); err != nil {
			after := l.convo.EstimatedTokens()
			l.emit(CompactionDoneEvent{Round: r, EstimatedTokensAfter: after, Duration: time.Since(t0), Err: err})
			compactionDebugf("auto round %d failed after %v: %v", r, time.Since(t0), err)
			return err
		}
		after := l.convo.EstimatedTokens()
		d := time.Since(t0)
		l.emit(CompactionDoneEvent{Round: r, EstimatedTokensAfter: after, Duration: d, Err: nil})
		compactionDebugf("auto round %d/%d done in %v est=%d->%d", r, maxCompactionRounds, d, before, after)
	}
	if l.convo.NeedsCompaction() {
		return fmt.Errorf("context still over limit after %d compaction rounds; use /clear or start a shorter session", maxCompactionRounds)
	}
	return nil
}

func compactionDebugf(format string, args ...interface{}) {
	if os.Getenv("DROVER_CODE_DEBUG_COMPACTION") != "1" {
		return
	}
	log.Printf("[drover-code compaction] "+format, args...)
}

func (l *Loop) runCompactionRound(ctx context.Context, msgs []api.Message) error {
	cut, err := convo.CompactionCutPoint(msgs, compactionKeepTail)
	if err != nil {
		return err
	}
	head := msgs[:cut]
	tailCount := len(msgs) - cut
	body := serializeMessagesForCompaction(head)
	if utf8.RuneCountInString(body) > maxCompactionBodyRunes {
		runes := []rune(body)
		body = "[... earlier turns truncated for summarisation ...]\n\n" + string(runes[len(runes)-maxCompactionBodyRunes:])
	}

	prompt := "Summarize the following earlier conversation. Do not use tools.\n\n" + body
	req := api.StreamRequest{
		System:    compactionSystemPrompt,
		Messages:  []api.Message{api.UserMessage(prompt)},
		Tools:     nil,
		MaxTokens: 4096,
	}

	summary, _, err := l.collectStreamText(ctx, req)
	if err != nil {
		return fmt.Errorf("compaction: %w", err)
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return fmt.Errorf("compaction: empty summary from model")
	}

	l.convo.Summarise(summary, tailCount)
	return nil
}

func (l *Loop) collectStreamText(ctx context.Context, req api.StreamRequest) (string, api.Usage, error) {
	if l.driver == nil {
		return "", api.Usage{}, fmt.Errorf("inference driver not configured")
	}
	tmp := convo.NewManagerWithSystem(req.System)
	for _, msg := range req.Messages {
		tmp.Append(msg)
	}

	ch, _, err := l.driver.Generate(ctx, tmp, nil)
	if err != nil {
		return "", api.Usage{}, err
	}

	var out strings.Builder
	var usage api.Usage
	for ev := range ch {
		switch e := ev.(type) {
		case TextYielded:
			out.WriteString(e.Text)
		case TurnCompleted:
			usage = e.Usage
		case InferenceError:
			return "", api.Usage{}, e.Err
		}
	}
	return strings.TrimSpace(out.String()), usage, nil
}

func serializeMessagesForCompaction(msgs []api.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		role := "user"
		if m.Role == api.RoleAssistant {
			role = "assistant"
		}
		fmt.Fprintf(&b, "[%s]\n", role)
		for _, block := range m.Content {
			switch bb := block.(type) {
			case api.TextBlock:
				b.WriteString(bb.Text)
				b.WriteByte('\n')
			case api.ToolUseBlock:
				fmt.Fprintf(&b, "[tool_use %s]\n", bb.Name)
			case api.ToolResultBlock:
				content := bb.Content
				if utf8.RuneCountInString(content) > maxCompactionToolResult {
					r := []rune(content)
					content = string(r[:maxCompactionToolResult-1]) + "…"
				}
				flag := ""
				if bb.IsError {
					flag = " (error)"
				}
				fmt.Fprintf(&b, "[tool_result%s]\n%s\n", flag, content)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// ----------------------------------------------------------------------------
// Tool execution
// ----------------------------------------------------------------------------

// executeTools runs all tool calls from a single assistant response.
func (l *Loop) executeTools(ctx context.Context, calls []api.ToolUseBlock) ([]api.ToolResultBlock, error) {
	if l.executor == nil {
		return nil, fmt.Errorf("tool executor not configured")
	}
	return l.executor.ExecuteTools(ctx, calls)
}

func hookLinearTrace(ctx context.Context, traceID telemetry.TraceID) {
	issue := telemetry.LinearIssueFrom(ctx)
	client := telemetry.LinearClientFrom(ctx)
	if issue == "" || client == nil || traceID == "" {
		return
	}
	internalID, _, err := client.GetIssueDetails(ctx, issue)
	if err != nil {
		return
	}
	_ = client.LinkTrace(ctx, internalID, string(traceID))
}

// heartbeatInterval returns 0 when heartbeats are disabled.
func heartbeatInterval() time.Duration {
	s := strings.TrimSpace(os.Getenv("DROVER_CODE_HEARTBEAT_INTERVAL_SECS"))
	if s == "" {
		return 30 * time.Second
	}
	sec, err := strconv.Atoi(s)
	if err != nil || sec < 0 {
		return 30 * time.Second
	}
	if sec == 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}

func (l *Loop) runHeartbeat(interval time.Duration, stop <-chan struct{}) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			turn := int(l.heartbeatTurn.Load())
			l.emit(HeartbeatEvent{Turn: turn, Time: time.Now().UTC()})
		}
	}
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

// emit sends an event to the consumer. Non-blocking: if the channel is full
// the event is dropped rather than stalling the agent loop.
func (l *Loop) emit(e Event) {
	select {
	case l.eventCh <- e:
	default:
	}
}

// summariseInput produces a short human-readable description of a tool call's
// input — used in ToolStartEvent.InputSummary and permission prompts.
func summariseInput(name string, input json.RawMessage) string {
	// Try to extract a "command", "path", or "query" field as the summary.
	var m map[string]json.RawMessage
	if err := json.Unmarshal(input, &m); err != nil {
		return name
	}

	for _, key := range []string{"command", "path", "query", "pattern", "url"} {
		if v, ok := m[key]; ok {
			var s string
			if err := json.Unmarshal(v, &s); err == nil {
				return truncate(fmt.Sprintf("%s: %s", name, s), 100)
			}
		}
	}

	return truncate(fmt.Sprintf("%s %s", name, string(input)), 100)
}

func concatTextFromBlocks(blocks []api.ContentBlock) string {
	var b strings.Builder
	for _, block := range blocks {
		if tb, ok := block.(api.TextBlock); ok {
			b.WriteString(tb.Text)
		}
	}
	return b.String()
}

// truncate clips s to at most n runes, appending "…" if truncated.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}

// LastRunTurns returns the number of agentic (API) turns in the last Run call.
func (l *Loop) LastRunTurns() int {
	return l.lastRunTurns
}

// SessionInputTokens returns cumulative input tokens for this loop instance.
func (l *Loop) SessionInputTokens() int {
	return l.totalInput
}

// SessionOutputTokens returns cumulative output tokens for this loop instance.
func (l *Loop) SessionOutputTokens() int {
	return l.totalOutput
}

// SetMaxSessionTokens sets a cap on cumulative assistant output tokens
// (API OutputTokens) for this loop across Run calls. Input/context tokens are
// not counted. Zero disables. Used with DROVER_CODE_MAX_TOKENS.
func (l *Loop) SetMaxSessionTokens(n int) {
	if n < 0 {
		n = 0
	}
	l.maxSessionOutputTokens = n
}
