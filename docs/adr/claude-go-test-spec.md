## drover-code — Test Specification

This document describes the automated tests we implement before wiring a real `ANTHROPIC_API_KEY`.

### Goals

- Validate **core correctness** (stream parsing, tool call assembly, loop behavior).
- Validate **safety** invariants (path containment, binary detection, edit ambiguity refusal).
- Validate **compatibility** with expected wire formats (Anthropic SSE + JSON-RPC framing).
- Keep tests **deterministic** and **offline** where possible (use `httptest` and temp dirs).

---

## 1) `internal/api` tests

### 1.1 Stream parser (`internal/api/stream.go`)

- **Parses basic text streaming**
  - Input: SSE sequence `content_block_start(text)` → multiple `content_block_delta(text_delta)` → `content_block_stop` → `message_delta` → `message_stop`
  - Assert: `Stream.Next()` yields `ContentBlockStartEvent`, `ContentBlockDeltaEvent`(TextDelta), `ContentBlockStopEvent`, `MessageDeltaEvent` in order.

- **Accumulates tool input JSON fragments**
  - Input: `content_block_start(tool_use)` + multiple `content_block_delta(input_json_delta)` + `content_block_stop`
  - Assert: deltas surface as `InputJSONDelta.PartialJSON` and can be concatenated to reconstruct the original JSON string.

- **Ignores benign events**
  - Input includes `ping` and `message_start`
  - Assert: those do not surface from `Next()`.

- **Error event becomes stream error**
  - Input: `event: error` with JSON payload
  - Assert: `Next()` terminates; `Err()` is non-nil and contains the error message.

### 1.2 Client (`internal/api/client.go`)

Using an `httptest` server:

- **Headers**
  - Assert request includes: `x-api-key`, `anthropic-version`, `accept: text/event-stream`, `content-type: application/json`

- **Request JSON shape**
  - `stream: true`, `model`, `max_tokens`, `messages` are present
  - User message → wire `{"type":"text","text":...}`
  - Tool result message → wire `{"type":"tool_result","tool_use_id":...,"content":...,"is_error":...}`
  - Tool use message (assistant content) uses `{"type":"tool_use","id":...,"name":...,"input":<json>}`

---

## 2) `internal/agent` tests

### 2.1 End-to-end loop without real API key (`internal/agent/loop.go`)

Using `httptest` for `/v1/messages` with deterministic SSE:

- **Tool call executes and feeds back**
  - First streamed response: emits a tool_use block (complete JSON input)
  - Assert: tool executes; events include `ToolStartEvent` → `ToolDoneEvent`
  - Second streamed response: returns final text; assert `DoneEvent` emitted

- **Parallel tool execution preserves result ordering**
  - First response contains two independent tool_use blocks.
  - Tools sleep; assert wall-clock is closer to max(sleeps) than sum(sleeps).
  - Assert tool_result blocks are appended in the same order as tool_use blocks.

---

## 3) `internal/tools` tests

### 3.1 File system tools (`internal/tools/fs`)

Use `t.TempDir()` with tool WorkDir set to that directory.

- **`read_file`**
  - Refuses binary (null byte) files
  - Line slicing includes correct 1-based numbering

- **`write_file`**
  - Creates parents
  - Uses atomic semantics (at least no partial writes; practical test is “file content equals expected”)

- **`edit_file`**
  - Exact match replaces once
  - Fuzzy match (whitespace differences) replaces once
  - Multiple matches → returns an error and does not modify the file

- **`list_directory` / `file_info`**
  - Returns expected metadata and existence behavior

### 3.2 Search tools (`internal/tools/search`)

- **`glob`**
  - `**` matches recursively
  - Hidden directories are skipped (e.g. `.git`) unless explicitly targeted

- **`grep`**
  - Pure Go backend returns expected match formatting with context lines
  - Does not require `rg` installed (tests should not depend on `rg`)

### 3.3 `web_fetch` (`internal/tools/web`)

With `httptest`:

- Text/plain returns raw text
- text/html returns stripped text (no tags)

---

## 4) `internal/permissions` tests

- **Mode bypass**
  - `ModeBypass` always allows

- **Deny beats allow**
  - Config deny list denies even if allow list contains tool

- **AlwaysAllow persistence**
  - When prompt returns `AlwaysAllow`, engine writes JSON to rules path
  - New engine instance loads and allows without prompting

---

## 5) `internal/bridge` tests

- **Send framing**
  - `Bridge.Send` writes `Content-Length: N\r\n\r\n<json>`
  - `N` equals byte length of the JSON payload

- **Read framing**
  - `readMessage` parses a framed JSON-RPC message from an input stream

---

## 6) `internal/tools/git` tests (integration)

Use a temp git repo:

- Initialize repo, create a file, commit
- Assert `git_status`, `git_log` return expected non-empty outputs

If `git` is not available in PATH, skip the suite.

