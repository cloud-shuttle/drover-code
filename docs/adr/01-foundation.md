# 01 — Foundation: Types, API Client, and Streaming

**Package:** `internal/api`  
**Files:** `types.go`, `client.go`, `stream.go`  
**Depends on:** nothing (no internal imports)  
**Depended on by:** everything

---

## Purpose

This package is the boundary between the Anthropic wire format and the rest of
the codebase. It owns three things:

1. **Typed representations** of every concept the Anthropic Messages API deals
   in — content blocks, messages, stream events, tool definitions.
2. **An HTTP client** that builds requests and manages the connection lifecycle.
3. **An SSE stream reader** that parses server-sent events into those typed
   representations and presents them as a clean iterator.

Nothing outside this package ever touches `map[string]any`, raw JSON, or HTTP
response bodies. The type boundary is strict by design.

---

## 1. Types (`types.go`)

### 1.1 The core problem: discriminated unions in Go

The Anthropic API uses a discriminated union for message content — a field
called `type` selects which shape the object takes. TypeScript handles this
naturally with union types. Go does not have sum types, so we use the standard
Go pattern: an unexported marker method on an interface.

```go
type ContentBlock interface {
    isContentBlock()
}

type TextBlock      struct { Text string }
type ToolUseBlock   struct { ID, Name string; Input json.RawMessage }
type ToolResultBlock struct { ToolUseID, Content string; IsError bool }

func (TextBlock) isContentBlock()       {}
func (ToolUseBlock) isContentBlock()    {}
func (ToolResultBlock) isContentBlock() {}
```

The unexported method `isContentBlock()` means only types defined in this
package can satisfy the interface. Callers switch on concrete types:

```go
switch b := block.(type) {
case api.TextBlock:
    fmt.Print(b.Text)
case api.ToolUseBlock:
    executeToolCall(b.ID, b.Name, b.Input)
case api.ToolResultBlock:
    // shouldn't appear in assistant messages — log and skip
}
```

The same pattern applies to `StreamEvent` and `Delta`. Three separate
discriminated unions, each sealed by its own unexported marker method.

### 1.2 Why `json.RawMessage` for tool input

`ToolUseBlock.Input` is `json.RawMessage`, not `any` or `map[string]any`.

Three reasons:

**Deferred parsing.** Each tool parses its own input with its own struct type.
The foundation layer doesn't know (and shouldn't know) what fields `bash` vs
`read_file` vs `glob` expect. `json.RawMessage` passes the bytes through
untouched and lets the tool unmarshal them correctly.

**Accumulation.** Tool input arrives in fragments during streaming
(`InputJSONDelta`). We concatenate string fragments into a `strings.Builder`,
then convert the final result to `json.RawMessage` with a single allocation.
If we tried to parse JSON incrementally we'd need a streaming JSON parser — a
significant complexity increase for no real benefit.

**Round-trip fidelity.** When the assistant's tool call is stored in the
conversation history and sent back to the API in a subsequent turn, the input
must be reproduced exactly. `json.RawMessage` is sent as-is — no re-encoding,
no floating-point drift, no key reordering.

### 1.3 Message constructors

Rather than having callers build `Message` structs directly (error-prone,
especially around the `Content []ContentBlock` field), we provide constructor
functions that enforce correct structure:

```go
// UserMessage wraps plain text in the correct content block structure.
func UserMessage(text string) Message {
    return Message{
        Role:    RoleUser,
        Content: []ContentBlock{TextBlock{Text: text}},
    }
}

// AssistantMessage takes whatever blocks came back from a streaming response.
func AssistantMessage(blocks []ContentBlock) Message {
    return Message{Role: RoleAssistant, Content: blocks}
}

// ToolResultMessage converts a slice of results into the user turn that
// delivers them back to the model. All results go in one message — the API
// requires that tool results for a single assistant response are batched.
func ToolResultMessage(results []ToolResultBlock) Message {
    content := make([]ContentBlock, len(results))
    for i, r := range results {
        content[i] = r
    }
    return Message{Role: RoleUser, Content: content}
}
```

The batching in `ToolResultMessage` is critical. If the model issued three tool
calls and you send three separate user messages (one result each), the API
rejects the conversation because it contains consecutive user messages. All
results for one assistant turn must arrive in a single user turn.

### 1.4 Tool definitions

```go
type ToolDefinition struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    InputSchema json.RawMessage `json:"input_schema"`
}
```

`InputSchema` is a JSON Schema object describing the tool's parameters. The
Anthropic API uses this to validate the model's tool calls and to help the
model understand what parameters to provide.

The `input_schema` field name (snake_case) is what the API expects. Note the
potential confusion: Go convention is `InputSchema` but the JSON key is
`input_schema`. The struct tag handles this automatically.

### 1.5 Stream events

The full event hierarchy:

```
StreamEvent
├── ContentBlockStartEvent   { Index int; ContentBlock ContentBlock }
│       ContentBlock is either TextBlock (empty) or ToolUseBlock (empty Input)
├── ContentBlockDeltaEvent   { Index int; Delta Delta }
│       Delta is either TextDelta or InputJSONDelta
├── ContentBlockStopEvent    { Index int }
├── MessageDeltaEvent        { StopReason string; InputTokens, OutputTokens int }
└── MessageStopEvent
```

The `Index` field on content block events is the position of the block in the
eventual response. It starts at 0 and increments. A typical response with text
followed by a tool call produces:

```
ContentBlockStartEvent{Index: 0, ContentBlock: TextBlock{}}
ContentBlockDeltaEvent{Index: 0, Delta: TextDelta{"Here's what I'll do: "}}
ContentBlockDeltaEvent{Index: 0, Delta: TextDelta{"read the file first."}}
ContentBlockStopEvent{Index: 0}
ContentBlockStartEvent{Index: 1, ContentBlock: ToolUseBlock{ID: "...", Name: "read_file"}}
ContentBlockDeltaEvent{Index: 1, Delta: InputJSONDelta{`{"path": "`}}
ContentBlockDeltaEvent{Index: 1, Delta: InputJSONDelta{`main.go"}`}}
ContentBlockStopEvent{Index: 1}
MessageDeltaEvent{StopReason: "tool_use", InputTokens: 1240, OutputTokens: 89}
MessageStopEvent{}
```

The index is not guaranteed to be contiguous in all future API versions. Our
accumulator uses `map[int]...` rather than a slice to be forward-compatible.

---

## 2. HTTP Client (`client.go`)

### 2.1 Why not the official SDK?

The official `anthropic-sdk-go` exists and is well-maintained. We use raw HTTP
for three reasons:

**Streaming control.** The SDK abstracts the SSE layer. We need direct access
to the `io.ReadCloser` so our SSE parser can control buffering, handle partial
lines correctly, and propagate cancellation through `context.Context` without
an extra goroutine layer.

**Wire format ownership.** The way messages are serialised to JSON is
non-trivial (see §2.3). We want to own that code rather than work around SDK
assumptions.

**Dependency minimalism.** The SDK pulls in additional dependencies. For a
tool whose primary virtue is "single static binary", each dependency is a cost.

This decision is worth revisiting as the SDK matures. The current approach
requires maintaining the marshalling code manually.

### 2.2 Client structure

```go
type Client struct {
    apiKey     string
    baseURL    string  // allows test overrides
    model      string
    httpClient *http.Client
}
```

The `httpClient` has a 10-minute timeout. This is deliberately long — streaming
responses for tasks like "refactor this 5,000 line file" can run for several
minutes. A shorter timeout would cause silent failures on legitimate long tasks.

The timeout applies to the entire request, including the streaming body read.
In practice, the stream produces tokens continuously so the connection stays
alive. A stalled stream (no new tokens for a long period) will eventually hit
the timeout, which is the correct failure behaviour.

### 2.3 Message serialisation

The conversion from `[]Message` to the JSON wire format is the most complex
part of the client. We marshal manually rather than adding JSON struct tags to
`Message` and `ContentBlock` for two reasons:

**The wire format doesn't match our type hierarchy.** The API wants
`{"type": "text", "text": "..."}` inside `content`, but our Go type is
`TextBlock{Text: "..."}` with no `type` field (it's encoded by the Go type
itself). Adding a `type` field to each struct would pollute our domain model
with a concern that belongs only at the serialisation boundary.

**Different content block types go in different message roles.** `ToolUseBlock`
only appears in assistant messages; `ToolResultBlock` only in user messages.
The API does not enforce this at the type level but it's a hard constraint at
runtime. By handling this in the marshaller we can add validation later.

```go
func marshalMessages(msgs []Message) []map[string]any {
    out := make([]map[string]any, len(msgs))
    for i, m := range msgs {
        content := make([]map[string]any, len(m.Content))
        for j, block := range m.Content {
            switch b := block.(type) {
            case TextBlock:
                content[j] = map[string]any{"type": "text", "text": b.Text}
            case ToolUseBlock:
                content[j] = map[string]any{
                    "type":  "tool_use",
                    "id":    b.ID,
                    "name":  b.Name,
                    "input": b.Input,  // json.RawMessage passes through verbatim
                }
            case ToolResultBlock:
                content[j] = map[string]any{
                    "type":        "tool_result",
                    "tool_use_id": b.ToolUseID,
                    "content":     b.Content,
                    "is_error":    b.IsError,
                }
            }
        }
        role := "user"
        if m.Role == RoleAssistant {
            role = "assistant"
        }
        out[i] = map[string]any{"role": role, "content": content}
    }
    return out
}
```

The `json.RawMessage` value in `ToolUseBlock.Input` is special: when
`json.Marshal` encounters a `json.RawMessage` value in a `map[string]any`, it
embeds the raw JSON bytes directly rather than re-encoding them. This is the
correct behaviour — the tool input must round-trip exactly.

### 2.4 Request construction

```go
func (c *Client) StreamMessage(ctx context.Context, req StreamRequest) (*Stream, error) {
    body, _ := c.buildRequestBody(req)

    httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
        c.baseURL+"/v1/messages", bytes.NewReader(body))

    httpReq.Header.Set("x-api-key", c.apiKey)
    httpReq.Header.Set("anthropic-version", "2023-06-01")
    httpReq.Header.Set("content-type", "application/json")
    httpReq.Header.Set("accept", "text/event-stream")  // ← triggers SSE response

    resp, err := c.httpClient.Do(httpReq)
    // non-200 → read body, return structured error
    // 200 → wrap body in Stream, return
}
```

The `accept: text/event-stream` header is what switches the API into streaming
mode. Without it, the API returns a complete JSON response body. We always
use streaming — it lets us start displaying text to the user immediately and
it's the only mode that works correctly with long-running tool-heavy tasks.

### 2.5 Error handling

Non-200 responses read the body and return a structured error:

```go
if resp.StatusCode != http.StatusOK {
    defer resp.Body.Close()
    errBody, _ := io.ReadAll(resp.Body)
    return nil, fmt.Errorf("api error %d: %s", resp.StatusCode, errBody)
}
```

Common error codes and their meanings:

| Status | Meaning | Typical cause |
|---|---|---|
| 400 | Bad request | Malformed message history (consecutive same-role messages) |
| 401 | Unauthorized | Invalid or expired API key |
| 429 | Rate limited | Too many requests; back off and retry |
| 529 | Overloaded | Anthropic capacity; retry with exponential backoff |

Retry logic is not implemented in this package — it belongs in the agent loop
which has the context to decide whether a retry is appropriate. The client
returns the raw error.

---

## 3. SSE Stream Reader (`stream.go`)

### 3.1 The SSE format

Server-sent events is a simple line-based text protocol:

```
event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: message_stop
data: {"type":"message_stop"}

```

Rules:
- Lines starting with `event:` set the event type for the current block
- Lines starting with `data:` carry the payload (always one line per event for Anthropic)
- A blank line terminates a block and signals the consumer to process it
- Lines starting with `:` are comments (Anthropic sends `: ping` keepalives)

### 3.2 Iterator design

We use the `Next() / Event() / Err()` iterator pattern rather than a callback
or channel for two reasons:

**Backpressure.** The caller controls the pace of consumption. If the agent
loop is busy processing a tool result, it can pause consuming from the stream.
With a channel, a goroutine would be blocked trying to send, keeping the HTTP
connection alive but stalled. With the iterator, we simply don't call `Next()`
until ready.

**Error propagation.** Errors (network failures, API error events) are
delivered through the same path as normal events, via `Err()`. The caller
handles them at the same point in the same loop, rather than in a separate
error channel or callback.

```go
type Stream struct {
    scanner *bufio.Scanner
    body    io.Closer
    current StreamEvent
    err     error
    done    bool
}

func (s *Stream) Next() bool   // advances; returns false on done or error
func (s *Stream) Event() StreamEvent  // valid only after Next() == true
func (s *Stream) Err() error   // nil unless Next() returned false due to error
func (s *Stream) Close()       // releases the HTTP connection
```

Callers must always call `Close()`, typically via `defer`:

```go
stream, err := client.StreamMessage(ctx, req)
if err != nil { return err }
defer stream.Close()

for stream.Next() {
    // process stream.Event()
}
return stream.Err()
```

### 3.3 Scanner configuration

```go
sc := bufio.NewScanner(body)
sc.Buffer(make([]byte, 64*1024), 1024*1024)
```

The default `bufio.Scanner` buffer is 64 KB, which is too small for tool input
deltas — a single `input_json_delta` event carrying a large code block can
exceed 64 KB. We configure a 1 MB maximum token size to handle pathological
inputs safely.

### 3.4 The read loop

```go
func (s *Stream) readBlock() (eventType, data string, err error) {
    for s.scanner.Scan() {
        line := s.scanner.Text()
        if line == "" {
            if data != "" || eventType != "" {
                return eventType, data, nil  // complete block
            }
            continue  // skip consecutive blank lines
        }
        if after, ok := strings.CutPrefix(line, "event: "); ok {
            eventType = after
        } else if after, ok := strings.CutPrefix(line, "data: "); ok {
            data = after
        }
        // lines starting with ':' are ignored (SSE comments / pings)
    }
    if err := s.scanner.Err(); err != nil {
        return "", "", err
    }
    return "", "", io.EOF
}
```

`strings.CutPrefix` (Go 1.20+) is cleaner than `strings.HasPrefix` +
`strings.TrimPrefix` — it returns the suffix and a boolean in one call.

### 3.5 Event parsing

The `parseEvent` function switches on the event type string and unmarshals the
JSON data into private wire-format structs, then converts them to our public
types.

```go
func parseEvent(eventType, data string) (StreamEvent, error) {
    switch eventType {
    case "ping", "message_start", "":
        return nil, nil  // intentionally ignored

    case "content_block_start":
        var w wireContentBlockStart
        json.Unmarshal([]byte(data), &w)
        // ... convert w.ContentBlock to TextBlock or ToolUseBlock

    case "content_block_delta":
        var w wireContentBlockDelta
        json.Unmarshal([]byte(data), &w)
        // ... convert w.Delta to TextDelta or InputJSONDelta

    case "content_block_stop":
        // just the index
        return ContentBlockStopEvent{Index: w.Index}, nil

    case "message_delta":
        // stop reason + token counts
        return MessageDeltaEvent{...}, nil

    case "message_stop":
        return MessageStopEvent{}, nil

    case "error":
        // API-level error embedded in the stream
        return nil, fmt.Errorf("api error %s: %s", w.Error.Type, w.Error.Message)

    default:
        return nil, nil  // forward-compatible: ignore unknown event types
    }
}
```

Returning `nil, nil` for unknown event types (and for `ping` / `message_start`)
is the correct forward-compatible behaviour. The API may add new event types
in future versions. Treating unknown types as errors would break existing
clients.

`message_start` carries initial message metadata (model, input token estimate)
that we don't need at this layer — the agent loop gets accurate token counts
from `message_delta` at the end.

### 3.6 Private wire-format types

We use unexported structs for the raw JSON shapes to prevent them leaking into
the rest of the codebase:

```go
type wireContentBlockStart struct {
    Index        int             `json:"index"`
    ContentBlock json.RawMessage `json:"content_block"`
}

type wireContentBlock struct {
    Type  string          `json:"type"`
    Text  string          `json:"text"`   // text block
    ID    string          `json:"id"`     // tool_use block
    Name  string          `json:"name"`   // tool_use block
    Input json.RawMessage `json:"input"`  // tool_use block (always {} initially)
}

type wireDelta struct {
    Type        string `json:"type"`
    Text        string `json:"text"`           // text_delta
    PartialJSON string `json:"partial_json"`   // input_json_delta
}
```

The two-step unmarshal (outer struct → inner `json.RawMessage` → inner struct)
avoids allocating intermediate `map[string]any` values and is about 3× faster
than a single-pass unmarshal into a dynamic structure. For a path that runs on
every single SSE event during streaming, this matters.

### 3.7 The accumulation problem

The stream reader itself only parses individual events. It does *not* accumulate
`InputJSONDelta` fragments into complete tool inputs — that responsibility
belongs to the agent loop's `streamResponse` method.

Why this split? The stream reader is a low-level primitive that doesn't know
about the higher-level concept of "a tool call being assembled". Putting
accumulation here would require the reader to maintain state across events for
multiple concurrent blocks (the API can interleave deltas from multiple blocks
in theory). Keeping the reader stateless makes it easier to test and reason
about.

The agent loop maintains per-index accumulators:

```go
type toolAcc struct {
    id, name string
    jsonBuf   strings.Builder
}
toolAccs := map[int]*toolAcc{}

// On InputJSONDelta:
toolAccs[e.Index].jsonBuf.WriteString(d.PartialJSON)

// On ContentBlockStopEvent for a tool block:
input := json.RawMessage(toolAccs[e.Index].jsonBuf.String())
finalisedBlocks[e.Index] = api.ToolUseBlock{..., Input: input}
```

`strings.Builder` is used rather than `bytes.Buffer` because all the fragments
are strings, avoiding a `[]byte` → `string` conversion at the end. The final
`json.RawMessage(...)` conversion is a zero-copy type assertion since
`json.RawMessage` is `[]byte` and `strings.Builder.String()` returns a new
string — one allocation total for the entire tool input, regardless of how
many fragments arrived.

---

## 4. Testing Strategy

### Unit tests for `types.go`
- Verify constructor functions produce correctly-shaped messages
- Verify `marshalMessages` round-trips through `json.Marshal` to the expected
  wire format (compare against golden JSON files)
- Verify `ToolResultMessage` batches multiple results into one message

### Unit tests for `stream.go`
- Feed synthetic SSE byte sequences through `newStream` and verify events
- Test: single text block, single tool call, interleaved text + tool call
- Test: ping events are ignored
- Test: `message_start` is ignored
- Test: error event returns error from `Next()`
- Test: large `input_json_delta` fragments (> 64 KB) don't panic
- Test: empty stream (just `message_stop`) returns cleanly

### Integration tests for `client.go`
- Use `httptest.Server` to serve canned SSE responses
- Verify the `accept: text/event-stream` header is set
- Verify `x-api-key` is set
- Verify non-200 responses return errors with body content
- Verify `json.RawMessage` tool inputs pass through without re-encoding

### Test helper: `FakeStream`
For testing layers above the client without hitting the network:

```go
// FakeStream implements the same interface as Stream but is driven by
// a slice of pre-built events rather than an HTTP connection.
type FakeStream struct {
    events []StreamEvent
    pos    int
    err    error
}

func (f *FakeStream) Next() bool {
    if f.pos >= len(f.events) { return false }
    f.pos++
    return true
}
func (f *FakeStream) Event() StreamEvent { return f.events[f.pos-1] }
func (f *FakeStream) Err() error         { return f.err }
func (f *FakeStream) Close()             {}
```

---

## 5. Edge Cases and Known Constraints

### Concurrent blocks
The API specification allows multiple content blocks to be streamed
concurrently (interleaved deltas for different indices). Our accumulator
handles this correctly via `map[int]` keyed accumulators. In practice the
current API serialises blocks sequentially, but the code is correct either way.

### Empty tool input
When the model calls a tool with no parameters (e.g. `git_status` which takes
an empty object `{}`), the API sends a `ContentBlockStartEvent` with an empty
`Input` field (`{}`), then immediately a `ContentBlockStopEvent` with no
`InputJSONDelta` events in between. The accumulator's `jsonBuf` remains empty;
`json.RawMessage("")` is an invalid JSON value. Handle this:

```go
raw := toolAccs[e.Index].jsonBuf.String()
if raw == "" {
    raw = "{}"
}
input := json.RawMessage(raw)
```

### Scanner EOF vs error
`bufio.Scanner.Scan()` returns false both on clean EOF and on error.
`scanner.Err()` distinguishes them — it returns nil on clean EOF. Our `readBlock`
checks this correctly: EOF → `io.EOF` → `Next()` returns false without setting
`s.err`.

### Context cancellation
When the caller cancels the context (e.g. user presses Ctrl+C), the
`http.Client.Do()` returns an error, which propagates through `newStream` at
construction time. If cancellation happens mid-stream, the next `scanner.Scan()`
call fails because the underlying `http.Response.Body` is closed by the HTTP
client. The scanner returns false, `scanner.Err()` returns a non-nil error,
and our `readBlock` propagates it through `s.err`.

---

## 6. Future Considerations

### Retry with exponential backoff
429 and 529 responses should be retried. A `RetryingClient` wrapper that
implements the same interface as `Client` but adds retry logic would allow the
agent loop to remain simple while getting resilient network behaviour. The
retry client should respect the `Retry-After` header when present.

### Batch API
For the dream memory consolidation and coordinator decompose/synthesise steps
(which are non-streaming, fire-and-forget calls), the Anthropic Batch API would
be more efficient than individual streaming requests. This is a future
optimisation — the current approach (regular streaming calls) is correct and
sufficient.

### Request signing
If drover-code is ever deployed in an environment that uses IAM or similar
credential rotation, the client would need to support pluggable authentication.
The current design hard-codes API key auth via `x-api-key`. A `Credentials`
interface with an `AddHeaders(req *http.Request)` method would make this
extensible without changing the rest of the client.

---

*Next: [`02-agent-loop.md`](./02-agent-loop.md) — Conversation Manager, Agent Loop, and Event System*
