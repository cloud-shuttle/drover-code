# Drover-Code Architecture Overview

A visual guide to module relationships, data flow, and integration points.

---

## Module Dependency Graph

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                            cmd/drover-code/main.go                          │
│                             (CLI Bootstrap)                                  │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                 ┌──────────────────┼──────────────────┬─────────────────┐
                 ▼                  ▼                  ▼                 ▼
           ┌──────────┐      ┌──────────┐      ┌──────────┐     ┌─────────┐
           │   TUI    │      │Headless  │      │ Bridge   │     │ GitHub  │
           │(Bubble   │      │  Mode    │      │(JSON-RPC)│     │Webhook  │
           │  Tea)    │      │          │      │          │     │         │
           └────┬─────┘      └────┬─────┘      └────┬─────┘     └────┬────┘
                │                 │                 │                │
                │                 │                 │                │
                └─────────────────┴─────────────────┴────────────────┘
                                    │
                ┌───────────────────┼───────────────────┐
                ▼                   ▼                   ▼
          ┌──────────┐        ┌──────────┐       ┌──────────┐
          │ Config   │        │   TUI    │       │Dream(*)  │
          │ Loader   │        │ Events   │       │          │
          │& Markdown│        │& Styling │       │(optional)│
          └──────────┘        └──────────┘       └──────────┘
                │                   │                  │
                └───────────────────┼──────────────────┘
                                    ▼
                    ┌───────────────────────────┐
                    │   Agent Loop              │
                    │  ┌─────────────────────┐  │
                    │  │ • Run user input    │  │
                    │  │ • Stream response   │  │
                    │  │ • Execute tools     │  │
                    │  │ • Emit events       │  │
                    │  └─────────────────────┘  │
                    └──────────────┬────────────┘
                                   │
                ┌──────────────────┼──────────────────┐
                ▼                  ▼                  ▼
          ┌─────────────┐  ┌──────────────┐  ┌──────────────┐
          │Conversation │  │ Tool         │  │API Client    │
          │Manager      │  │ Registry     │  │              │
          │             │  │              │  │• Types       │
          │• Message    │  │• Execute     │  │• HTTP Client │
          │  history    │  │• Permissions │  │• SSE Stream  │
          │• System     │  │• Validation  │  │              │
          │  prompt     │  │              │  │→ Anthropic   │
          │• Tokens     │  │[All Tools]   │  │  Messages API│
          │• Compaction │  │              │  │              │
          └─────────────┘  └──────────────┘  └──────────────┘
                │                  │                  △
                │                  │                  │
                │         ┌────────┴────────┐         │
                │         ▼                 ▼         │
                │    ┌──────────┐      ┌──────────┐  │
                │    │FS Tools  │      │Git Tools │  │
                │    │          │      │          │  │
                │    │• read    │      │• status  │  │
                │    │• write   │      │• diff    │  │
                │    │• edit    │      │• commit  │  │
                │    │• list    │      │• push    │  │
                │    │• info    │      │• create_ │  │
                │    └──────────┘      │  branch  │  │
                │                      └──────────┘  │
                │         ┌────────────────┬─────────┘
                │         ▼                ▼
                │    ┌──────────────┐ ┌─────────┐
                │    │Search Tools  │ │ Web/UKC │
                │    │              │ │         │
                │    │• glob        │ │• fetch  │
                │    │• grep        │ │• ukc_*  │
                │    └──────────────┘ └─────────┘
                │
                ▼
          ┌──────────────┐
          │Permissions   │
          │Engine        │
          │              │
          │• Presets     │
          │• Approval    │
          │  prompts     │
          └──────────────┘
```

---

## Data Flow: User Input to Response

### 1. TUI/Headless → Agent Loop

```
User Input (stdin/TUI)
        │
        ▼
Parse & Validate
        │
        ▼
Append to Conversation (convo.Manager)
        │
        ▼
Format messages with system prompt
        │
        ▼
Agent.Run(userInput)
```

### 2. Agent Loop → API → Stream

```
Agent.Run()
    │
    ├─→ convo.Messages() [snapshot]
    │
    ├─→ client.StreamMessage(messages)
    │       │
    │       ├─→ Build JSON request
    │       │   • system_prompt
    │       │   • messages
    │       │   • tools (registry.Definitions())
    │       │   • model
    │       │
    │       ├─→ POST /v1/messages (HTTP)
    │       │
    │       └─→ SSE Stream reader iterator
    │           • readMessage() loop
    │           • parseEvent()
    │           • Discriminate StreamEvent types
    │
    └─→ Accumulate blocks
        ├─→ TextBlock + TextDelta → strings.Builder
        │
        └─→ ToolUseBlock + InputJSONDelta → per-index accumulator
            • strings.Builder for JSON fragments
            • Concat on ContentBlockStopEvent
            • Finalise as json.RawMessage
```

### 3. Tool Execution

```
Agent detects stop_reason="tool_use"
    │
    ├─→ Extract tool calls from blocks
    │
    ├─→ For each tool call:
    │   │
    │   ├─→ toolName, toolInput (json.RawMessage)
    │   │
    │   ├─→ permissions.IsAllowed(toolName)?
    │   │   ├─→ No: emit ToolDeniedEvent, continue
    │   │   └─→ Yes: proceed
    │   │
    │   ├─→ permissions.NeedsApproval(toolName, input)?
    │   │   ├─→ Yes: emit PermissionPromptEvent
    │   │   │        wait for TUI/bridge approval
    │   │   │        (non-blocking event)
    │   │   │
    │   │   └─→ No: proceed
    │   │
    │   ├─→ registry.Execute(ctx, toolName, input)
    │   │       │
    │   │       ├─→ Look up tool by name
    │   │       │
    │   │       ├─→ tool.Execute(ctx, input)
    │   │       │   ├─→ Parse input (tool-specific unmarshal)
    │   │       │   ├─→ Perform action
    │   │       │   └─→ Return output string
    │   │       │
    │   │       └─→ Catch panics → ToolError
    │   │
    │   ├─→ Emit ToolResultEvent
    │   │
    │   └─→ Collect output as ToolResultBlock
    │
    ├─→ All results collected → ToolResultMessage()
    │
    ├─→ Append to conversation
    │
    └─→ Loop: Agent.Run() again with updated history
```

### 4. Event Emission

```
Agent.Run() (internal event loop)
    │
    ├─→ TextEvent: emit as text streams in
    │
    ├─→ ToolCallEvent: when tool_use block complete
    │
    ├─→ ToolResultEvent: after tool.Execute()
    │
    ├─→ UsageEvent: on MessageDeltaEvent (token counts)
    │
    ├─→ ErrorEvent: on API or tool errors
    │
    └─→ CompleteEvent: when stop_reason="end_turn"
        │
        └─→ Consumer (TUI/headless) reads events from agent.Events() channel
            └─→ Render/output
```

---

## Coordinator Mode: Task Parallelism

```
Coordinator.RunWorkers()
    │
    ├─→ Take snapshot of convo.Manager (immutable)
    │
    ├─→ API call: decompose(prompt) → []Task
    │
    ├─→ For each task:
    │   ├─→ Create new Agent with fresh convo.Manager
    │   │
    │   ├─→ Seed with snapshot context
    │   │
    │   ├─→ Spawn goroutine: agent.Run(taskPrompt)
    │   │
    │   └─→ Collect results
    │
    ├─→ Wait for all workers (sync.WaitGroup)
    │
    ├─→ Merge results back to main conversation
    │
    └─→ Continue with next phase (if any)
```

---

## Dream Memory: Consolidation Cycle

```
Session ends (TUI exit / headless finish)
    │
    ▼
dream.Worker.Trigger(session)
    │ [non-blocking buffered send]
    │
    ├─→ Channel full? Drop silently (memories are nice-to-have)
    │
    └─→ Background goroutine receives
        │
        ├─→ Summarise(session) via API
        │   └─→ Single non-streaming API call
        │
        ├─→ Store.Save(entry)
        │   ├─→ JSON: atomic temp-file + rename
        │   └─→ SQLite: INSERT + COMMIT
        │
        └─→ Store.Prune() if retention limits set
```

### Next Session: Memory Injection

```
Application startup
    │
    ├─→ dream.Store.Recent(5)
    │
    ├─→ Format entries into markdown fragment:
    │   "## Prior Sessions
    │    - [date] refactored auth module
    │    - [date] added test coverage
    │    ..."
    │
    └─→ convo.Manager.SetSystemPrompt(base + injection)
        └─→ Memories available to LLM for context
```

---

## Bridge (IDE): Request-Response Loop

```
IDE Extension
    │
    ├─→ Write LSP frame to stdin
    │   Content-Length: N\r\n
    │   \r\n
    │   {"jsonrpc":"2.0","id":1,"method":"drover/execute","params":{...}}
    │
    └─→ Wait for response
        │
        ▼
Bridge.Run() [goroutine per request]
    │
    ├─→ readMessage() [length-prefixed parser]
    │
    ├─→ parseJSON() into Message
    │
    ├─→ Route to handler (drover/execute)
    │
    ├─→ Handler runs agent.Run() in background
    │
    ├─→ Collect events as they arrive
    │
    ├─→ Send streaming frames back to IDE
    │   Each frame: Content-Length: N\r\n\r\n{event}
    │
    └─→ Final frame includes completion status
```

---

## GitHub Webhook: Event to Agent

```
GitHub user comments "@drover-code refactor auth"
    │
    ├─→ GitHub sends webhook POST
    │   Authorization: HMAC-SHA256(secret, body)
    │
    └─→ webhook.Server.ListenAndServe()
        │
        ├─→ Verify HMAC signature
        │
        ├─→ parser.ParseIssueCommentEvent()
        │
        ├─→ Extract mention: "refactor auth"
        │
        ├─→ github.Runner.Run()
        │   │
        │   ├─→ Clone repo (git clone)
        │   │
        │   ├─→ Create agent session
        │   │   • Set repo context in system prompt
        │   │   • Same agent loop as CLI
        │   │
        │   ├─→ agent.Run("refactor auth")
        │   │
        │   └─→ Collect output
        │
        ├─→ Post result back as comment
        │   Sanitise output → markdown
        │   → POST /repos/{owner}/{repo}/issues/{number}/comments
        │
        └─→ Webhook responds 200 OK
```

---

## UKC Agent: Remote Execution

```
Coordinator (local)
    │
    ├─→ Spin up UKC instance
    │   └─→ ukc_create tool
    │
    ├─→ Upload workspace to worker
    │   POST /workspace/upload
    │   ├─→ tar/gzip project files
    │   └─→ Send to ukc-agent
    │
    ├─→ Execute drover-code on worker
    │   POST /exec
    │   ├─→ Start subprocess: drover-code --headless
    │   ├─→ Stream output via SSE
    │   └─→ Monitor for completion
    │
    ├─→ Download results
    │   GET /workspace/download
    │   ├─→ tar/gzip results
    │   └─→ Receive back
    │
    └─→ Process results in coordinator context
        └─→ Merge into main conversation

ukc-agent (remote, in worker)
    │
    ├─→ Receive workspace upload
    │   ├─→ Extract tar/gzip
    │   └─→ Verify AGENT_TOKEN
    │
    ├─→ Start drover-code subprocess
    │   ├─→ Set environment for headless mode
    │   └─→ Stream all output through SSE channel
    │
    ├─→ Wait for completion
    │   ├─→ Capture exit code
    │   └─→ Prepare result artifacts
    │
    └─→ Allow coordinator to download
        └─→ Stream results as tar/gzip
```

---

## Configuration Cascade

```
Settings load order (first wins):

1. ~/.claude/settings.json         ← user's home directory
2. .claude/settings.json           ← project directory
3. .claude/settings.local.json     ← local dev overrides
4. Environment variables           ← override all (highest priority)

Example resolution:
ANTHROPIC_MODEL env var
    └─→ overrides "model" key in any settings.json
    └─→ overrides --model CLI flag (if conflict)

System Prompt composition:

Base system prompt (hardcoded in agent)
    ↓
+ Markdown instructions (CLAUDE.md files walked up from workDir)
    ↓
+ Dream memories (if enabled)
    ↓
= Final system prompt sent to API
```

---

## Permissions Model: Three Layers

```
                  User Input
                      │
                      ▼
            Preset (default/unikernel/full/readonly)
                 ┌──────────────┐
                 │ Global allow │
                 │ categories:  │
                 │ • read       │
                 │ • write      │
                 │ • system     │
                 │ • git        │
                 │ • web        │
                 └──────────────┘
                      │
                      ▼
         Specific Tool Allowance
            engine.IsAllowed(toolName)?
            ├─→ No → ToolDeniedEvent
            └─→ Yes → continue
                      │
                      ▼
           Tool-specific Permission Check
         tool.NeedsPermission(input)?
         ├─→ No → Execute immediately
         │
         └─→ Yes → Emit PermissionPrompt
                   ├─→ TUI: interactive approval
                   ├─→ Bridge: extension prompts
                   └─→ Headless: timeout → denied
                      │
                      ▼
                   Execute or Deny
```

---

## Testing Strategy: Layers

```
Unit Tests (per module)
├─→ api/types_test.go: message marshalling round-trips
├─→ api/stream_test.go: SSE event parsing
├─→ api/client_test.go: request construction
├─→ agent/loop_test.go: plan-act-observe with fake streams
├─→ convo/manager_test.go: conversation state, snapshots
├─→ tools/*_test.go: tool-specific validation
├─→ permissions/engine_test.go: allowance rules
└─→ coordinator/coordinator_test.go: decomposition logic

Integration Tests
├─→ Agent loop + full tool set + fake API
├─→ TUI event handling + agent interaction
├─→ Coordinator task distribution
├─→ Bridge JSON-RPC round-trip
└─→ GitHub webhook parsing → agent → comment post

Fuzz Tests
├─→ api/stream_fuzz_test.go: malformed SSE
├─→ config/merge_fuzz_test.go: JSON settings merge
├─→ github/parser_fuzz_test.go: webhook payloads
├─→ undercover/undercover_fuzz_test.go: secret detection
├─→ tools/registry_fuzz_test.go: tool lookup
├─→ tui/permission_fuzz_test.go: permission prompts
└─→ convo/convo_fuzz_test.go: compaction edge cases

Property Tests (Haskell-style QC)
├─→ convo_property_test.go: compaction invariants
│   • Messages unchanged after compaction
│   • Tokens ≤ original (compression)
│   • No message loss
│
├─→ registry_property_test.go: registration completeness
│   • All tools reachable by name
│   • No duplicate names
│
├─→ permissions_property_test.go: consistency
│   • IsAllowed + NeedsPermission monotonic
│   • Preset transitions valid
│
└─→ undercover_property_test.go: redaction coverage
    • All secrets removed
    • Valid output remains

Live Evals (opt-in, RUN_AGENT_EVALS=1)
└─→ evals/: real Anthropic API calls
    • Token counting calibration
    • Tool execution integration
    • Multi-turn conversations
    • Error recovery
```

---

## Environment Variable Resolution

```
Variable Lookup (first match wins):

Example: ANTHROPIC_MODEL

1. CLI flag: --model claude-3-5-sonnet-20241022
   └─→ Highest priority

2. Environment: ANTHROPIC_MODEL=claude-3-5-sonnet-...
   └─→ Medium priority

3. .claude/settings.local.json: {"model": "..."}
   └─→ Project-local override

4. .claude/settings.json: {"model": "..."}
   └─→ Project-wide setting

5. ~/.claude/settings.json: {"model": "..."}
   └─→ User-global setting

6. Hardcoded default in code
   └─→ Lowest priority
```

---

## Critical Invariants

### Conversation Manager
- Message history is immutable between snapshots
- RWMutex ensures thread-safe snapshots
- Token count is monotonically increasing
- System prompt never changes mid-session

### Agent Loop
- Tool results always bundled in single user message
- Tool executions serialised (one tool at a time)
- Stop reason determines control flow (not message content)
- Tool input never modified after accumulation complete

### Stream Reader
- Each event processed exactly once
- Events processed in order (SSE guarantees)
- Partial lines never split across events
- Scanner buffer large enough for max tool input

### Permissions
- Allowance checked before approval
- Approval decisions cached only within TUI interaction
- No implicit approval (explicit required)
- Denial is always recoverable (try again)

### Dream Memory
- Non-blocking (trigger channel full → drop silently)
- Consolidation happens after session exit
- Pruning runs immediately after save
- Import migration runs exactly once on first SQLite open

---

*This document describes the current architecture and should be kept in sync with major structural changes.*
