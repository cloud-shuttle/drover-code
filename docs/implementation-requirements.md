# Drover-Code: Complete Implementation Requirements

This document identifies all required modules, APIs, and integration points that need to be implemented to build a complete drover-code system.

---

## 1. CORE API LAYER (`internal/api`)

### 1.1 Types Module (`types.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Discriminated union types using unexported marker methods
- ContentBlock interface with TextBlock, ToolUseBlock, ToolResultBlock
- StreamEvent and Delta discriminated unions
- Message constructors (UserMessage, AssistantMessage, ToolResultMessage)
- ToolDefinition structure for API schema delivery

**Key Exports:**
- `type ContentBlock interface`
- `type StreamEvent interface`
- `type Delta interface`
- `func UserMessage(text string) Message`
- `func AssistantMessage(blocks []ContentBlock) Message`
- `func ToolResultMessage(results []ToolResultBlock) Message`

### 1.2 HTTP Client Module (`client.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Raw HTTP client (not using official SDK)
- Request construction with streaming headers
- Message serialisation (manual marshalling for correctness)
- Error handling and structured errors
- 10-minute timeout for long-running tasks

**Key Exports:**
- `type Client struct`
- `func NewClient(apiKey, baseURL, model string) *Client`
- `func (c *Client) StreamMessage(ctx context.Context, req StreamRequest) (*Stream, error)`
- `func (c *Client) StreamNonStreaming(ctx context.Context, req StreamRequest) (Message, error)`

**Dependencies:**
- HTTP standard library
- JSON marshalling/unmarshalling
- Context for cancellation

### 1.3 SSE Stream Reader (`stream.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Parse server-sent events from Anthropic API
- Iterator pattern: `Next()`, `Event()`, `Err()`, `Close()`
- Event type parsing and discrimination
- Backpressure support through iterator
- Wire-format to typed conversion
- Configurable scanner buffer for large inputs

**Key Exports:**
- `type Stream struct`
- `func (s *Stream) Next() bool`
- `func (s *Stream) Event() StreamEvent`
- `func (s *Stream) Err() error`
- `func (s *Stream) Close() error`

---

## 2. TOOL SYSTEM (`internal/tools`)

### 2.1 Tool Interface and Registry (`registry.go`, `register.go`)
**Status:** ✅ IMPLEMENTED

**Tool Interface:**
```go
type Tool interface {
    Name() string
    Description() string
    InputSchema() json.RawMessage
    NeedsPermission(input json.RawMessage) bool
    Execute(ctx context.Context, input json.RawMessage) (string, error)
}
```

**Registry Responsibilities:**
- Collect and index tools by name
- Generate ToolDefinition list for API
- Dispatch tool calls to implementations
- Goroutine-safe concurrent execution

**Key Exports:**
- `type Registry struct`
- `func NewRegistry() *Registry`
- `func (r *Registry) Register(t Tool)`
- `func (r *Registry) Definitions() []api.ToolDefinition`
- `func (r *Registry) Execute(ctx context.Context, name string, input json.RawMessage) (string, error)`
- `func RegisterAll(r *Registry, workDir string)`

### 2.2 Tool Utility Module (`toolutil/util.go`)
**Status:** ⚠️ PARTIAL (needs schema builder enhancements)

**Responsibilities:**
- JSON Schema builder for tool inputs
- Common parameter types
- Input validation helpers
- Error formatting utilities

**Key Components:**
- `Schema` builder with fluent API
- Property type helpers (string, number, boolean, object, array)
- Required/optional field handling
- Description injection

### 2.3 Implemented Tools

#### File System Tools (`fs/`)
**Status:** ✅ IMPLEMENTED

| Tool | Type | File |
|------|------|------|
| `read_file` | Read-only | fs/read.go |
| `write_file` | Write | fs/write.go |
| `edit_file` | Write | fs/edit.go |
| `list_directory` | Read-only | fs/list.go |
| `file_info` | Read-only | fs/info.go |

#### Shell Tools (`shell/`)
**Status:** ✅ IMPLEMENTED

| Tool | Type | File |
|------|------|------|
| `bash` | System | shell/bash.go |

#### Search Tools (`search/`)
**Status:** ✅ IMPLEMENTED

| Tool | Type | File |
|------|------|------|
| `glob` | Read-only | search/glob.go |
| `grep` | Read-only | search/grep.go |

#### Git Tools (`git/`)
**Status:** ✅ IMPLEMENTED

| Tool | Type | File |
|------|------|------|
| `git_status` | Read-only | git/status.go |
| `git_diff` | Read-only | git/diff.go |
| `git_log` | Read-only | git/log.go |
| `git_add` | Write | git/add.go |
| `git_commit` | Write | git/commit.go |
| `git_push` | Write | git/push.go |
| `git_create_branch` | Write | git/branch.go |

#### Web Tools (`web/`)
**Status:** ✅ IMPLEMENTED

| Tool | Type | File |
|------|------|------|
| `web_fetch` | Read-only | web/fetch.go |

#### Unikraft Cloud Tools (`ukc/`)
**Status:** ✅ IMPLEMENTED

| Tool | Type | File |
|------|------|------|
| `ukc_create` | System | ukc/tool_create.go |
| `ukc_exec` | System | ukc/tool_exec.go |
| `ukc_delete` | System | ukc/tool_delete.go |
| `ukc_delete_all` | System | ukc/tool_delete_all.go |
| `ukc_list` | Read-only | ukc/tool_list.go |

---

## 3. AGENT LOOP (`internal/agent` + `internal/convo`)

### 3.1 Conversation Manager (`convo/manager.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Maintain message history as immutable snapshots
- System prompt management
- Token budget estimation (heuristic-based)
- Thread-safe RWMutex for concurrent access
- Message appending with validation

**Key Exports:**
- `type Manager struct`
- `func NewManager(systemPrompt string, estimator TokenEstimator) *Manager`
- `func (m *Manager) Messages() []api.Message` (snapshot copy)
- `func (m *Manager) Append(msg api.Message) error`
- `func (m *Manager) EstimatedTokens() int`
- `func (m *Manager) EstimatedTokenUsage() TokenUsage`

**Key Features:**
- Automatic context compaction when soft limit exceeded
- Snapshot copies prevent racing on concurrent access
- Atomic message additions

### 3.2 Agent Loop (`agent/loop.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Plan → Act → Observe cycle
- Stream response and accumulate blocks
- Tool call execution and result injection
- Error handling and retry logic
- Event emission for UI/observer consumption

**Agent State Machine:**
1. Send user message to API
2. Stream and accumulate response blocks
3. If `stop_reason == "tool_use"`:
   - Extract tool calls
   - Execute each tool
   - Inject results as user message
   - Repeat from step 1
4. If `stop_reason == "end_turn"`:
   - Emit final response event
   - Return

**Key Exports:**
- `type Agent struct`
- `func NewAgent(client *api.Client, convo *convo.Manager, tools *tools.Registry) *Agent`
- `func (a *Agent) Run(ctx context.Context, userInput string) error`
- `func (a *Agent) Events() <-chan Event` (event stream)

**Event Types:**
- `TokenUsageEvent` — API token consumption
- `ToolCallEvent` — Tool invocation with name/input
- `ToolResultEvent` — Tool output and status
- `TextEvent` — Streamed text from API
- `ErrorEvent` — Fatal errors
- `CompleteEvent` — Turn finished

### 3.3 Event System (`agent/events.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Define event types for UI/coordinator consumption
- Serializable event hierarchy
- Event metadata (timestamps, sequence numbers)

**Key Types:**
```go
type Event interface {
    Type() string  // "text", "tool_call", "tool_result", "usage", "error", "complete"
    Timestamp() time.Time
}

// Concrete types: TextEvent, ToolCallEvent, ToolResultEvent, UsageEvent, ErrorEvent, CompleteEvent
```

### 3.4 Agent Errors (`agent/errors.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Structured error types
- Error categorisation (retryable vs permanent)
- Error wrapping with context

**Key Types:**
- `UserError` — Message parsing/validation failures
- `ToolError` — Tool execution failures (may be retryable)
- `APIError` — API communication failures (may be retryable)

---

## 4. CONFIGURATION SYSTEM (`internal/config`)

### 4.1 Configuration Loader (`loader.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Load settings from `~/.claude/settings.json` → `<workdir>/.claude/settings.json`
- Load local override from `.claude/settings.local.json`
- Merge cascade (system → project → local)
- Environment variable overrides

**Key Exports:**
- `func Load(workDir string) (Settings, error)`
- `type Settings struct` with model, timeouts, permission presets, dream config

### 4.2 Runtime Application (`apply_runtime.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Apply settings to client (model, base URL)
- Apply settings to agent/convo (context limits, compaction heuristics)
- Environment variable precedence

**Key Functions:**
- `func ApplyClientSettings(c *api.Client, s Settings) error`
- `func ApplyAgentSettings(a *Agent, s Settings) error`

### 4.3 Markdown Injection (`markdown_glob.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Recursively find CLAUDE.md files in working directory tree
- Walk upward from workDir to project root
- Concatenate markdown into system prompt fragment
- Inject into base system prompt

**Key Functions:**
- `func GlobMarkdownInstructions(workDir string) (string, error)`

---

## 5. CONVERSATION COMPACTION (`internal/convo` - compaction subsystem)

### 5.1 Compaction Engine (`convo/compaction_cut.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Identify oldest conversational "chunk" (user → assistant → tools → results)
- Select compaction candidate when context limit approaching
- Implement summarisation via API callback

**Key Functions:**
- `func (m *Manager) CompactionCandidate() ([]api.Message, error)`
- `func (m *Manager) ReplaceCompacted(old []api.Message, summary api.Message) error`

### 5.2 Token Calibration (`convo/calibration.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Heuristic token estimation (characters ÷ 4 by default)
- Calibration against real API responses
- Per-model tuning (B2 heuristic)

**Key Functions:**
- `func EstimateTokens(text string) int`
- `func CalibrateFromResponse(estimatedBefore int, actualAfter int) float64`

---

## 6. PERMISSIONS ENGINE (`internal/permissions`)

### 6.1 Permission Rules (`engine.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Define permission categories (read, write, system, git, web, dangerous)
- Tool-to-category mapping
- Check before tool execution

**Key Functions:**
- `func (e *Engine) IsAllowed(toolName string) bool`
- `func (e *Engine) NeedsApproval(toolName string, input json.RawMessage) bool`

### 6.2 Permission Presets (`preset.go`)
**Status:** ✅ IMPLEMENTED

**Presets:**
- `default` — allow read + git read/write, require approval for system/web
- `unikernel` — minimal headless preset (read/write + safe shell)
- `full` — unrestricted (development only)
- `readonly` — only read operations

---

## 7. DREAM MEMORY SYSTEM (`internal/dream`)

### 7.1 Dream Worker (`dream.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Non-blocking consolidation of sessions into memories
- Background goroutine for summarisation
- Safe buffered trigger channel (size 8, silent drop if full)

**Key Types:**
- `type Worker struct`
- `func NewWorker(store Store, client API) *Worker`
- `func (w *Worker) Trigger(s Session)` (non-blocking)
- `func (w *Worker) Start(ctx context.Context)`

### 7.2 Store Interface and Implementations

#### JSON Store (`dream.go`)
**Status:** ✅ IMPLEMENTED

**Features:**
- Default backend
- Single `.claude/memory.json` file
- Atomic writes via temp-file + rename
- O(n) loading for recent entries

#### SQLite Store (`sqlite_store.go`)
**Status:** ✅ IMPLEMENTED

**Features:**
- Opt-in via `DROVER_CODE_DREAM_BACKEND=sqlite`
- `.claude/memory.db` (modernc.org/sqlite, pure Go)
- Indexed recency queries
- Auto-import from existing memory.json

### 7.3 Retention Engine (`retention.go`)
**Status:** ✅ IMPLEMENTED

**Features:**
- Optional max entry count (settings or env)
- Optional max age in days
- Automatic pruning after consolidation save
- Startup pruning if limits are set

---

## 8. COORDINATOR MODE (`internal/coordinator`)

### 8.1 Multi-Worker Coordinator (`coordinator.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Decompose task into parallel subtasks
- Spawn worker agents with shared context
- Coordinate and merge results
- Handle partial failures

**Key Functions:**
- `func NewCoordinator(convo *convo.Manager, tools *tools.Registry) *Coordinator`
- `func (c *Coordinator) Decompose(ctx context.Context, prompt string) ([]Task, error)`
- `func (c *Coordinator) RunWorkers(ctx context.Context, tasks []Task) ([]Result, error)`

**Decomposition Examples:**
- "Write tests for all uncovered files" → one worker per file
- "Refactor auth module" → one worker per component
- "Update all dependencies" → one worker per dependency

---

## 9. IDE BRIDGE (`internal/bridge`)

### 9.1 JSON-RPC Bridge (`bridge.go`)
**Status:** ✅ IMPLEMENTED

**Protocol:**
- LSP wire format (Content-Length + JSON-RPC 2.0)
- Bidirectional message handling
- Concurrent request handling on goroutines
- Response routing via pending map

**Key Exports:**
- `type Bridge struct`
- `func NewBridge(r io.Reader, w io.Writer) *Bridge`
- `func (b *Bridge) Register(method string, h Handler)`
- `func (b *Bridge) Run(ctx context.Context) error`
- `func (b *Bridge) Send(id int64, method string, params any) error`
- `func (b *Bridge) SendError(id int64, code int, message string) error`

**Registered Methods (minimal):**
- `drover/execute` — execute agent with input prompt
- `drover/cancel` — cancel in-flight execution
- `ping` — connection check

---

## 10. GITHUB WEBHOOK INTEGRATION (`internal/github`)

### 10.1 Webhook Server (`server.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- HTTP server listening for GitHub push/issue_comment events
- HMAC-SHA256 signature verification
- Thread-safe event routing

**Key Functions:**
- `func NewServer(port int, secret string, handler EventHandler) *Server`
- `func (s *Server) Start() error`
- `func (s *Server) Close() error`

### 10.2 Event Parser (`parser.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Parse GitHub webhook payloads
- Extract `@drover-code` mentions from comments
- Build execution context (repo, file, branch)

**Key Functions:**
- `func ParseIssueCommentEvent(payload []byte) (Event, error)`
- `func ExtractDroverMentions(comment string) ([]Mention, error)`

### 10.3 Event Runner (`runner.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Clone/fetch repo
- Create agent session with GitHub context
- Post results back as comment
- Handle authentication and permissions

**Key Functions:**
- `func NewRunner(client *github.Client, token string, workDir string) *Runner`
- `func (r *Runner) Run(ctx context.Context, ev Event) error`
- `func (r *Runner) PostComment(ctx context.Context, repoURL, number string, body string) error`

### 10.4 GitHub Client Wrapper (`client.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Thin wrapper around GitHub API
- Post/update comments
- Clone repo via git
- Fetch branch updates

---

## 11. TUI (Terminal UI) (`internal/tui`)

### 11.1 Bubble Tea Model (`model.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Bubble Tea model implementation
- Message routing (from stdin, agent events)
- State management (input buffer, conversation history, view mode)

**Key Types:**
- `type Model struct` (implements tea.Model)
- `func NewModel(convo *convo.Manager, agent *agent.Agent) *Model`
- `func (m *Model) Init() tea.Cmd`
- `func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd)`
- `func (m *Model) View() string`

### 11.2 View Rendering (`view.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Render conversation history
- Render input prompt area
- Render status bar (mode, tokens, model)
- Render help text

**Key Functions:**
- `func (m *Model) renderHistory() string`
- `func (m *Model) renderInput() string`
- `func (m *Model) renderStatus() string`

### 11.3 Permission Prompt (`permission.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Interactive permission approval for sensitive tools
- Tool description display
- Input/output preview
- Approval/denial UI

**Key Functions:**
- `func (m *Model) promptPermission(tool string, input json.RawMessage) bool`

### 11.4 Styling (`styles.go`)
**Status:** ✅ IMPLEMENTED

**Provides:**
- Lipgloss style definitions
- Color palette
- Component styling (input, status, history, etc.)

### 11.5 Program Launcher (`program.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Initialize and run Bubble Tea program
- Handle terminal setup/teardown
- Signal handling (Ctrl+C)

**Key Functions:**
- `func Run(ctx context.Context, m *Model) error`

---

## 12. HEADLESS MODE (`cmd/drover-code`)

### 12.1 Headless Output Formatter
**Status:** ✅ IMPLEMENTED

**Formats:**
- Plain text (default)
- JSON Lines (DROVER_CODE_JSONL=1)
- Structured result JSON (--result-json PATH)

**Key Features:**
- Line-based output for piping
- Machine-readable event serialisation
- Final artifact output

---

## 13. WEBHOOK COMMAND (`cmd/drover-code webhook`)

**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Parse webhook flags
- Start GitHub webhook server
- Route events to runner
- Keep process alive

**Flags:**
- `--port` (default: 8080)
- `--secret` (GitHub webhook secret)
- `--work-dir` (default: current dir)

---

## 14. CUSTOM COMMANDS SYSTEM (`internal/commands`)

**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Load command definitions from Markdown (frontmatter) and JSON configuration
- Expand variables (`$1`, `$ARGUMENTS`), placeholders (`{var}`), file contents (`@file.txt`), and inline shell execution (``!`cmd` ``)
- Evaluate operations through `guardclient` for risk and policy governance
- Provide an `Executor` for TUI and Headless environments to evaluate custom inputs

**Key Modules:**
- `types.go`: `CommandDefinition` and `CommandRegistry`
- `parser.go`: Frontmatter and JSON parsing
- `loader.go`: Directory traversal and configuration injection
- `expander.go`: Regex-based prompt expansion
- `executor.go`: Integration of template expansion and Drover Guard evaluation

---

## 14. UNIKRAFT CLOUD AGENT (`cmd/ukc-agent`)

### 14.1 Workspace Sync Endpoint (`/workspace`)
**Status:** ⚠️ PARTIAL

**Responsibilities:**
- Accept uploaded workspace tar/zip
- Extract to worker filesystem
- Prepare for drover-code execution
- Upload results back to coordinator

**Endpoints:**
- `POST /workspace/upload` — receive and extract
- `GET /workspace/download` — stream results back

### 14.2 Execution Endpoint (`/exec`)
**Status:** ⚠️ PARTIAL

**Responsibilities:**
- Execute drover-code subprocess
- Stream tool execution logs via SSE
- Report completion/failure

**Endpoints:**
- `POST /exec` — start execution with config
- `GET /exec/:id` — stream output

---

## 15. TELEMETRY (`internal/telemetry`)

### 15.1 Context and Logging (`context.go`)
**Status:** ✅ IMPLEMENTED

**Features:**
- Request ID tracing
- User ID tracking
- Structured logging

### 15.2 Langfuse Integration (`langfuse.go`)
**Status:** ⚠️ PARTIAL

**Responsibilities:**
- Optional Langfuse trace export
- Trace parent/child relationships
- Token usage reporting

---

## 16. UNDERCOVER MODE (`internal/undercover`)

### 16.1 Sensitive Output Detection (`undercover.go`)
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Detect API keys, tokens, passwords in output
- Redact before display
- Warning system

**Key Functions:**
- `func Detect(text string) []Finding`
- `func (f Finding) Redact(text string) string`

---

## 17. COMMAND ENTRY POINT (`cmd/drover-code/main.go`)

### 17.1 CLI Bootstrap
**Status:** ✅ IMPLEMENTED

**Responsibilities:**
- Flag parsing
- Mode detection (TUI, headless, webhook, bridge)
- Context setup (config, API client, agent)
- Mode-specific execution

**Flags/Modes:**
- TUI: default (no flags)
- Headless: non-TTY stdin or `DROVER_CODE_HEADLESS=1`
- Webhook: `./drover-code webhook`
- IDE Bridge: `CLAUDE_CODE_IDE_BRIDGE=1`
- Coordinator: `CLAUDE_CODE_COORDINATOR_MODE=1`

---

## INTEGRATION POINTS MATRIX

| Component | Depends On | Depended On By |
|-----------|-----------|---|
| `api` (types, client, stream) | (none - foundation) | All |
| `tools` (registry, toolutil) | `api` | `agent`, `coordinator` |
| `agent` (loop, events) | `api`, `tools`, `convo` | `tui`, `coordinator`, `bridge`, `github`, `cmd` |
| `convo` (manager, compaction) | `api` | `agent`, `coordinator`, `dream` |
| `config` (loader, apply, markdown) | (stdlib) | `cmd` |
| `permissions` (engine, presets) | (stdlib) | `tui`, `agent` |
| `dream` (worker, stores, retention) | `api`, `convo` | `cmd` |
| `coordinator` | `api`, `agent`, `convo`, `tools` | `cmd` |
| `bridge` (JSON-RPC) | `agent`, `api`, `tools` | `cmd` |
| `github` (server, parser, runner) | `api`, `agent`, `tools`, `config` | `cmd` |
| `tui` (Bubble Tea) | `agent`, `convo`, `permissions`, `config` | `cmd` |
| `telemetry` (context, langfuse) | (stdlib) | `cmd`, optional in various packages |
| `undercover` (detection, redaction) | (stdlib) | `tui`, `bridge` |
| `ukc-agent` | `api`, REST framework | standalone server |

---

## CRITICAL IMPLEMENTATION DETAILS

### API Client: Message Marshalling
- Manual JSON marshalling (not struct tags) to handle discriminated unions
- `json.RawMessage` for tool input round-trip fidelity
- Type field injection at serialisation time

### Tool Interface
- Five methods: `Name()`, `Description()`, `InputSchema()`, `NeedsPermission()`, `Execute()`
- Goroutine-safe (coordinator calls tools concurrently)
- `json.RawMessage` for deferred parsing

### Agent Loop
- Strict plan → act → observe cycle
- Accumulator for tool input fragments (strings.Builder for efficiency)
- Per-index maps for concurrent block assembly
- Error differentiation (retryable vs permanent)

### Conversation Manager
- RWMutex for thread-safe snapshots
- Snapshot copies prevent racing
- Token estimation + automatic compaction

### Dream Memory
- Non-blocking trigger channel (8-size buffer)
- Background consolidation goroutine
- Two backend implementations (JSON, SQLite)
- Retention caps with pruning

### Coordinator
- Task decomposition via API
- Worker isolation with snapshot contexts
- Result merging and failure handling

### Bridge (JSON-RPC)
- Content-Length framing
- Bidirectional message routing
- Goroutine-per-request handlers
- Pending map for response correlation

### GitHub Webhook
- HMAC-SHA256 signature verification
- `@drover-code` mention extraction
- Inline comment posting
- Full repo interaction via git + agent

---

## MISSING/INCOMPLETE IMPLEMENTATIONS

1. **UKC Agent (`cmd/ukc-agent`)**
   - Workspace upload/download endpoints need full implementation
   - Execution streaming needs SSE integration

2. **Langfuse Telemetry**
   - Optional integration needs completion
   - Trace export and parent/child relationships

3. **Bridge IDE Extensions**
   - VS Code extension stub exists but needs full implementation
   - JetBrains plugin not yet started

4. **Batch API Integration**
   - Future optimization for non-streaming calls (dream consolidation)
   - Not yet implemented but documented

5. **Retry/Backoff Logic**
   - Currently relies on caller retries
   - Could benefit from RetryingClient wrapper for 429/529 responses

6. **Real Token Counting**
   - Currently uses character heuristic (÷4)
   - Proper tokenizer integration could be added

---

## BUILD TARGETS

```bash
# Main binary (single static binary)
CGO_ENABLED=0 go build -o drover-code ./cmd/drover-code

# UKC agent (optional deployment)
CGO_ENABLED=0 go build -o ukc-agent ./cmd/ukc-agent

# Tests (all packages)
CGO_ENABLED=0 go test ./...

# Fuzz targets (see .github/workflows/ci.yml)
go test -fuzz=. -fuzztime=10s ./...
```

---

## ENVIRONMENT VARIABLES

### Core API
- `ANTHROPIC_API_KEY` — required
- `ANTHROPIC_AUTH_TOKEN` — alternative key format
- `ANTHROPIC_BASE_URL` — optional gateway override
- `ANTHROPIC_MODEL` — override default model

### Modes
- `DROVER_CODE_HEADLESS=1` — force headless mode
- `CLAUDE_CODE_IDE_BRIDGE=1` — enable IDE bridge
- `CLAUDE_CODE_COORDINATOR_MODE=1` — enable coordinator mode
- `GITHUB_TOKEN` — for webhook mode

### Permissions
- `DROVER_CODE_PERMISSION_PRESET=` — permission set (default, unikernel, full, readonly)

### Dream Memory
- `DROVER_CODE_DREAM_BACKEND=sqlite` — use SQLite instead of JSON
- `DROVER_CODE_DREAM_SKIP_JSON_IMPORT=1` — skip JSON→SQLite migration
- `DROVER_CODE_DREAM_MAX_ENTRIES=` — max memory entries (0 = unlimited)
- `DROVER_CODE_DREAM_MAX_AGE_DAYS=` — max memory age (0 = unlimited)

### Output
- `DROVER_CODE_JSONL=1` — force JSON Lines output
- `DROVER_CODE_HEADLESS_PLAIN=1` — force plain text in headless mode
- `DROVER_CODE_RESULT_PATH=` — write final result JSON to file

### Development
- `RUN_AGENT_EVALS=1` — run integration eval tests (requires API key)

---

## TESTING STRATEGY

### Unit Tests (per module)
- `types_test.go` — message construction, marshalling round-trips
- `stream_test.go` — SSE parsing, event accumulation
- `client_test.go` — request construction, error handling
- Each tool has `_test.go` for schema validation and execution

### Integration Tests
- Agent loop with fake streams
- Full conversation flow with mock tools
- Coordinator task decomposition
- Bridge JSON-RPC message routing

### Fuzz Tests
- Stream parsing (`stream_fuzz_test.go`)
- Config merge (`merge_fuzz_test.go`)
- GitHub parser (`parser_fuzz_test.go`)
- Undercover detection (`undercover_fuzz_test.go`)
- Registry (`registry_fuzz_test.go`)
- Permissions (`permission_fuzz_test.go`)
- Convo compaction (`convo_fuzz_test.go`)

### Property Tests
- Conversation compaction invariants (`convo_property_test.go`)
- Registry completeness (`registry_property_test.go`)
- Permissions engine consistency (`engine_property_test.go`)
- Undercover redaction coverage (`undercover_property_test.go`)

### Eval Tests (opt-in)
- Live agent tasks against real API (see `evals/`)
- Token counting calibration
- Tool integration validation

---

*Last updated: [Current Date]*
*This is a reference document; specific implementation status may change.*
