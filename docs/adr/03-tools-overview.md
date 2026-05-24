# 03 — Tools Overview: Interface, Registry, and toolutil

**Packages:** `internal/tools`, `internal/tools/toolutil`  
**Files:** `tools/registry.go`, `tools/register.go`, `tools/toolutil/util.go`  
**Depends on:** `internal/api`  
**Depended on by:** `internal/agent`, `internal/coordinator`, all tool sub-packages

---

## Purpose

This layer defines the contract every tool must honour, the registry that
collects them and presents them to the API and the agent loop, and the shared
utilities every tool implementation reaches for. It is a narrow but load-bearing
layer — if the interface is wrong, every tool implementation pays for it.

Three design pressures shaped these decisions:

1. **Goroutine safety.** Coordinator mode runs multiple worker agents
   concurrently, each of which may call the same tool simultaneously. Every
   tool must be safe to call from multiple goroutines without external
   synchronisation.

2. **Testability.** The agent loop should be testable with a mock registry
   that doesn't touch the filesystem, spawn subprocesses, or make network
   calls. The interface must be narrow enough that mocking is cheap.

3. **Extensibility.** Adding the 40th tool should not require changes to the
   agent loop, the registry, or any existing tool. Registration is the only
   coupling point.

---

## 1. The `Tool` Interface

```go
type Tool interface {
    Name()            string
    Description()     string
    InputSchema()     json.RawMessage
    NeedsPermission(input json.RawMessage) bool
    Execute(ctx context.Context, input json.RawMessage) (string, error)
}
```

Five methods. Every design decision here was made by asking: "what is the
minimum information the agent loop needs from a tool?"

### 1.1 `Name() string`

The name is the exact string the model uses in a `tool_use` content block.
It must be stable across versions — changing a tool name breaks any
conversation history that contains that tool call (the `tool_use_id`
correlation would still work but the model would produce the old name).

Convention: snake_case, verb-first where possible (`read_file`, `write_file`,
`git_status`). This matches the Claude Code convention and gives the model
a consistent pattern to learn.

The name is also the registry key. Two tools with the same name cause a panic
at startup — this is a programming error, not a runtime error.

### 1.2 `Description() string`

Sent verbatim to the Anthropic API as the tool description. The model uses
this to decide when to call the tool and how to use it. Writing good
descriptions is one of the highest-leverage things you can do for tool
quality.

Guidelines for tool descriptions:
- Write from the model's perspective, not the implementer's
- Describe the primary use case first, edge cases second
- State explicitly what the tool does NOT do
- Mention performance characteristics when relevant
  (`Returns up to 1000 matches` prevents the model from expecting exhaustive results)
- Mention when to prefer this tool over similar ones
  (`For targeted changes to existing files, prefer edit_file`)

### 1.3 `InputSchema() json.RawMessage`

Returns a JSON Schema object sent to the API alongside the description. The API
uses this to validate the model's tool calls and to guide the model's
parameter construction. It is called once at registry initialisation time
(when building `Definitions()`) and the result is cached by the registry.

The schema is expressed as `json.RawMessage` rather than a typed struct for
two reasons:

**JSON Schema is itself a schema language.** Representing it as a Go type
hierarchy would duplicate effort already done by the JSON Schema specification.
A raw JSON object is the correct representation.

**Toolutil's `Schema` builder is enough.** The schemas are simple — they rarely
go deeper than two levels of nesting. The builder is more readable than hand-
writing JSON strings and more explicit than reflection-based approaches:

```go
// Clear, explicit, no surprises:
toolutil.NewSchema("object").
    Prop("path",    toolutil.NewSchema("string").Desc("File path")).
    Prop("content", toolutil.NewSchema("string").Desc("Content to write")).
    Required("path", "content").
    Build()

// vs reflection-based (hides the schema from the reader):
jsonschema.Reflect(&WriteFileInput{})
```

The `Build()` call marshals the schema once; the result is returned on every
`InputSchema()` call. Tools should compute their schema once (e.g. as a
`var` package-level value) rather than rebuilding it on every call.

### 1.4 `NeedsPermission(input json.RawMessage) bool`

The permission decision is **per-call**, not per-tool. The same tool might need
permission for some inputs and not others.

The primary use case is the `bash` tool: a read-only command like `ls` or
`cat` is arguably safe to auto-approve; a destructive command like `rm -rf`
absolutely is not. The implementation can inspect the `input` JSON to make
an input-aware decision.

In practice, most tools take a simple approach:

```go
// Always requires permission — any file write is irreversible
func (t *WriteFile) NeedsPermission(_ json.RawMessage) bool { return true }

// Never requires permission — reading is always safe
func (t *ReadFile) NeedsPermission(_ json.RawMessage) bool { return false }

// Always requires permission — bash can do anything
func (t *Bash) NeedsPermission(_ json.RawMessage) bool { return true }
```

A future refinement for `bash` would inspect the command string:

```go
func (t *Bash) NeedsPermission(input json.RawMessage) bool {
    var inp struct{ Command string `json:"command"` }
    json.Unmarshal(input, &inp)
    return !isReadOnlyCommand(inp.Command)
}

func isReadOnlyCommand(cmd string) bool {
    safePatterns := []string{"ls ", "cat ", "echo ", "pwd", "which ", "grep "}
    for _, p := range safePatterns {
        if strings.HasPrefix(cmd, p) { return true }
    }
    return false
}
```

This is deliberately not implemented yet — the pattern matching is fragile
(a user could write `echo $(rm -rf /)`) and gives false confidence. The
`plan` permission mode (auto-approve reads, gate writes) is a better
architectural answer to this problem.

### 1.5 `Execute(ctx context.Context, input json.RawMessage) (string, error)`

The output is always a plain string. This is a deliberate simplification —
the Anthropic API accepts structured content in `tool_result` blocks, but
plain text is sufficient for all current use cases and is much simpler for
the model to reason about than nested JSON.

The string returned to the model should be:
- Human-readable (the model is trained on human text)
- Self-describing (include the command that was run, paths involved)
- Bounded (use `toolutil.Truncate` for outputs that could be large)
- Honest about errors (include the full error message, not just "failed")

The `context.Context` parameter is critical for two reasons:

**Cancellation.** If the user presses Ctrl+C while a tool is running, the
context is cancelled. Tools must respect this — `bash` uses
`exec.CommandContext`, file operations should check `ctx.Err()` between
chunks, network calls should use `http.NewRequestWithContext`.

**Timeout.** The context may carry a deadline (the `bash` tool sets its own
per-call timeout, but outer timeouts from the webhook server's job context
also apply). Tools that don't respect `ctx` can hang indefinitely.

---

## 2. The Registry

### 2.1 Structure

```go
type Registry struct {
    mu    sync.RWMutex
    tools map[string]Tool
}
```

The registry is a concurrent map. `sync.RWMutex` allows many readers
concurrently — the `Execute` and `NeedsPermission` methods acquire a read
lock only long enough to look up the tool, then release it before calling
into the tool. The tool itself handles its own concurrency.

### 2.2 Registration

```go
func (r *Registry) Register(t Tool) {
    r.mu.Lock()
    defer r.mu.Unlock()
    if _, exists := r.tools[t.Name()]; exists {
        panic(fmt.Sprintf("tools: duplicate registration for %q", t.Name()))
    }
    r.tools[t.Name()] = t
}
```

The panic on duplicate registration is intentional. Silently ignoring a
duplicate (first wins or last wins) would hide bugs where two packages both
try to register `bash`. A panic at startup is far more visible than a
subtle behaviour change.

Registration happens at startup in `tools/register.go`:

```go
func RegisterAll(r *Registry, workDir string) {
    r.Register(&fs.ReadFile{WorkDir: workDir})
    r.Register(&fs.WriteFile{WorkDir: workDir})
    // ...
}
```

Each tool receives `workDir` at registration time. This means tools don't
need to look up the working directory on every call — it's baked in at
construction. If the user `cd`s during a session... they can't, because
drover-code captures `os.Getwd()` at startup and all tools use that snapshot.

### 2.3 `Definitions()` — the API contract

```go
func (r *Registry) Definitions() []api.ToolDefinition {
    r.mu.RLock()
    defer r.mu.RUnlock()
    defs := make([]api.ToolDefinition, 0, len(r.tools))
    for _, t := range r.tools {
        defs = append(defs, api.ToolDefinition{
            Name:        t.Name(),
            Description: t.Description(),
            InputSchema: t.InputSchema(),
        })
    }
    return defs
}
```

The order of definitions is non-deterministic (Go map iteration). This is
acceptable — the model selects tools by name, not by position. If deterministic
ordering becomes important (e.g. for reproducible test snapshots), sort by
name before returning.

The definitions slice is computed fresh on every call to `Definitions()`.
This is called once per agent turn (once per `StreamMessage` call). The cost
is one `RLock`, one slice allocation, and N string copies — negligible.

An optimisation would cache the definitions and invalidate on registration.
Since all registration happens at startup and no tools are registered or
deregistered during a session, this optimisation is never needed in practice.

### 2.4 Dispatch

```go
func (r *Registry) Execute(ctx context.Context, name string, input json.RawMessage) (string, error) {
    r.mu.RLock()
    t, ok := r.tools[name]
    r.mu.RUnlock()

    if !ok {
        return "", fmt.Errorf("unknown tool %q", name)
    }
    return t.Execute(ctx, input)
}
```

The lock is released before calling `t.Execute()`. This is the key
concurrency design: the registry is only locked during the map lookup, not
during tool execution. If the registry held a read lock for the duration of
`Execute()`, concurrent tool calls would be sequentialised by the lock.

The "unknown tool" error path is important. If the model hallucinates a tool
name (e.g. `delete_file` which doesn't exist), the error is returned as a
`tool_result` with `is_error: true`. The model will typically respond by
acknowledging its mistake and using the correct tool name.

### 2.5 Mock registry for testing

```go
type MockTool struct {
    name         string
    executeFunc  func(ctx context.Context, input json.RawMessage) (string, error)
    needsPerm    bool
}

func (m *MockTool) Name() string           { return m.name }
func (m *MockTool) Description() string    { return "mock" }
func (m *MockTool) InputSchema() json.RawMessage { return []byte(`{"type":"object"}`) }
func (m *MockTool) NeedsPermission(_ json.RawMessage) bool { return m.needsPerm }
func (m *MockTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
    return m.executeFunc(ctx, input)
}
```

Create a registry with mock tools for testing the agent loop without any
real file system or subprocess access. This is the primary reason we use
constructor injection rather than package-level singletons.

---

## 3. The Permission Contract

### 3.1 `PermissionFunc`

```go
type PermissionFunc func(ctx context.Context, req PermissionRequest) Decision

type PermissionRequest struct {
    ToolName string
    Input    json.RawMessage
    Summary  string         // human-readable, pre-computed by the loop
}

type Decision int
const (
    Allow       Decision = iota
    AlwaysAllow
    Deny
)
```

`PermissionFunc` is a function type, not an interface. This is a deliberate
choice: function types are easier to compose and inject than single-method
interfaces.

```go
// The TUI's permission function (simplified):
permitFn := func(ctx context.Context, req tools.PermissionRequest) tools.Decision {
    respCh := make(chan agent.PermissionDecision, 1)
    eventCh <- agent.PermissionRequestEvent{..., DecisionCh: respCh}
    select {
    case d := <-respCh:
        return mapDecision(d)
    case <-ctx.Done():
        return tools.Deny
    }
}

// Headless auto-approve:
tools.AllowAll

// Always-deny (for testing):
func alwaysDeny(_ context.Context, _ tools.PermissionRequest) tools.Decision {
    return tools.Deny
}
```

### 3.2 `AllowAll`

```go
func AllowAll(_ context.Context, _ PermissionRequest) Decision {
    return Allow
}
```

`AllowAll` is used in:
- Headless mode (piped input) — the user isn't present to approve anyway
- Worker agents in coordinator mode — coordinator made the permission decision
- GitHub webhook runner — running in CI with no interactive user
- Tests — don't want permission prompts blocking test execution

It is a package-level function (not a variable) so it can't be accidentally
reassigned and can be compared by address for logging purposes.

### 3.3 The permission function is called before every tool that needs it

This is important: `NeedsPermission` is not a one-time check at registration.
It is called for every individual tool invocation, with the actual input.
The permission engine (Phase 4) intercepts this and applies its rule chain
(persisted allows, deny lists, mode) before ever reaching the interactive
`promptFn`.

The flow:

```
registry.Execute(ctx, name, input)
    └─ tool found
    └─ l.registry.NeedsPermission(name, input)
          ├─ false → execute directly
          └─ true  → l.permitFn(ctx, PermissionRequest{...})
                          ├─ Allow/AlwaysAllow → execute
                          └─ Deny → return denied ToolResultBlock
```

---

## 4. `toolutil` — Shared Tool Infrastructure

### 4.1 `WriteAtomic`

```go
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
    dir := filepath.Dir(path)
    tmp, err := os.CreateTemp(dir, ".drover-code-tmp-*")
    // ... write data to tmp ...
    // ... chmod tmp ...
    // ... close tmp ...
    return os.Rename(tmp.Name(), path)
}
```

Every file write in drover-code goes through `WriteAtomic`. The invariant it
provides: the target file is either the old version or the new version — never
a partial write.

Why `os.CreateTemp` in the **same directory** as the target? `os.Rename` is
only atomic within the same filesystem. If the temp file is in `/tmp` and the
target is in `/home/user/project`, `os.Rename` is a cross-device move that
falls back to a copy-then-delete — not atomic. By creating the temp file in
the same directory (`filepath.Dir(path)`), we guarantee both are on the same
filesystem and `os.Rename` is a true atomic operation.

The temp file pattern `.drover-code-tmp-*` is distinctive enough to identify
if one is ever left behind (e.g. if the process is killed between `CreateTemp`
and `Rename`). Cleanup of stale temp files is not currently implemented — they
are small and rare enough that it's not a practical problem.

```go
// Why we chmod before close, not after:
if err := tmp.Chmod(perm); err != nil { ... }
if err := tmp.Close(); err != nil { ... }
if err := os.Rename(tmpName, path); err != nil { ... }
```

`Chmod` before `Close` ensures the permissions are set on the temp file
before it becomes visible at the target path. After `Rename`, the file is
immediately accessible with the correct permissions.

### 4.2 `SafePath`

```go
func SafePath(workDir, path string) (string, error) {
    if !filepath.IsAbs(path) {
        path = filepath.Join(workDir, path)
    }
    abs, err := filepath.Abs(path)
    if err != nil {
        return "", fmt.Errorf("resolve path: %w", err)
    }
    if workDir != "" {
        absWork, _ := filepath.Abs(workDir)
        if !strings.HasPrefix(abs, absWork+string(filepath.Separator)) && abs != absWork {
            return "", fmt.Errorf("path %q escapes working directory", path)
        }
    }
    return abs, nil
}
```

Path traversal is the most common class of security vulnerability in tools
that accept user-supplied file paths. If the model is manipulated into calling
`read_file` with `path: "../../../../etc/passwd"`, `SafePath` catches it.

The check `strings.HasPrefix(abs, absWork+string(filepath.Separator))` is
careful about the separator: without it, a path like `/home/user/project-evil`
would incorrectly pass the prefix check for workDir `/home/user/project`.
The `|| abs == absWork` case handles the workDir itself (e.g.
`list_directory` with `path: "."`).

`filepath.Abs` resolves `..` components and symlinks in the path itself,
but it does not resolve symlinks in the **target**. A symlink at
`/project/secret -> /etc/passwd` would pass this check (the symlink is
inside the project) but point outside it. This is intentional — preventing
symlink traversal would require `filepath.EvalSymlinks`, which does a full
filesystem walk and is significantly more expensive. The threat model for
drover-code is a misbehaving model, not a malicious filesystem.

### 4.3 `Truncate`

```go
const MaxOutputBytes = 200_000

func Truncate(s string) string {
    if len(s) <= MaxOutputBytes {
        return s
    }
    b := []byte(s[:MaxOutputBytes])
    for !utf8.Valid(b) {
        b = b[:len(b)-1]
    }
    return string(b) + fmt.Sprintf(
        "\n\n[output truncated at %d bytes — %d bytes total]",
        MaxOutputBytes, len(s),
    )
}
```

The 200,000 byte cap prevents any single tool output from consuming more
than about 50,000 tokens — a quarter of the total context window. Without
this cap, `cat` on a large log file could fill the entire context in one
call.

The UTF-8 validity loop at the clip boundary is important. Slicing a UTF-8
string at an arbitrary byte position can cut a multi-byte character in half,
producing an invalid UTF-8 sequence. The model (and JSON serialisation) may
behave unpredictably with invalid UTF-8. The loop backs up byte by byte until
the prefix is valid — at most 3 steps for any valid UTF-8 input.

The truncation note at the end is informative: it tells the model that there
is more content it didn't see. Without this, the model might conclude that a
file ends mid-sentence, or that a command's output was complete when it wasn't.

**Why 200,000 bytes and not tokens?** We cap at bytes because counting bytes is
O(1) (slice length). Counting tokens accurately requires running a tokenizer
which is O(n) and involves more code. At 200,000 bytes and ~4 chars/token,
we're looking at ~50,000 tokens — a reasonable upper bound for any single
tool result.

### 4.4 The `Schema` builder

```go
type Schema struct {
    m map[string]any
}

func NewSchema(typ string) *Schema    { return &Schema{m: map[string]any{"type": typ}} }
func (s *Schema) Desc(d string) *Schema { s.m["description"] = d; return s }
func (s *Schema) Prop(name string, child *Schema) *Schema { ... }
func (s *Schema) Required(names ...string) *Schema { ... }
func (s *Schema) Enum(vals ...string) *Schema { ... }
func (s *Schema) Items(child *Schema) *Schema { ... }
func (s *Schema) Build() json.RawMessage { b, _ := json.Marshal(s.m); return b }
```

The builder is a thin wrapper over `map[string]any`. It produces valid JSON
Schema objects via `json.Marshal`. The fluent API makes schemas readable:

```go
// bash tool input schema
toolutil.NewSchema("object").
    Prop("command", toolutil.NewSchema("string").
        Desc("The bash command to execute")).
    Prop("timeout_seconds", toolutil.NewSchema("integer").
        Desc("Max execution time in seconds (default: 120, max: 600)")).
    Prop("working_directory", toolutil.NewSchema("string").
        Desc("Directory to run in, defaults to project root")).
    Required("command").
    Build()
```

The `Required` call sets `"required": ["command"]` in the schema, which tells
the model that `command` must be present. Optional fields are omitted from
`Required` but still appear in `properties`.

The builder does not validate the schema it builds — there is no check that
required field names appear in properties, or that types are valid JSON Schema
types. This is a deliberate trade-off: schema validation adds complexity and
compile-time dependencies. Schema correctness is verified by the API returning
400 errors during development.

**Thread safety:** The `Schema` type is not goroutine-safe during construction.
Schemas should be built once (at package init time or in `InputSchema()`) and
the resulting `json.RawMessage` stored. After `Build()` is called, the result
is a `[]byte` which is safe to read from multiple goroutines.

---

## 5. Tool Categorisation by Permission

Understanding which tools need permission and why helps inform the permission
engine's default rules.

### Never needs permission (read-only, no side effects)

| Tool | Why safe |
|---|---|
| `read_file` | Read-only; cannot modify state |
| `list_directory` | Read-only metadata |
| `file_info` | Read-only metadata |
| `glob` | Read-only search |
| `grep` | Read-only search |
| `git_status` | Read-only git query |
| `git_diff` | Read-only git query |
| `git_log` | Read-only git query |
| `web_fetch` | Network request, but outbound only; no local side effects |

### Always needs permission (write, execute, or irreversible)

| Tool | Why dangerous |
|---|---|
| `write_file` | Creates or overwrites files |
| `edit_file` | Modifies existing files |
| `bash` | Can execute arbitrary code |
| `git_add` | Stages changes |
| `git_commit` | Creates commits |
| `git_push` | Sends commits to remote |
| `git_create_branch` | Creates branches |

This categorisation maps directly to the `ModePlan` behaviour: auto-approve
the first group, gate the second group for batch user approval before execution.

---

## 6. Adding a New Tool: Checklist

When implementing a new tool, work through this checklist:

```
□ Package: create internal/tools/<category>/<name>.go
□ Struct: export the type, embed WorkDir if it touches files
□ Name(): snake_case, verb-first, globally unique
□ Description(): write for the model, not the implementer
□ InputSchema(): use toolutil.NewSchema; store result as package var
□ NeedsPermission(): true for any write, execute, or irreversible action
□ Execute(): 
  □ unmarshal input with json.Unmarshal into a typed struct
  □ validate required fields before doing any work
  □ use toolutil.SafePath for all file paths
  □ use toolutil.WriteAtomic for all file writes
  □ wrap output with toolutil.Truncate
  □ include context in the output string (command run, path accessed)
  □ return descriptive errors (not just "failed")
  □ respect ctx cancellation
□ Goroutine safety: verify tool has no shared mutable state
□ Register: add to tools/register.go RegisterAll()
□ Test: unit test with mock inputs covering happy path + error cases
```

---

## 7. Tool Output Format Guidelines

The string returned by `Execute` goes directly into a `tool_result` content
block that the model reads. Format choices significantly affect model
comprehension.

**Include the operation in the output.** The model may forget what it called
or why. Repeating the key parameter grounds the response:

```
// Good:
"$ go test ./...\nexit_code: 0\n\n[stdout]\nok github.com/foo/bar 0.123s"

// Less good:
"ok github.com/foo/bar 0.123s"
```

**Use structured separators for multi-part outputs.** When stdout and stderr
are separate, label them:

```
[stdout]
<content>

[stderr]
<content>
```

**State explicitly when there are no results.** Empty output is ambiguous:

```
// Good:
"no files matched pattern '**/*.rb' in /project"

// Bad:
""   // model may think the tool failed
```

**Quantify bounded outputs.** When results are capped:

```
"1000 file(s) matched '**/*.go' (limit reached, more files may exist):"
```

**Include timing for slow operations.** Helps the model understand it needs
to wait or should avoid repeating the call:

```
"$ npm install\nexit_code: 0  elapsed: 8.3s"
```

---

## 8. Testing Strategy

### Unit tests for the registry

```go
// Registration
reg := tools.NewRegistry()
reg.Register(&MockTool{name: "test_tool"})
assert(reg.Get("test_tool") != nil)

// Duplicate registration panics
assert.Panics(func() {
    reg.Register(&MockTool{name: "test_tool"}) // second registration
})

// Unknown tool returns error
_, err := reg.Execute(ctx, "nonexistent", nil)
assert(err != nil)
assert(strings.Contains(err.Error(), "unknown tool"))

// Definitions includes all registered tools
defs := reg.Definitions()
assert(len(defs) == 1)
assert(defs[0].Name == "test_tool")
```

### Unit tests for `toolutil`

```go
// SafePath: rejects traversal
_, err := toolutil.SafePath("/project", "../../etc/passwd")
assert(err != nil)

// SafePath: allows subdirectory
p, err := toolutil.SafePath("/project", "src/main.go")
assert(err == nil)
assert(p == "/project/src/main.go")

// SafePath: allows workDir itself
p, err := toolutil.SafePath("/project", ".")
assert(err == nil)
assert(p == "/project")

// Truncate: leaves short strings unchanged
assert(toolutil.Truncate("hello") == "hello")

// Truncate: clips long strings
long := strings.Repeat("x", 300_000)
result := toolutil.Truncate(long)
assert(len(result) <= 200_100)  // 200k + truncation note
assert(strings.Contains(result, "truncated"))

// Truncate: clips at valid UTF-8 boundary
// (construct a string where byte 200_000 falls mid-multibyte-char)

// WriteAtomic: creates file that didn't exist
toolutil.WriteAtomic("/tmp/test-atomic", []byte("hello"), 0o644)
assert(os.ReadFile("/tmp/test-atomic") == "hello")

// WriteAtomic: replaces file atomically (no partial write visible)
// (hard to test atomicity directly, but verify result is correct)
```

### Concurrency tests for the registry

```go
// Multiple goroutines calling Execute concurrently
var wg sync.WaitGroup
for i := 0; i < 100; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        reg.Execute(ctx, "test_tool", []byte(`{}`))
    }()
}
wg.Wait()
// Run with -race; should have no data races
```

---

## 9. Future Considerations

### Structured tool output

Today `Execute` returns `string`. A future version could return:

```go
type ToolOutput struct {
    Text   string          // always present
    Data   json.RawMessage // optional structured data
    Schema json.RawMessage // schema describing Data
}
```

The Anthropic API supports structured content in `tool_result` blocks. This
would allow the model to process machine-readable outputs (e.g. file listings
as JSON arrays) more precisely than parsing text.

### Tool versioning

As tools evolve, their input schemas may change. A `Version() string` method
on `Tool` would allow the registry to detect schema mismatches between a
conversation's tool call (which embedded the old schema) and the current
registration. Currently, schema changes silently break in-progress sessions.

### Dynamic tool registration

The current design registers all tools at startup and never changes the
registry. A future extension point: allow tools to be registered and
deregistered at runtime (e.g. when the user installs a plugin). This would
require invalidating the `Definitions()` cache and notifying the model that
available tools have changed — non-trivial to do correctly mid-conversation.

### Tool call logging

For audit purposes (especially in the GitHub webhook use case), every tool
call and its result should be logged. The registry's `Execute` method is the
right place to intercept this:

```go
func (r *Registry) Execute(ctx context.Context, name string, input json.RawMessage) (string, error) {
    r.mu.RLock()
    t, ok := r.tools[name]
    r.mu.RUnlock()
    if !ok { return "", fmt.Errorf("unknown tool %q", name) }

    start := time.Now()
    result, err := t.Execute(ctx, input)
    r.log(name, input, result, err, time.Since(start))  // future
    return result, err
}
```

---

*Previous: [`02-agent-loop.md`](./02-agent-loop.md)*  
*Next: [`04-fs-tools.md`](./04-fs-tools.md) — File System Tools*
