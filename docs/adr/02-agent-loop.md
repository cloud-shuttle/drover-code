# 02 — Agent Loop: Conversation Manager, Agent Loop, and Event System

**Packages:** `internal/convo`, `internal/agent`  
**Files:** `convo/manager.go`, `agent/events.go`, `agent/loop.go`  
**Depends on:** `internal/api`, `internal/tools`  
**Depended on by:** `internal/tui`, `internal/coordinator`, `cmd/drover-code`

---

## Purpose

These two packages together form the engine of drover-code. The conversation
manager owns the state that persists across tool calls within a session. The
agent loop drives the plan → act → observe cycle that turns a single user
message into a complete response, including however many tool calls that
requires. The event system is the interface through which the loop communicates
with its consumer — the TUI, the headless printer, or the coordinator.

The three are deliberately separated:

- `convo` has no knowledge of the API or tools — it is pure state management
- `agent` orchestrates between the API client, the conversation, and the tools
- Events are typed messages, not callbacks — the loop never calls into the TUI

---

## 1. Conversation Manager (`convo/manager.go`)

### 1.1 Responsibilities

The manager owns exactly three things:

1. The message history — a `[]api.Message` representing the entire conversation
2. The system prompt — a `string` that is sent with every API request
3. Token budget tracking — an estimate of how much context has been used

Everything else (what model to use, which tools are available, how to render
output) belongs to other packages.

### 1.2 Thread safety

```go
type Manager struct {
    mu           sync.RWMutex
    messages     []api.Message
    systemPrompt string
    contextLimit int
}
```

The `sync.RWMutex` separates read and write access. Multiple goroutines can
read the message history concurrently (coordinator mode runs multiple worker
agents that all hold a reference to the parent context snapshot), but appending
a new message requires exclusive access.

The key insight is that `Messages()` returns a **snapshot copy**, not a
reference to the internal slice:

```go
func (m *Manager) Messages() []api.Message {
    m.mu.RLock()
    defer m.mu.RUnlock()
    snap := make([]api.Message, len(m.messages))
    copy(snap, m.messages)
    return snap
}
```

This means the slice the caller passes to `client.StreamMessage()` is
independent of the manager's internal slice. If a concurrent `Append()` call
adds a message while the API request is in flight, the in-flight request is
unaffected. The next request will pick up the new message.

This is important for coordinator mode: the coordinator takes a snapshot of
the current conversation to seed each worker, then workers operate independently
without racing on the shared history.

### 1.3 Why not a channel-based append queue?

An alternative design would have callers send messages to a channel and a
single goroutine own the slice. This would eliminate the mutex and make the
memory model simpler.

We rejected this because:

1. **The agent loop is already sequential within a turn.** `Run()` alternates
   between streaming a response and executing tools. There is never a situation
   where two messages from the same loop are appended concurrently.
2. **A mutex is cheaper than a goroutine + channel.** For a path that runs once
   per tool call, the extra goroutine + channel allocation adds latency without
   benefit.
3. **Coordinator workers need their own managers.** Each worker gets a fresh
   `convo.Manager` seeded from a snapshot. There is no shared append queue to
   synchronise on.

### 1.4 Token estimation

```go
// Default runes-per-token divisor is 4; override via settings / env (design/14 B2).

func (m *Manager) EstimatedTokens() int {
    m.mu.RLock()
    defer m.mu.RUnlock()
    div := 4 // or configured charsPerToken (1..32)
    total := utf8.RuneCountInString(m.systemPrompt)
    for _, msg := range m.messages {
        total += contentChars(msg.Content)
    }
    return total / div
}
```

We count Unicode rune characters (not bytes) because the tokenizer works on
Unicode codepoints. The default **runes÷4** rule is a cheap stand-in for
Claude-style tokenization — it will drift from real `usage.input_tokens` on
JSON-heavy tool turns, CJK, or unusual symbols.

**Tuning:** `charsPerTokenEstimate` / `DROVER_CODE_CHARS_PER_TOKEN` (see
[14 — UX / memory](./14-ux-memory-improvements.md) §B2). Smaller divisor ⇒ larger
estimated token count ⇒ **earlier** compaction (safer if the API counts higher
than the heuristic).

**Calibration (B3):** After each main `StreamMessage`, the loop records the ratio
`usage.input_tokens / EstimatedTokens()` (pre-request) in `convo.Manager` as an
EMA. `/tokens` in the TUI surfaces this so operators can align divisor and
`contextLimitEstimate` with live API behavior without bundling a tokenizer.

A future phase may swap `EstimatedTokens()` for `tiktoken-go` or similar; the
manager API and compaction gate stay the same.

**Why not count tokens on every append?** Counting is O(n) in the character
count of the new content. For a single tool result that's cheap. But after
compaction, the manager's history is short again, and incremental counting
would need to track a running total carefully. Scanning the full history on
each `NeedsCompaction()` check is simpler and accurate enough since compaction
checks happen at most once per agent turn.

### 1.5 Compaction

```go
func (m *Manager) Summarise(summary string, keepTail int) {
    m.mu.Lock()
    defer m.mu.Unlock()
    if len(m.messages) <= keepTail {
        return
    }
    cutPoint := len(m.messages) - keepTail
    tail := make([]api.Message, keepTail)
    copy(tail, m.messages[cutPoint:])
    summaryMsg := api.UserMessage(summarySentinel(summary))
    m.messages = append([]api.Message{summaryMsg}, tail...)
}

func summarySentinel(summary string) string {
    return "[Earlier conversation summary — treat as background context]\n\n" + summary
}
```

The sentinel phrase is important. Without it, the model might treat the summary
as a direct user request ("fix the bug", "explain the code") and try to respond
to it rather than using it as context. The framing phrase tells the model it is
reading condensed history.

`keepTail` is typically 6–10 messages (3–5 turns). This preserves the
immediate conversational context — the last thing the user said, the last thing
the model did, the last tool result — which is where the most relevant state
for the current task lives.

The `make` + `copy` for `tail` is deliberate: we must not hold a reference to
the old `m.messages` backing array after compaction, otherwise it can't be
garbage collected. The `append([]api.Message{...}, tail...)` creates a new
backing array.

### 1.6 System prompt lifecycle

The system prompt is set once at session start and rebuilt if CLAUDE.md files
change or if undercover mode is activated mid-session. The `SetSystemPrompt`
method is safe to call from any goroutine and takes effect on the next API
call — it is not reflected in in-flight requests.

```go
func (m *Manager) SetSystemPrompt(prompt string) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.systemPrompt = prompt
}
```

The system prompt is sent with **every** API request — it is not part of the
message history and does not consume message slots. It does consume tokens, so
long CLAUDE.md files and memory injections do reduce the effective context
window available for conversation.

---

## 2. Agent Events (`agent/events.go`)

### 2.1 Design philosophy

The agent loop communicates with its consumer exclusively through a typed event
channel. It never calls into the TUI, never writes to stdout, never makes
assumptions about how its output will be displayed. This separation has several
benefits:

**Testability.** You can test the agent loop by providing a mock tool registry
and reading events off the channel. No terminal, no BubbleTea, no mocking of
display code.

**Multiple consumers.** The same loop works whether the consumer is the
BubbleTea TUI, the headless CLI printer, the IDE bridge, or a test harness.
Different consumers handle the same events differently.

**Backpressure without blocking.** The `emit()` method is non-blocking — it
drops events when the channel is full rather than stalling the loop. The loop's
job is to make progress on the user's task. A slow consumer (TUI rendering)
should not slow down tool execution.

### 2.2 Event types

```go
type Event interface{ isAgentEvent() }
```

| Event | When | Key fields |
|---|---|---|
| `TextDeltaEvent` | Each streaming token | `Text string` |
| `ToolStartEvent` | After permission granted, before Execute | `CallIndex, ID, Name, InputSummary` |
| `ToolDoneEvent` | After Execute returns | `CallIndex, ID, Name, IsError, OutputSummary` |
| `PermissionRequestEvent` | Tool needs approval | `ToolName, Input, Summary, DecisionCh` |
| `UsageEvent` | After each API response | `Usage, TotalInput, TotalOutput` |
| `DoneEvent` | Loop complete, no more tool calls | — |
| `ErrorEvent` | Fatal error in loop | `Err error` |

### 2.3 The permission channel protocol

`PermissionRequestEvent` is the most complex event because it requires a
round-trip: the loop sends the event, then **blocks** waiting for a response.

```go
type PermissionRequestEvent struct {
    ToolName   string
    Input      []byte
    Summary    string
    DecisionCh chan<- PermissionDecision  // send-once
}
```

The `DecisionCh` is a buffered channel of size 1 created by the loop before
emitting the event. The consumer (TUI permission overlay, or headless auto-approver)
sends exactly one decision on this channel.

The loop's `executeSingleTool` method blocks on receiving from this channel:

```go
// In the agent loop (simplified):
respCh := make(chan PermissionDecision, 1)
l.emit(PermissionRequestEvent{..., DecisionCh: respCh})

select {
case d := <-respCh:
    if d == PermDeny { /* return denied result */ }
    // proceed with execution
case <-ctx.Done():
    return /* cancelled */
}
```

The `select` on `ctx.Done()` is critical. If the user quits while a permission
prompt is displayed, the context is cancelled and the loop exits cleanly rather
than hanging forever waiting for a decision that will never arrive.

In the TUI, `PermissionRequestEvent` triggers the permission overlay. The
overlay captures keypresses and sends to `DecisionCh` on `y`, `a`, or `n`.
The overlay must handle the case where the user quits (`tea.Quit`) while the
prompt is shown — in that case the TUI's `Program` cancels the context, which
unblocks the `select` above.

### 2.4 CallIndex

`ToolStartEvent` and `ToolDoneEvent` carry a `CallIndex` field — the position
of this tool call within the current assistant response (0-based). This allows
the TUI to maintain one spinner per active tool call and match done events to
their corresponding start events.

In coordinator mode, worker events are relabelled: `workerIdx*100 + callIndex`.
This ensures indices are unique across all workers without requiring coordination
between them. The TUI treats coordinator worker spinners identically to direct
tool spinners.

### 2.5 InputSummary and OutputSummary

These are short human-readable descriptions generated by the loop, not the full
inputs/outputs. They are used in spinner labels and tool status rows in the TUI.

Generation logic for `InputSummary`:

```go
func summariseInput(name string, input json.RawMessage) string {
    var m map[string]json.RawMessage
    json.Unmarshal(input, &m)
    // Try keys in preference order: command, path, query, pattern, url
    for _, key := range []string{"command", "path", "query", "pattern", "url"} {
        if v, ok := m[key]; ok {
            var s string
            json.Unmarshal(v, &s)
            if s != "" {
                return truncate(fmt.Sprintf("%s: %s", name, s), 100)
            }
        }
    }
    return truncate(fmt.Sprintf("%s %s", name, string(input)), 100)
}
```

This produces readable summaries like `bash: go test ./...` or
`read_file: internal/api/stream.go` without exposing full JSON payloads in
the UI. The 100-character truncation prevents long bash commands from
overflowing the status bar.

---

## 3. Agent Loop (`agent/loop.go`)

### 3.1 The core loop

```go
func (l *Loop) Run(ctx context.Context, input string) error {
    l.convo.Append(api.UserMessage(input))

    for {
        // Stream the API response, collect all content blocks
        blocks, usage, err := l.streamResponse(ctx)
        if err != nil {
            l.emit(ErrorEvent{Err: err})
            return err
        }

        // Update cumulative token counts
        l.totalInput += usage.InputTokens
        l.totalOutput += usage.OutputTokens
        l.emit(UsageEvent{Usage: usage,
            TotalInputTokens:  l.totalInput,
            TotalOutputTokens: l.totalOutput,
        })

        // Commit the assistant's response to conversation history
        l.convo.Append(api.AssistantMessage(blocks))

        // Extract tool calls
        var calls []api.ToolUseBlock
        for _, b := range blocks {
            if tc, ok := b.(api.ToolUseBlock); ok {
                calls = append(calls, tc)
            }
        }

        // No tool calls → the model is finished
        if len(calls) == 0 {
            l.emit(DoneEvent{})
            return nil
        }

        // Execute all tool calls, feed results back
        results, err := l.executeTools(ctx, calls)
        if err != nil {
            l.emit(ErrorEvent{Err: err})
            return err
        }
        l.convo.Append(api.ToolResultMessage(results))
    }
}
```

The loop is intentionally simple. All complexity lives in `streamResponse`
(SSE accumulation) and `executeTools` (parallel execution + permission).

### 3.2 Why the loop is synchronous

`Run()` blocks until the entire user request is complete. The caller (TUI)
runs it via `tea.Cmd`, which executes it off BubbleTea's main goroutine. This
means:

- The BubbleTea UI remains responsive during the entire loop execution
- Events arrive on the channel and are processed by `Update()` as they come
- The user can cancel via Ctrl+C at any point

An alternative design would have `Run()` accept a `chan<- Event` and return
immediately, pushing all work to a background goroutine. We rejected this
because it makes error handling more complex (the caller needs to watch for
`ErrorEvent` to know the loop failed) and makes cancellation harder to reason
about. The synchronous model with `tea.Cmd` gives us all the benefits of
non-blocking UI with simpler flow control.

### 3.3 streamResponse in detail

```go
func (l *Loop) streamResponse(ctx context.Context) ([]api.ContentBlock, api.Usage, error) {
    stream, err := l.client.StreamMessage(ctx, api.StreamRequest{
        System:   l.convo.SystemPrompt(),
        Messages: l.convo.Messages(),  // snapshot
        Tools:    l.registry.Definitions(),
    })
    if err != nil {
        return nil, api.Usage{}, err
    }
    defer stream.Close()

    textAccs  := map[int]*textAcc{}   // index → accumulating text
    toolAccs  := map[int]*toolAcc{}   // index → accumulating JSON
    finalised := map[int]api.ContentBlock{}
    var usage  api.Usage

    for stream.Next() {
        switch e := stream.Event().(type) {

        case api.ContentBlockStartEvent:
            switch b := e.ContentBlock.(type) {
            case api.TextBlock:
                textAccs[e.Index] = &textAcc{}
            case api.ToolUseBlock:
                toolAccs[e.Index] = &toolAcc{id: b.ID, name: b.Name}
            }

        case api.ContentBlockDeltaEvent:
            switch d := e.Delta.(type) {
            case api.TextDelta:
                textAccs[e.Index].buf.WriteString(d.Text)
                l.emit(TextDeltaEvent{Text: d.Text})  // stream to consumer immediately
            case api.InputJSONDelta:
                toolAccs[e.Index].jsonBuf.WriteString(d.PartialJSON)
            }

        case api.ContentBlockStopEvent:
            if acc, ok := textAccs[e.Index]; ok {
                finalised[e.Index] = api.TextBlock{Text: acc.buf.String()}
                delete(textAccs, e.Index)
            }
            if acc, ok := toolAccs[e.Index]; ok {
                raw := acc.jsonBuf.String()
                if raw == "" { raw = "{}" }
                finalised[e.Index] = api.ToolUseBlock{
                    ID: acc.id, Name: acc.name,
                    Input: json.RawMessage(raw),
                }
                delete(toolAccs, e.Index)
            }

        case api.MessageDeltaEvent:
            usage.InputTokens  = e.InputTokens
            usage.OutputTokens = e.OutputTokens
        }
    }

    if err := stream.Err(); err != nil {
        return nil, api.Usage{}, err
    }

    return reconstructBlocks(finalised), usage, nil
}
```

The `delete(textAccs, e.Index)` and `delete(toolAccs, e.Index)` after
finalisation are important for memory management. Long responses with many tool
calls would otherwise accumulate large string builders in memory indefinitely.

**Reconstructing block order:** The `finalised` map is keyed by index. We
reconstruct the ordered slice by iterating over the index space:

```go
func reconstructBlocks(m map[int]api.ContentBlock) []api.ContentBlock {
    if len(m) == 0 { return nil }
    maxIdx := 0
    for idx := range m { if idx > maxIdx { maxIdx = idx } }
    out := make([]api.ContentBlock, maxIdx+1)
    for idx, b := range m { out[idx] = b }
    // Filter nils (gaps from non-contiguous indices)
    result := out[:0]
    for _, b := range out {
        if b != nil { result = append(result, b) }
    }
    return result
}
```

In practice indices are always contiguous starting at 0, but handling gaps
makes the code correct for any future API behaviour.

### 3.4 Parallel tool execution

```go
func (l *Loop) executeTools(ctx context.Context, calls []api.ToolUseBlock) ([]api.ToolResultBlock, error) {
    results := make([]api.ToolResultBlock, len(calls))
    g, gctx := errgroup.WithContext(ctx)

    for i, call := range calls {
        i, call := i, call  // capture loop variables

        g.Go(func() error {
            result, err := l.executeSingleTool(gctx, i, call)
            if err != nil {
                return err  // fatal: stops all workers via gctx cancellation
            }
            results[i] = result
            return nil
        })
    }

    if err := g.Wait(); err != nil {
        return nil, err
    }
    return results, nil
}
```

**Why parallel?** When the model issues multiple tool calls in one response
(e.g. "read these three files"), they are independent and can run
simultaneously. Sequential execution would take 3× as long for a common
pattern. On a codebase exploration task where the model reads 10 files in a
single turn, parallel execution reduces that round-trip from ~10 sequential
disk reads to one.

**errgroup semantics:** `errgroup.WithContext` creates a derived context
`gctx`. If any goroutine returns a non-nil error, `gctx` is cancelled, which
signals all other goroutines to stop. `g.Wait()` then returns the first error.

**Note on "fatal" errors:** A tool returning an error does *not* stop the
other tool goroutines — the error is captured in `WorkerResult` and returned
as a `tool_result` with `is_error: true`. The only errors that propagate through
`errgroup` are programming errors (panics recovered as errors) or context
cancellation. Tool execution errors are handled in `executeSingleTool`.

```go
func (l *Loop) executeSingleTool(ctx context.Context, idx int, call api.ToolUseBlock) (api.ToolResultBlock, error) {
    // 1. Permission check
    if l.registry.NeedsPermission(call.Name, call.Input) {
        decision := l.permitFn(ctx, tools.PermissionRequest{
            ToolName: call.Name,
            Input:    call.Input,
            Summary:  summariseInput(call.Name, call.Input),
        })
        if decision == tools.Deny {
            l.emit(ToolDoneEvent{..., IsError: true, OutputSummary: "denied"})
            return api.ToolResultBlock{
                ToolUseID: call.ID,
                Content:   "Tool execution denied by user.",
                IsError:   true,
            }, nil  // not a fatal error — model can continue
        }
    }

    // 2. Notify consumer
    l.emit(ToolStartEvent{
        CallIndex:    idx,
        ID:           call.ID,
        Name:         call.Name,
        InputSummary: summariseInput(call.Name, call.Input),
    })

    // 3. Execute
    output, execErr := l.registry.Execute(ctx, call.Name, call.Input)

    // 4. Notify consumer
    if execErr != nil {
        l.emit(ToolDoneEvent{..., IsError: true, OutputSummary: truncate(execErr.Error(), 80)})
        return api.ToolResultBlock{
            ToolUseID: call.ID,
            Content:   execErr.Error(),
            IsError:   true,
        }, nil  // not fatal — return error as tool result, model decides what to do
    }

    l.emit(ToolDoneEvent{..., IsError: false, OutputSummary: truncate(output, 80)})
    return api.ToolResultBlock{
        ToolUseID: call.ID,
        Content:   output,
        IsError:   false,
    }, nil
}
```

**Why tool errors are not fatal:** The model is better at deciding what to do
with a tool error than the loop is. If `read_file` fails because the file
doesn't exist, the model might try `glob` to search for it, or tell the user
it couldn't find it. If we propagated the error as a fatal loop failure, we'd
lose that reasoning. Returning `is_error: true` tool results is the correct
protocol-level behaviour — the API documents it for exactly this reason.

### 3.5 Permission ordering in parallel execution

There is a subtle issue with permission prompts during parallel tool execution.
If the model issues two tool calls simultaneously and both need permission, the
loop emits two `PermissionRequestEvent`s in quick succession. The TUI receives
them on the event channel and shows one at a time.

The loop handles this correctly: the two goroutines in `executeTools` both call
`permitFn` and both block waiting for their respective `DecisionCh`. The TUI
processes them sequentially — one permission prompt at a time. The user approves
or denies each one, and each goroutine unblocks independently.

The execution order of the tools after permission is granted is non-deterministic
(goroutine scheduler). This is fine — the results slice is indexed by `i` so
results land in the correct position regardless of execution order.

### 3.6 Non-blocking emit

```go
func (l *Loop) emit(e Event) {
    select {
    case l.eventCh <- e:
    default:
        // Channel full — drop event
    }
}
```

The event channel is buffered (typically 256 or 512 events). In normal
operation it never fills — the consumer drains it faster than the loop
produces events. The `default` case is a safety valve for pathological
situations (e.g. TUI is frozen, hundreds of tool calls complete simultaneously).

**What gets dropped?** `TextDeltaEvent` and `ToolDoneEvent` are the most
frequently emitted. Dropping a text delta means a few characters don't appear
in the live streaming view but appear in the final committed response (since
we re-render the full response from the accumulated buffer on `DoneEvent`).
Dropping a `ToolDoneEvent` means a spinner stays active briefly longer than it
should. Neither is catastrophic.

**What must never be dropped?** `PermissionRequestEvent` and `DoneEvent`. For
`PermissionRequestEvent`, if the event is dropped, the loop blocks forever
on `respCh` — a deadlock. We prevent this by making the permission event path
blocking: `executeSingleTool` waits for the decision before continuing, and
the `select` in `emit` would only drop it if the channel is full. The channel
should never be full at that point because permission prompts are rare and the
consumer always processes them promptly. For production hardening, consider
making permission event emission blocking (remove the `default` case
specifically for `PermissionRequestEvent`).

### 3.7 Loop construction and dependency injection

```go
type Loop struct {
    client   *api.Client
    convo    *convo.Manager
    registry *tools.Registry
    permitFn tools.PermissionFunc
    eventCh  chan<- agent.Event
    totalInput, totalOutput int
}

func NewLoop(
    client   *api.Client,
    mgr      *convo.Manager,
    registry *tools.Registry,
    permitFn tools.PermissionFunc,
    eventCh  chan<- agent.Event,
) *Loop
```

Every dependency is injected. The loop has no global state and no singletons.
This makes it straightforward to:

- Swap `tools.AllowAll` for a real permission function (TUI mode vs headless)
- Provide a mock registry in tests
- Create multiple independent loop instances (coordinator workers)
- Test permission handling by injecting a `permitFn` that always denies

### 3.8 Conversation state after a run

After `Run()` returns (successfully), the conversation manager contains:

```
[initial user message]
[assistant response, turn 1]
[tool results, turn 1]     ← batched into one user message
[assistant response, turn 2]
[tool results, turn 2]
...
[final assistant response]  ← the one with no tool calls
```

The caller (TUI or headless) does not need to do anything with this — the
next call to `Run()` with the next user message will automatically continue
the conversation because the manager retains all the history.

---

## 4. Testing Strategy

### Unit tests for `convo/manager.go`

- Verify `Append` + `Messages` round-trip correctly
- Verify `Messages()` returns a copy (mutating the returned slice does not
  affect the manager's internal state)
- Verify concurrent `Append` + `Messages` calls don't race (run with `-race`)
- Verify `NeedsCompaction()` returns true when estimated tokens exceed limit
- Verify `Summarise()` correctly replaces old messages and preserves the tail
- Verify the sentinel phrase appears in the summary message body
- Verify compaction with `keepTail > len(messages)` is a no-op

### Unit tests for `agent/loop.go`

**Happy path:**
```go
// No tools: single user message → single text response → DoneEvent
registry := tools.NewRegistry()  // empty
loop := agent.NewLoop(fakeClient, mgr, registry, tools.AllowAll, eventCh)
err := loop.Run(ctx, "hello")
// expect: TextDeltaEvent(s), DoneEvent, no errors
```

**Tool call:**
```go
// Register a tool that returns "file contents"
// Feed it a fake stream that emits one ToolUseBlock + one text block
// Verify: ToolStartEvent, ToolDoneEvent, TextDeltaEvent, DoneEvent
// Verify: convo.Messages() contains tool_use + tool_result turns
```

**Tool error:**
```go
// Register a tool that returns an error
// Verify: ToolDoneEvent with IsError=true
// Verify: tool_result in conversation has is_error=true
// Verify: loop continues (does not return error)
// Verify: model's follow-up response is consumed correctly
```

**Permission denied:**
```go
// Register a tool with NeedsPermission=true
// Inject permitFn that always returns Deny
// Verify: PermissionRequestEvent emitted
// Verify: tool_result with is_error=true and "denied" content
// Verify: loop continues
```

**Cancellation:**
```go
// Cancel context while stream is reading
// Verify: loop returns context.Canceled
// Verify: ErrorEvent emitted
// Verify: no goroutine leak (use goleak)
```

**Parallel tool execution:**
```go
// Feed a stream with two simultaneous tool calls
// Register two tools that record their execution time
// Verify: tools executed concurrently (completion times overlap)
// Verify: results in correct index positions
```

### Race condition tests
All tests should run with `-race`. The parallel tool execution test is
particularly important — it directly tests concurrent access to the results
slice.

---

## 5. Performance Characteristics

| Operation | Complexity | Notes |
|---|---|---|
| `convo.Append()` | O(1) amortised | Slice append |
| `convo.Messages()` | O(n) | Full copy of message slice |
| `convo.EstimatedTokens()` | O(total chars) | Scans all messages |
| `stream.Next()` | O(line length) | Scanner read + JSON unmarshal |
| `executeTools()` parallel | O(max(tool latency)) | Bounded by slowest tool |
| `executeTools()` sequential | O(sum(tool latency)) | Worst case if errgroup serialises |

`convo.Messages()` is O(n) in the number of messages. For a typical session
(50–200 messages before compaction), this is fast. For the coordinator case
where `Messages()` is called once per worker at startup, it's called 2–4 times
total — not a bottleneck.

The token estimation scan is O(total chars) which grows with session length.
Running it only on `NeedsCompaction()` calls (once per agent turn) keeps the
cumulative cost manageable. After compaction, total chars drops back to near
zero.

---

## 6. Edge Cases and Failure Modes

### The model produces a response with only tool calls and no text
This is valid and common — the model might silently call a tool without
explaining itself. `streamResponse` handles this correctly: `textAccs` is
empty and `finalised` contains only `ToolUseBlock` entries. The blocks slice
returned contains no `TextBlock`s, and no `TextDeltaEvent`s are emitted. The
TUI shows only tool spinners, no text.

### The model produces an empty response
Shouldn't happen in practice, but if `blocks` is empty and no tool calls,
the loop emits `DoneEvent` and returns. The conversation manager has a
`AssistantMessage([]ContentBlock{})` appended — an empty assistant turn. This
is valid but unusual. The next user message starts a fresh exchange.

### Infinite tool loop
If the model repeatedly calls tools without ever producing a final text
response, `Run()` loops indefinitely. Mitigation options (not yet implemented):

1. **Turn limit:** Return an error after N tool-calling turns (e.g. 25, matching
   Claude Code's default)
2. **Token budget:** Return an error when `convo.EstimatedTokens()` exceeds
   a hard ceiling
3. **Context cancellation:** The user can always press Ctrl+C

For Phase 1–3, the context cancellation path is sufficient. Turn limits should
be added in Phase 4.

### Tool result too large for context
A tool might return 200,000 characters (the `toolutil.Truncate` cap). If the
conversation already has 150,000 tokens of context, appending this result
pushes over the 200k limit and the next API call returns a 400 error.

Mitigation: check `NeedsCompaction()` after appending tool results, not just
after API responses. If the context is near capacity after tool results,
compact before the next API call. This is not yet implemented — it requires
careful handling because compaction itself requires an API call and could
interfere with the tool call sequence.

---

## 7. Future Considerations

### Turn limit
```go
type Loop struct {
    // ...
    maxTurns int  // 0 = unlimited
    turns    int
}
// In the loop: if l.maxTurns > 0 && l.turns >= l.maxTurns { return ErrTurnLimit }
```

### Streaming tool results back to the model
The current protocol sends tool results as a user message only after **all**
tool calls complete. A future protocol extension might allow streaming tool
results to the model as they arrive — particularly useful for long-running
bash commands where the model could start reasoning about early output while
later output is still arriving. This would require a significant change to
both the API client and the loop, and depends on Anthropic adding API support.

### Structured output from tools
Today tools return `string`. A future version might return structured data
(JSON) that the model can reason about more precisely. The `Tool` interface
would need a `ReturnType() json.RawMessage` method to describe the output
schema, and `ToolResultBlock.Content` would need to accept `json.RawMessage`
as an alternative to `string`.

---

*Previous: [`01-foundation.md`](./01-foundation.md)*  
*Next: [`03-tools-overview.md`](./03-tools-overview.md) — Tool Interface, Registry, and toolutil*
