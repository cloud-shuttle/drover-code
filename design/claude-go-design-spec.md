# drover-code: Go Port of Claude Code — Design Specification

**Version:** 1.0  
**Module:** `github.com/cloudshuttle/drover-code`  
**Goal:** Full feature parity with Claude Code as a single static binary with no Node/Bun dependency, using BubbleTea for the terminal UI.

---

## Table of Contents

1. [Motivation and Constraints](#1-motivation-and-constraints)
2. [Module Structure](#2-module-structure)
3. [Core API Types](#3-core-api-types)
4. [API Client and Streaming](#4-api-client-and-streaming)
5. [Conversation Context Manager](#5-conversation-context-manager)
6. [Tool Interface and Registry](#6-tool-interface-and-registry)
7. [Agent Loop](#7-agent-loop)
8. [Terminal UI (BubbleTea)](#8-terminal-ui-bubbletea)
9. [Slash Commands](#9-slash-commands)
10. [File System Tools](#10-file-system-tools)
11. [Shell Tool (bash)](#11-shell-tool-bash)
12. [Search Tools (glob, grep)](#12-search-tools-glob-grep)
13. [Git Tools](#13-git-tools)
14. [Web Fetch Tool](#14-web-fetch-tool)
15. [Config and CLAUDE.md Injection](#15-config-and-claudemd-injection)
16. [Permission Engine](#16-permission-engine)
17. [Undercover Mode](#17-undercover-mode)
18. [Dream Memory System](#18-dream-memory-system)
19. [Coordinator Mode](#19-coordinator-mode)
20. [IDE Bridge](#20-ide-bridge)
21. [GitHub @drover-code Webhook Integration](#21-github-claude-webhook-integration)
22. [Phased Delivery](#22-phased-delivery)
23. [CGO Avoidance](#23-cgo-avoidance)
24. [Dependency List](#24-dependency-list)
25. [Build and Run Reference](#25-build-and-run-reference)

---

## 1. Motivation and Constraints

### Primary goal
A single static binary (`CGO_ENABLED=0 go build`) that delivers full Claude Code feature parity without requiring Node.js, Bun, or any native library dependencies. Drop it on any Linux/macOS/Windows machine with an `ANTHROPIC_API_KEY` and it runs.

### Non-negotiable constraints
- `CGO_ENABLED=0` enforced in CI from day one — no regression
- Compatible with existing `.claude/settings.json`, `.claude/permissions.json`, and `CLAUDE.md` files so users can share project configuration regardless of which client they use
- IDE bridge uses JSON-RPC over stdio with drover-specific method names (e.g. `drover/execute`); adapters may be needed for extensions that still call `claude/execute`
- Webhook integration is protocol-compatible with GitHub's delivery system

### Key architectural departure from the TypeScript original
The original runs on Bun with React/Ink for the terminal UI. The Go port replaces:

| Original | Go port |
|---|---|
| Bun runtime | Standard `go build` |
| React/Ink terminal renderer | BubbleTea (Elm architecture) |
| `node_modules` | Single binary, zero runtime deps |
| `bun:bundle` feature flags | Build tags + `ldflags` |
| tiktoken (Python) | Pure Go tokenizer |
| better-sqlite3 (CGO) | `modernc.org/sqlite` or JSON flat file |

---

## 2. Module Structure

```
cmd/drover-code/
    main.go                  ← entrypoint; subcommand dispatch

internal/
    api/
        types.go             ← ContentBlock, Message, StreamEvent, ToolDefinition
        client.go            ← HTTP client, request builder
        stream.go            ← SSE parser, Stream iterator

    convo/
        manager.go           ← thread-safe conversation history, compaction

    tools/
        registry.go          ← Tool interface, Registry, PermissionFunc
        register.go          ← RegisterAll() wires all tool packages
        toolutil/
            util.go          ← WriteAtomic, SafePath, Schema builder, Truncate
        fs/
            read.go          ← read_file
            write.go         ← write_file
            edit.go          ← edit_file (fuzzy match + unified diff)
            ls.go            ← list_directory, file_info
        shell/
            bash.go          ← bash (subprocess, timeout, stdout/stderr split)
        search/
            glob.go          ← glob (** support, pure Go)
            grep.go          ← grep (rg if available, pure Go fallback)
        git/
            git.go           ← git_status, git_diff, git_log, git_add,
                                git_commit, git_push, git_create_branch
        web/
            fetch.go         ← web_fetch (HTML→text, no CGO)

    agent/
        events.go            ← typed event channel messages
        loop.go              ← agentic loop: stream → collect → execute → repeat

    tui/
        styles.go            ← lipgloss styles, colour palette
        messages.go          ← tea.Msg wrappers, waitForEvent command
        model.go             ← BubbleTea Model, Init, Update
        view.go              ← View() — pure render function
        permission.go        ← permission overlay component
        program.go           ← Program wrapper, makePermitFn, Run()

    config/
        loader.go            ← three-level settings merge, CLAUDE.md walker

    permissions/
        engine.go            ← rule priority chain, persistence, mode switching

    undercover/
        undercover.go        ← public repo detection, system prompt injection

    dream/
        dream.go             ← Store interface, JSON store, Worker, BuildInjection

    coordinator/
        coordinator.go       ← decompose → parallel workers → synthesise

    bridge/
        bridge.go            ← JSON-RPC over stdio (LSP wire format)

    github/
        types.go             ← webhook payload shapes, Trigger, ReplyTarget
        client.go            ← GitHub REST API client, signature verification
        parser.go            ← @drover-code mention extraction from all event types
        runner.go            ← placeholder → clone → agent → update comment
        server.go            ← HTTP webhook server, dedup, semaphore, job queue
```

---

## 3. Core API Types

### Design rationale
All Anthropic wire-format types are defined once in `internal/api` and imported everywhere. Nothing outside this package ever touches `map[string]any` or raw JSON for content blocks.

### Key types

```go
// ContentBlock — discriminated union
type ContentBlock interface{ isContentBlock() }
type TextBlock    struct{ Text string }
type ToolUseBlock struct{ ID, Name string; Input json.RawMessage }
type ToolResultBlock struct{ ToolUseID, Content string; IsError bool }

// StreamEvent — discriminated union of SSE events
type StreamEvent interface{ isStreamEvent() }
type ContentBlockStartEvent  struct{ Index int; ContentBlock ContentBlock }
type ContentBlockDeltaEvent  struct{ Index int; Delta Delta }
type ContentBlockStopEvent   struct{ Index int }
type MessageDeltaEvent       struct{ StopReason string; InputTokens, OutputTokens int }
type MessageStopEvent        struct{}

// Delta — what arrives inside a content_block_delta
type Delta interface{ isDelta() }
type TextDelta      struct{ Text string }
type InputJSONDelta struct{ PartialJSON string }
```

### Constructor helpers

```go
func UserMessage(text string) Message
func AssistantMessage(blocks []ContentBlock) Message
func ToolResultMessage(results []ToolResultBlock) Message
```

---

## 4. API Client and Streaming

### Client
Thin raw HTTP client — no SDK dependency. Controls headers, timeouts, and error handling directly.

- Base URL: `https://api.anthropic.com`
- Timeout: 10 minutes (streaming responses for large tasks can be long)
- Auth: `x-api-key` header
- Accept: `text/event-stream` for streaming

### Stream iterator
SSE parser that presents a clean `Next() / Event() / Err() / Close()` iterator interface.

```go
stream, err := client.StreamMessage(ctx, req)
defer stream.Close()
for stream.Next() {
    switch e := stream.Event().(type) {
    case api.ContentBlockDeltaEvent: ...
    }
}
if err := stream.Err(); err != nil { ... }
```

### Critical implementation detail
Tool input JSON arrives in fragments (`InputJSONDelta`) across multiple SSE events. The stream reader accumulates per-block string builders keyed by content block index, and only finalises a `ToolUseBlock` (with complete `json.RawMessage` input) on `ContentBlockStopEvent`. The agent loop never sees partial tool inputs.

---

## 5. Conversation Context Manager

### Responsibilities
- Thread-safe conversation history (needed for coordinator mode's concurrent reads)
- System prompt management with live replacement
- Token budget estimation
- Compaction (summarise-and-truncate)

### Interface

```go
type Manager struct { /* sync.RWMutex, []api.Message, systemPrompt, tokenCount */ }

func NewManager() *Manager
func NewManagerWithSystem(prompt string) *Manager
func (m *Manager) Append(msg api.Message)
func (m *Manager) Messages() []api.Message      // returns a snapshot copy
func (m *Manager) SetSystemPrompt(s string)
func (m *Manager) SystemPrompt() string
func (m *Manager) NeedsCompaction() bool
func (m *Manager) EstimatedTokens() int
func (m *Manager) Summarise(summary string, keepTail int)
```

### Token estimation
Phase 1–3: `chars / 4` (conservative heuristic, ~4 chars/token for English code/prose).  
Phase 3+: `github.com/tiktoken-go/tokenizer` (pure Go, no CGO).

### Compaction
When `EstimatedTokens() > 180_000`, the `/compact` slash command (or automatic trigger) fires a separate non-streaming API call requesting a 3–5 bullet summary of the older messages. The summary replaces those messages with a single synthetic user turn wrapped in a sentinel phrase:

```
[Earlier conversation summary — treat as background context]
<summary text>
```

---

## 6. Tool Interface and Registry

### Tool interface

```go
type Tool interface {
    Name()            string
    Description()     string
    InputSchema()     json.RawMessage  // JSON Schema object
    NeedsPermission(input json.RawMessage) bool
    Execute(ctx context.Context, input json.RawMessage) (string, error)
}
```

All tool implementations must be goroutine-safe — coordinator mode calls them concurrently from multiple worker agents.

### Registry

```go
type Registry struct { /* sync.RWMutex, map[string]Tool */ }

func (r *Registry) Register(t Tool)
func (r *Registry) Get(name string) Tool
func (r *Registry) Definitions() []api.ToolDefinition  // for API calls
func (r *Registry) Execute(ctx context.Context, name string, input json.RawMessage) (string, error)
func (r *Registry) NeedsPermission(name string, input json.RawMessage) bool
```

### PermissionFunc

```go
type PermissionFunc func(ctx context.Context, req PermissionRequest) Decision

// AllowAll approves everything without prompting — used in headless and worker-agent modes.
func AllowAll(_ context.Context, _ PermissionRequest) Decision { return Allow }
```

### Schema builder
`toolutil.NewSchema()` fluent builder produces `json.RawMessage` JSON Schema objects without code generation or reflection:

```go
toolutil.NewSchema("object").
    Prop("path", toolutil.NewSchema("string").Desc("File path")).
    Prop("content", toolutil.NewSchema("string").Desc("Content to write")).
    Required("path", "content").
    Build()
```

---

## 7. Agent Loop

### The core loop

```go
func (l *Loop) Run(ctx context.Context, input string) error {
    l.convo.Append(api.UserMessage(input))
    for {
        blocks, usage, err := l.streamResponse(ctx)  // stream → collect
        l.convo.Append(api.AssistantMessage(blocks))

        calls := extractToolCalls(blocks)
        if len(calls) == 0 { l.emit(DoneEvent{}); return nil }

        results, err := l.executeTools(ctx, calls)   // parallel execution
        l.convo.Append(api.ToolResultMessage(results))
    }
}
```

### Parallel tool execution
Multiple tool calls within a single assistant response execute concurrently using `errgroup.WithContext`. Results are collected into a fixed-size slice (preserving original order for model correlation by `tool_use_id`).

```go
g, gctx := errgroup.WithContext(ctx)
for i, call := range calls {
    i, call := i, call
    g.Go(func() error {
        results[i], _ = l.executeSingleTool(gctx, i, call)
        return nil
    })
}
g.Wait()
```

### Event channel
The loop emits typed events on a `chan<- agent.Event` as things happen. The consumer (TUI or CLI printer) drains this on a separate goroutine. Events:

| Event | When emitted |
|---|---|
| `TextDeltaEvent` | Each streaming text token |
| `ToolStartEvent` | After permission granted, before Execute |
| `ToolDoneEvent` | After Execute returns |
| `PermissionRequestEvent` | When a tool needs user approval |
| `UsageEvent` | After each API response completes |
| `DoneEvent` | Response complete, no tool calls |
| `ErrorEvent` | Fatal error in the loop |

`emit()` is non-blocking (drops if full) so the loop is never stalled by a slow consumer.

### Cancellation
All tool subprocesses receive the loop's `context.Context`. `exec.CommandContext` sends SIGKILL on cancellation — no zombie processes. Ctrl+C in the TUI cancels the context, which propagates to the current stream read and any running tool.

---

## 8. Terminal UI (BubbleTea)

### Architecture
BubbleTea uses the Elm architecture: single `Model`, pure `Update(msg) (Model, tea.Cmd)`, pure `View() string`. The concurrent I/O pattern:

- Agent loop runs as a `tea.Cmd` (off the main goroutine)
- Each streaming event is delivered as a `tea.Msg` via `program.Send()`
- `waitForEvent(ch)` is a blocking `tea.Cmd` that re-arms after each event

### Layout (top to bottom)

```
┌──────────────────────────────┐
│  viewport (scrollable history) │  ← glamour-rendered markdown turns
│                              │
│  [live region]               │  ← streaming text + tool spinners
├──────────────────────────────┤
│  status bar                  │  ← model name | ● busy | in:1.2k out:450
├──────────────────────────────┤
│  input / permission prompt   │  ← textarea or permission overlay
└──────────────────────────────┘
```

### Libraries

| Library | Purpose |
|---|---|
| `charmbracelet/bubbletea` | Main framework |
| `charmbracelet/bubbles` | `textarea`, `spinner`, `viewport`, `list` |
| `charmbracelet/lipgloss` | Layout, adaptive colours, borders |
| `charmbracelet/glamour` | Markdown → ANSI in the viewport |

### Colour theme
Adaptive (`lipgloss.AdaptiveColor`) — works in light and dark terminals without any manual checks. Cool monochromatic base with a single amber accent.

### Permission prompt
When `permPrompt != nil`, `Update()` routes all keypresses to the permission overlay before anything else. The overlay renders above the input and blocks until `y` (allow once), `a` (always allow), or `n` (deny). The agent loop goroutine blocks on a channel until the decision arrives.

### Circular dependency resolution
`Model` needs to call `Loop.Run()`; `Loop` needs the `eventCh` that `Model` owns. Solved by injecting a `RunFunc` field on the model via `SetRunFunc()` after both are constructed.

---

## 9. Slash Commands

### Interface

```go
type Command interface {
    Name()        string
    Description() string
    Aliases()     []string
    Execute(ctx context.Context, args string, env *Env) error
}
```

`Env` carries the agent loop, context manager, config, and TUI handle. Autocomplete in the `textarea` is a prefix match on `/` — displayed as a rounded-border list overlay above the input.

### Key commands

| Command | Behaviour |
|---|---|
| `/compact` | Summarise + truncate context |
| `/clear` | Reset conversation history |
| `/model` | Swap model mid-session |
| `/memory` | Read/write dream entries |
| `/permissions` | Modify engine rules |
| `/tokens` | Show token usage |
| `/quit` | Exit |

---

## 10. File System Tools

### `read_file`
- Binary detection: null byte or invalid UTF-8 in first 8 KB → refuse
- Line range: `start_line` / `end_line` (1-based, inclusive); annotates output with line numbers
- Output capped at 200,000 bytes; truncation is noted so the model knows there is more
- `NeedsPermission: false`

### `write_file`
- Atomic write: temp file in same directory → `os.Rename`
- Creates parent directories with `os.MkdirAll`
- Preserves existing file permissions if file already exists
- `NeedsPermission: true`

### `edit_file`
Three-pass matching:

1. **Exact match** — `strings.Count(content, oldStr)`: if 1, replace directly; if >1, refuse with count
2. **Fuzzy match** — normalise whitespace on each line independently (collapse runs, trim trailing); repeat count check; if 1, locate in original and splice in replacement
3. **Not found** — return error with the searched string so the model can self-correct

Returns a unified diff of the change so the model can verify the result.  
`NeedsPermission: true`

### `list_directory`
Shows name, type (file/dir/link), size, and last-modified time. Non-recursive.  
`NeedsPermission: false`

### `file_info`
Stat equivalent: size, permissions, modification time, existence, symlink target.  
`NeedsPermission: false`

---

## 11. Shell Tool (bash)

```go
cmd := exec.CommandContext(cmdCtx, "bash", "-c", inp.Command)
cmd.Dir = workDir
// Capture stdout and stderr separately
var stdout, stderr bytes.Buffer
cmd.Stdout = &stdout
cmd.Stderr = &stderr
```

- Inherits the user's full environment
- Default timeout: 120 seconds; max: 600 seconds; enforced via `context.WithTimeout`
- stdout and stderr returned separately with labels — the model needs to distinguish them
- Exit code always reported, even for zero
- `NeedsPermission: true` (always — most powerful tool)

Output format:
```
$ <command>
exit_code: <N>  elapsed: <T>ms

[stdout]
<stdout text>

[stderr]
<stderr text>
```

---

## 12. Search Tools (glob, grep)

### `glob`
Pure Go `**` implementation — `filepath.Glob` does not support `**`.

Algorithm: `filepath.WalkDir` + recursive segment matching:
- `**` matches zero or more path components
- `*` matches any characters within one component
- `?` matches one character within one component

Skips hidden directories (`.git`, `.node_modules`) unless the pattern explicitly targets them. Cap: 1,000 matches.

### `grep`
Strategy: use `rg` (ripgrep) if found via `exec.LookPath("rg")`, otherwise pure Go.

**ripgrep path:** subprocess with `--line-number --no-heading --color=never --context=N`. rg exit code 1 = no matches (not an error).

**Pure Go path:** `filepath.WalkDir` + `regexp.Compile` + line-by-line scanner with a 1 MB buffer (handles minified files). Returns matches in rg-compatible format: `file:line:content` with `--` separators between groups.

Both paths respect `file_pattern` (glob filter), `context_lines`, `max_matches`, and `case_sensitive`.

---

## 13. Git Tools

All tools shell out to the `git` binary rather than using `go-git`. Rationale: git configuration, hooks, credentials, sparse-checkouts, and worktrees all work correctly with the subprocess approach. `go-git` diverges from native git in ways that create hard-to-debug discrepancies.

| Tool | Command | Needs permission |
|---|---|---|
| `git_status` | `git status --short --branch` | No |
| `git_diff` | `git diff --unified=3 [--cached] [base] [-- path]` | No |
| `git_log` | `git log -n<N> [--oneline] [-- path]` | No |
| `git_add` | `git add -A` or `git add <paths>` | Yes |
| `git_commit` | `git commit -m <message>` | Yes |
| `git_push` | `git push <remote> [branch] [--force-with-lease]` | Yes |
| `git_create_branch` | `git checkout -b <name> [from_ref]` | Yes |

`git_push` uses `--force-with-lease` when `force: true` — refuses if the remote has commits the pusher hasn't seen, preventing accidental overwrites of others' work.

---

## 14. Web Fetch Tool

```go
client: &http.Client{Timeout: 30 * time.Second}
// Response cap: 2 MB
// HTML → text: inline parser (no CGO dependency)
// User-Agent: drover-code/1.0
```

HTML stripping: minimal inline parser — strips script/style blocks, converts block-level elements to newlines, decodes the six most common HTML entities. No CGO, no external library. Can be replaced with `github.com/JohannesKaufmann/html-to-markdown` once dependency fetching is available.

`NeedsPermission: false`

---

## 15. Config and CLAUDE.md Injection

### Three-level settings merge

| Priority | File | Notes |
|---|---|---|
| 1 (lowest) | `~/.claude/settings.json` | Global; user-wide |
| 2 | `.claude/settings.json` | Project; committed |
| 3 (highest) | `.claude/settings.local.json` | Local; gitignored |

Merge is field-level (not JSON merge-patch): a zero/empty value in a higher-priority file does not overwrite a non-zero value from a lower-priority file.

### Settings fields

```go
type Settings struct {
    Model           string            // model string override
    PermissionMode  string            // "default" | "plan" | "bypassPermissions"
    AllowedTools    []string          // auto-approved tool names
    DeniedTools     []string          // always-denied tool names
    MaxTokens       int               // per-request token cap override
    CoordinatorMode bool              // enable multi-agent coordinator
    DreamEnabled    bool              // enable background memory consolidation
    UndercoverMode  *bool             // nil = auto-detect from remote URL
    Env             map[string]string // extra env vars for bash tool
}
```

### CLAUDE.md injection
Walk upward from `workDir` to the user's home directory, collecting `CLAUDE.md` files. Also check `~/.claude/CLAUDE.md` (global instructions). Concatenate in outermost-first order (most general → most specific), separated by section headers. Inject into the system prompt at session start.

`Save()` always writes to the project level (`.claude/settings.json`), never the global level.

---

## 16. Permission Engine

### Rule priority chain (checked in order)

1. **Bypass mode** → allow everything (worker agents in coordinator mode)
2. **Config deny list** → always deny
3. **Persisted deny rule** (`.claude/permissions.json`) → always deny
4. **Config allow list** → always allow
5. **Persisted allow rule** → always allow
6. **Tool reports `NeedsPermission: false`** → allow
7. **Fall through to `promptFn`** → block until user decides

### Modes

| Mode | Behaviour |
|---|---|
| `ModeDefault` | Prompts for anything marked `NeedsPermission: true` |
| `ModePlan` | Auto-approves read-only tools; queues write/exec tools for batch approval |
| `ModeBypass` | Approves everything without prompting |

### Persistence
`AlwaysAllow` decisions are written to `.claude/permissions.json` via atomic rename. Loaded at startup. Format: `[{"tool": "bash", "kind": 0}]` where kind 0 = allow, 1 = deny.

### Integration
`Engine.WrapPermitFn()` returns a `tools.PermissionFunc` that routes through the full priority chain. This replaces `tools.AllowAll` in interactive modes.

---

## 17. Undercover Mode

When operating on a public/open-source repository, the model must not reveal internal codenames, model versions, or that commits/PRs were written by an AI. Matches `utils/undercover.ts` from the leaked Claude Code source.

### Detection

```go
func Detect(repoPath string) Status {
    remote := gitRemoteURL(repoPath)   // git remote get-url origin
    if contains(remote, "github.com") && !isInternalDomain(remote) {
        return Status{Active: true}
    }
    // gitlab.com, bitbucket.org → active
    // self-hosted, no remote → inactive
}
```

Auto-detection can be overridden by `settings.UndercoverMode` (a `*bool` — nil means auto-detect).

### System prompt injection
When active, the following fragment is appended to the system prompt:

```
## UNDERCOVER MODE — CRITICAL
Do not mention internal model codenames (Capybara, Tengu, etc.),
unreleased model version numbers, internal tooling, or that you are
an AI. Do not include Co-Authored-By lines or AI attribution.
Write commit messages and PR descriptions as a human developer would.
```

---

## 18. Dream Memory System

Background memory consolidation that gives the model continuity across sessions.

### Flow
1. Session ends → `Worker.Trigger(session)` called (non-blocking, buffered channel of 8)
2. Background goroutine wakes, makes a non-streaming API call requesting 3–5 bullet summary
3. Summary persisted to `Store` with timestamp and extracted file-path tags
4. Next session start: `BuildInjection(store, 5)` loads most recent 5 entries
5. Injection prepended to system prompt as `## Memory from previous sessions`

### Storage
Default: JSON flat file at `.claude/memory.json` (atomic writes via temp + rename).  
Future: swap `Store` interface implementation to `modernc.org/sqlite` (pure Go, no CGO) for better query performance on large memory sets.

### Store interface

```go
type Store interface {
    Save(e Entry) error
    Recent(n int) ([]Entry, error)
    All() ([]Entry, error)
}
```

Memories are best-effort — errors are silently dropped. If the summarisation API call times out (60 second cap) the session is skipped.

---

## 19. Coordinator Mode

Activated via `DROVER_CODE_COORDINATOR_MODE=1` or `settings.CoordinatorMode: true`.

### Pipeline

```
User request
    │
    ▼
Coordinator LLM call → JSON array of 2-4 subtask descriptions
    │
    ├── Worker agent 0 (isolated convo.Manager, AllowAll perms)
    ├── Worker agent 1 (isolated convo.Manager, AllowAll perms)
    ├── Worker agent 2 (isolated convo.Manager, AllowAll perms)
    │   ... up to MaxWorkers=4 concurrent
    │
    ▼
Synthesis LLM call → merged response streamed to user
```

### Worker isolation
Each worker gets:
- Its own `convo.Manager` seeded with a focused system prompt for its subtask
- Its own tool registry instances (tools are goroutine-safe but benefit from per-worker config)
- `ModeBypass` permissions — the coordinator made the permission decision
- Its own event channel; tool events relabelled with `workerIdx*100 + callIndex` to avoid TUI index collisions

### Concurrency control
`errgroup.WithContext` + a semaphore channel of size `MaxWorkers` caps concurrent goroutines. Worker failures do not propagate as Go errors — they are captured in `WorkerResult.IsError` so the synthesis step can describe partial failures to the user.

### Event forwarding
Worker tool events are relabelled and forwarded to the coordinator's event channel so the TUI can show parallel spinner activity during execution.

---

## 20. IDE Bridge

Bidirectional JSON-RPC over stdio using the LSP wire format (length-prefixed JSON):

```
Content-Length: <N>\r\n
\r\n
<N bytes of JSON>
```

### Activation
`DROVER_CODE_IDE_BRIDGE=1` environment variable. In bridge mode the TUI is suppressed entirely — the IDE extension owns the UI.

### Protocol methods

| Method | Direction | Description |
|---|---|---|
| `initialize` | Extension → CLI | Capability negotiation |
| `drover/execute` | Extension → CLI | Run an agent turn |
| `ping` | Either | Health check |

### Capability advertisement (`initialize` response)

```json
{
  "capabilities": { "execute": true, "streamTokens": true },
  "serverInfo": { "name": "drover-code", "version": "0.1.0" }
}
```

### Concurrency
`Bridge.Request()` uses a `pending map[int64]chan Message` — in-flight requests are matched to responses by `id` field. Thread-safe via `sync.Mutex`. The read loop runs on its own goroutine; handlers are dispatched on new goroutines.

---

## 21. GitHub @drover-code Webhook Integration

Activated via `drover-code webhook` subcommand.

### Trigger detection
Parses `X-GitHub-Event` header + body. Triggers on:
- `issue_comment` (created) on any issue or PR containing `@drover-code <request>`
- `pull_request_review_comment` (created) on a PR diff containing `@drover-code <request>`

Multi-line requests are supported: continuation lines (no blank line, no leading `@`) are appended to the request.

### End-to-end flow

```
GitHub sends webhook
    │
    ▼
VerifySignature (HMAC-SHA256)
    │
    ▼
ParseWebhook → Trigger{Request, Context, ReplyTarget}
    │
    ▼
Deduplication check (one active job per issue/PR)
    │
    ▼
PostIssueComment("_Processing…_") → placeholder commentID
    │
    ├── git clone --depth=1 --branch=<PR head ref> <repo URL>
    │
    ├── Build system prompt from PR/issue metadata + diff context
    │
    ├── agent.Loop.Run(ctx, trigger.Request)
    │       (tool registry rooted at clone dir)
    │
    ▼
UpdateComment(commentID, response)   ← always runs, even on error
    │
    ▼
os.RemoveAll(cloneDir)
```

### Configuration

| Env var | Required | Default | Description |
|---|---|---|---|
| `ANTHROPIC_API_KEY` | Yes | — | Anthropic API key |
| `GITHUB_TOKEN` | Yes | — | GitHub PAT or installation token |
| `GITHUB_WEBHOOK_SECRET` | No | — | Webhook signature secret |
| `WEBHOOK_ADDR` | No | `:8080` | HTTP listen address |
| `WEBHOOK_WORK_DIR` | No | `/tmp/drover-code-work` | Clone base directory |

### Safety properties
- HMAC-SHA256 signature verification with `hmac.Equal` (constant-time comparison)
- Deduplication map prevents concurrent agents on the same issue/PR
- Semaphore of size 5 caps concurrent agent jobs
- 10-minute per-job context timeout
- `--depth=1` shallow clone minimises disk and network usage
- Clone directory removed on job completion (success or error)
- `confirmed: true` on all PR review comment posts (never posts partial comments)

---

## 22. Phased Delivery

### Phase 1 — Working agent, headless only
API client, SSE streaming, agent loop, tool registry, core tools (`read_file`, `write_file`, `edit_file`, `bash`, `glob`, `grep`), permission engine (CLI prompt), basic conversation manager.

**Deliverable:** `ANTHROPIC_API_KEY=sk-... drover-code` in a terminal. No fancy UI; streaming text to stdout.

### Phase 2 — Full BubbleTea TUI
BubbleTea model + view, lipgloss styles, glamour markdown rendering, permission overlay, slash command autocomplete, tool spinners, status bar, `waitForEvent` pump.

**Deliverable:** Full interactive terminal UI matching Claude Code's visual experience.

### Phase 3 — Full tool coverage
Remaining tools (`list_directory`, `file_info`, `web_fetch`, all git tools), config/CLAUDE.md injection, token counting + compaction, dream memory system.

**Deliverable:** Feature-complete tool set; session persistence via memory.

### Phase 4 — Advanced systems
Permission engine persistence, coordinator mode, IDE bridge, undercover mode, GitHub @drover-code webhook integration.

**Deliverable:** Full Claude Code feature parity.

---

## 23. CGO Avoidance

| Concern | Solution |
|---|---|
| SQLite (dream memory) | `modernc.org/sqlite` (pure Go mechanical translation) or JSON flat file |
| Token counting | `github.com/tiktoken-go/tokenizer` (pure Go) |
| `**` glob | Custom pure Go implementation (stdlib lacks it) |
| HTML → text | Minimal inline parser; no libxml2 |
| Git operations | `git` subprocess (no `go-git` divergence issues) |
| ripgrep | Runtime detection via `exec.LookPath`; pure Go fallback |

**CI gate:** `CGO_ENABLED=0 go build ./...` runs on every commit. CGO regression is a build failure.

---

## 24. Dependency List

### Runtime dependencies

| Package | Version | Purpose |
|---|---|---|
| `github.com/charmbracelet/bubbletea` | v1.2.4 | TUI framework |
| `github.com/charmbracelet/bubbles` | v0.20.0 | textarea, spinner, viewport |
| `github.com/charmbracelet/lipgloss` | v1.0.0 | Layout and styling |
| `github.com/charmbracelet/glamour` | v0.8.0 | Markdown → ANSI |
| `golang.org/x/sync` | v0.10.0 | `errgroup` for parallel tool execution |

### Future dependencies (Phase 3+)

| Package | Purpose |
|---|---|
| `github.com/tiktoken-go/tokenizer` | Accurate token counting |
| `modernc.org/sqlite` | Dream memory persistence (CGO-free SQLite) |
| `github.com/bmatcuk/doublestar/v4` | Alternative `**` glob (or use built-in) |
| `github.com/sergi/go-diff` | Richer unified diff output in `edit_file` |

### Explicitly avoided

| Package | Reason |
|---|---|
| `mattn/go-sqlite3` | Requires CGO |
| `go-git/go-git` | Diverges from native git; subprocess is more correct |
| `JohannesKaufmann/html-to-markdown` | Needed only if inline parser proves insufficient |

---

## 25. Build and Run Reference

```bash
# Fetch dependencies
go mod tidy

# Build (static binary, no CGO)
CGO_ENABLED=0 go build -o drover-code ./cmd/drover-code/

# Interactive TUI
ANTHROPIC_API_KEY=sk-... ./drover-code

# Headless / piped input
echo "explain the auth flow in this codebase" | ANTHROPIC_API_KEY=sk-... ./drover-code

# Coordinator mode (parallel agents)
ANTHROPIC_API_KEY=sk-... DROVER_CODE_COORDINATOR_MODE=1 ./drover-code

# IDE bridge (used by VS Code/JetBrains extension)
ANTHROPIC_API_KEY=sk-... DROVER_CODE_IDE_BRIDGE=1 ./drover-code

# GitHub webhook server
ANTHROPIC_API_KEY=sk-...     \
GITHUB_TOKEN=ghp_...         \
GITHUB_WEBHOOK_SECRET=abc123 \
WEBHOOK_ADDR=:8080           \
./drover-code webhook

# Cross-compile for Linux (from macOS)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o drover-code-linux-amd64 ./cmd/drover-code/

# Cross-compile for ARM (Raspberry Pi, LattePanda Sigma)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o drover-code-linux-arm64 ./cmd/drover-code/
```

### Project settings (`.claude/settings.json`)

```json
{
  "model": "claude-haiku-4-5-20251001",
  "permissionMode": "default",
  "allowedTools": ["read_file", "list_directory", "file_info", "glob", "grep", "git_status", "git_diff", "git_log"],
  "dreamEnabled": true,
  "coordinatorMode": false,
  "undercoverMode": null
}
```

---

*Generated from the drover-code design session — Cloud Shuttle Pty Ltd*
