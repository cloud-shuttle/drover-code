# 09 — Advanced Systems: Dream Memory and Coordinator

**Packages:** `internal/dream`, `internal/coordinator`  
**Files:** `dream/dream.go`, `coordinator/coordinator.go`  
**Depends on:** `internal/api`, `internal/agent`, `internal/convo`, `internal/tools`  
**Depended on by:** `cmd/drover-code`

---

## Purpose

These two systems handle the hardest parts of building a useful long-running
agent: memory across sessions, and parallelism within sessions.

**Dream** solves the amnesia problem. Every session starts with a blank
conversation. Without some form of persistence, the model can never know
"I worked on this project last week and we decided X." Dream consolidates
each session into a short memory entry and injects recent memories at the
start of the next session.

**Coordinator** solves the throughput problem. Large tasks — "refactor the
entire auth module", "write tests for all uncovered files" — decompose
naturally into parallel subtasks. Running them sequentially through a single
agent is slow. The coordinator spawns multiple worker agents and runs them
concurrently.

Both systems are optional and off by default. They add real complexity and
the base agent loop works well without them for most tasks.

---

## 1. Dream Memory (`dream/dream.go`)

### 1.1 Naming

The name comes from neuroscience: sleep consolidates short-term memories into
long-term storage. The Dream system does the same — at the end of each session,
it consolidates the conversation into a persistent summary. The analogy is
deliberate: memories are imprecise summaries, not verbatim transcripts, and
they fade over time.

The original Claude Code source names this system `dream` throughout, including
internal functions and the background goroutine name. We preserve the name.

### 1.2 Architecture overview

```
Session ends
    │
    ▼
Worker.Trigger(session)         ← non-blocking, buffered channel
    │
    ▼ (background goroutine)
Summarise via Anthropic API     ← non-streaming call, 60s timeout
    │
    ▼
Store.Save(entry)               ← atomic write to disk
```

```
Next session starts
    │
    ▼
Store.Recent(5)                 ← load 5 most recent entries
    │
    ▼
BuildInjection(entries)         ← format as system prompt fragment
    │
    ▼
mgr.SetSystemPrompt(base + injection)
```

### 1.3 The `Store` interface

```go
type Store interface {
    Save(e Entry) error
    Recent(n int) ([]Entry, error)
    All() ([]Entry, error)
}

type Entry struct {
    ID        string    `json:"id"`
    Timestamp time.Time `json:"timestamp"`
    Tags      []string  `json:"tags"`
    Content   string    `json:"content"`
    SessionID string    `json:"session_id"`
}
```

The interface is minimal by design. Two implementations exist:

**`jsonStore`** — default when `DROVER_CODE_DREAM_BACKEND` is unset. Stores all
entries in a single JSON file at `.claude/memory.json`. Atomic writes via
temp-file + rename. Simple, zero dependencies. Downside: loading all entries to
find recent ones is O(n) in total entries.

**`sqliteStore`** — opt-in with `DROVER_CODE_DREAM_BACKEND=sqlite` → `.claude/memory.db` (`modernc.org/sqlite`, pure Go, CGO-free). Indexed recency
queries; good for large session counts. On first open, an **empty** DB imports
existing `memory.json` and renames it to **`memory.json.imported`** (skip with
`DROVER_CODE_DREAM_SKIP_JSON_IMPORT=1`).

**Retention** — optional caps via settings `dreamMaxRetentionEntries` /
`dreamMaxRetentionAgeDays` and env `DROVER_CODE_DREAM_MAX_ENTRIES` /
`DROVER_CODE_DREAM_MAX_AGE_DAYS`. `Store.Prune` runs after each consolidation
save and once when the store is opened if any limit is active.

The interface split is intentional: callers use `dream.OpenStore(workDir)` and
`Store` without caring which backend is active.

### 1.4 The `Worker`

```go
type Worker struct {
    store     Store
    client    summariser
    retention Retention       // optional max entry count / max age; see dream/retention.go
    triggerCh chan Session  // buffered, size 8
    wg        sync.WaitGroup
}

func NewWorker(store Store, client summariser, retention Retention) *Worker

func (w *Worker) Trigger(s Session) {
    select {
    case w.triggerCh <- s:
    default:
        // Channel full — drop silently
        // Memories are nice-to-have, never blocking
    }
}

func (w *Worker) Start(ctx context.Context) {
    w.wg.Add(1)
    go func() {
        defer w.wg.Done()
        for {
            select {
            case s := <-w.triggerCh:
                w.consolidate(ctx, s)
            case <-ctx.Done():
                return
            }
        }
    }()
}
```

**Why a channel instead of calling consolidate directly?** The caller
(`main.go`) triggers consolidation after the TUI exits. At that point, the
program is shutting down. If consolidation ran synchronously, it would block
the process from exiting for up to 60 seconds (the API timeout). The channel
allows the trigger to return immediately, and `Worker.Wait()` allows the caller
to optionally wait for completion before exiting.

**Buffer of 8.** In normal use, there is one session per drover-code invocation.
The buffer of 8 handles edge cases where multiple sessions are triggered
rapidly. Coordinator mode triggers **one** consolidation at process exit with a
single transcript (synthesis plus per-worker output), not per-worker triggers.
If the buffer fills, triggers are dropped silently — memory consolidation is
best-effort.

**Why best-effort?** Dream memories are convenient context, not critical state.
The model can always work without them. An agent system that blocks or fails
because it couldn't write a memory entry would be far more disruptive than
occasionally missing a memory.

### 1.5 Consolidation

```go
func (w *Worker) consolidate(ctx context.Context, s Session) {
    if len(s.Messages) == 0 { return }

    // Build the conversation text for summarisation
    var conv strings.Builder
    for _, m := range s.Messages {
        role := "User"
        if m.Role == api.RoleAssistant { role = "Assistant" }
        for _, b := range m.Content {
            if tb, ok := b.(api.TextBlock); ok {
                fmt.Fprintf(&conv, "%s: %s\n\n", role, tb.Text)
            }
        }
    }

    summaryPrompt := fmt.Sprintf(`Summarise this conversation into 3-5 bullet points capturing:
- What was worked on (files, features, bugs)
- Key decisions made
- Important context for future sessions

Be concise. Output only the bullet points, nothing else.

---
%s`, conv.String())

    ctx2, cancel := context.WithTimeout(ctx, 60*time.Second)
    defer cancel()

    stream, err := w.client.StreamMessage(ctx2, api.StreamRequest{
        Messages:  []api.Message{api.UserMessage(summaryPrompt)},
        MaxTokens: 512,
    })
    ...
}
```

**Why only `TextBlock` content?** Tool calls and tool results are not included
in the conversation text sent for summarisation. They would add noise — the
model doesn't need to know the exact parameters of every `read_file` call or
the raw content of every file it read. The text blocks capture the important
information: what the user asked for and what the model explained.

**512 max tokens for the summary.** Three to five bullet points should fit in
200 tokens. The 512 cap gives ample headroom without risking a very long
summary that defeats the purpose of compression.

**Non-streaming call.** The consolidation happens in the background after the
user session ends. We don't need to stream the summary to the user — we just
want the final text. Using `StreamMessage` is slightly wasteful (it opens a
streaming connection for what's effectively a one-shot request) but avoids
maintaining two code paths (streaming and non-streaming) in the API client.
A future optimisation: add a `CompleteMessage()` method to the client.

### 1.6 Tag extraction

```go
func extractTags(summary string) []string {
    var tags []string
    seen := map[string]bool{}

    for _, word := range strings.Fields(summary) {
        word = strings.Trim(word, ".,;:\"'`()")
        if strings.Contains(word, ".go") || strings.Contains(word, ".ts") ||
           strings.Contains(word, ".py") || strings.Contains(word, "/") {
            if !seen[word] {
                tags = append(tags, word)
                seen[word] = true
            }
        }
    }
    return tags
}
```

Tags are file names and path components extracted heuristically from the
summary text. They serve two purposes:

1. **Future retrieval.** A tag-based query (`store.SearchByTag("auth.go")`)
   would return sessions that worked on auth-related files — more useful than
   recency alone.

2. **Context for the injection.** Showing the model which files were relevant
   in past sessions helps it prioritise where to look first.

The heuristic is crude: any word containing `.go`, `.ts`, `.py`, or `/` is
treated as a file reference. False positives (e.g. "e.g.", "i.e.") are rare in
code-related summaries. False negatives (file names in other languages, unusual
extensions) are acceptable — tags are a hint, not an index.

### 1.7 Injection

```go
func BuildInjection(store Store, maxEntries int) string {
    if store == nil { return "" }

    entries, err := store.Recent(maxEntries)
    if err != nil || len(entries) == 0 { return "" }

    var b strings.Builder
    b.WriteString("## Memory from previous sessions\n\n")
    for _, e := range entries {
        fmt.Fprintf(&b, "**%s**\n%s\n\n",
            e.Timestamp.Format("2006-01-02"),
            e.Content,
        )
    }
    return b.String()
}
```

The injection is formatted as a markdown section with date-stamped entries.
Each entry is the bullet-point summary from its session.

Example injection:

```markdown
## Memory from previous sessions

**2024-01-15**
- Refactored authentication module in src/auth/
- Decided to use JWT tokens instead of sessions; see ADR in docs/
- Left src/auth/refresh.go partially implemented — token expiry logic TODO

**2024-01-14**
- Added rate limiting middleware in src/middleware/
- Tests in src/middleware/ratelimit_test.go are passing
- Discussed moving to Redis for rate limit storage; deferred to v2
```

This gives the model genuine context at the start of the next session. Instead
of starting from scratch, it knows what was worked on, what decisions were made,
and what is incomplete.

**5 entries as the default maximum.** Each entry is ~100–200 tokens. Five
entries is ~500–1000 tokens — a small fraction of the 200k context window. If
the store has hundreds of entries, we still only inject the 5 most recent.
Older memories are accessible via the `/memory` slash command.

### 1.8 Session ID

```go
type Session struct {
    ID       string
    Messages []api.Message
}
```

The `ID` field allows deduplicating sessions. If `Trigger()` is called twice
for the same session (e.g. due to a bug), we could check whether an entry with
that session ID already exists and skip re-summarisation. The current
implementation doesn't do this deduplication — it's a future improvement.

Session IDs are currently set by the caller as simple strings (`"tui"`,
`"headless"`, `"coordinator-worker-0"`). A future implementation would use
UUIDs generated at session start.

### 1.9 Testing strategy

```go
// Store: save and retrieve
store, _ := dream.NewJSONStore(t.TempDir() + "/memory.json")

entry := dream.Entry{
    ID:        "1",
    Timestamp: time.Now(),
    Content:   "- Worked on auth module\n- Fixed login bug",
}
err := store.Save(entry)
assertNoError(t, err)

entries, err := store.Recent(10)
assertNoError(t, err)
assert(t, len(entries) == 1)
assert(t, entries[0].Content == entry.Content)

// Persistence across instances
store2, _ := dream.NewJSONStore(/* same path */)
entries, _ = store2.Recent(10)
assert(t, len(entries) == 1)

// Recent returns in recency order
store.Save(dream.Entry{ID: "2", Timestamp: time.Now().Add(time.Hour), Content: "newer"})
store.Save(dream.Entry{ID: "3", Timestamp: time.Now().Add(-time.Hour), Content: "older"})
entries, _ = store.Recent(10)
assert(t, entries[0].Content == "newer")
assert(t, entries[2].Content == "older")

// BuildInjection: empty store returns empty string
assert(t, dream.BuildInjection(nil, 5) == "")

emptyStore, _ := dream.NewJSONStore(t.TempDir() + "/empty.json")
assert(t, dream.BuildInjection(emptyStore, 5) == "")

// BuildInjection: formats correctly
store.Save(dream.Entry{
    ID:        "4",
    Timestamp: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
    Content:   "- Fixed auth bug",
})
injection := dream.BuildInjection(store, 1)
assert(t, strings.Contains(injection, "2024-01-15"))
assert(t, strings.Contains(injection, "Fixed auth bug"))
assert(t, strings.HasPrefix(injection, "## Memory"))

// Worker: consolidate produces an entry (requires fake API)
fakeClient := &fakeAPI{response: "- Refactored auth\n- Added tests"}
store3, _ := dream.NewJSONStore(t.TempDir() + "/worker.json")
worker := dream.NewWorker(store3, fakeClient, dream.Retention{})

ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
worker.Start(ctx)

worker.Trigger(dream.Session{
    ID: "test",
    Messages: []api.Message{
        api.UserMessage("refactor the auth module"),
        api.AssistantMessage([]api.ContentBlock{api.TextBlock{Text: "Done"}}),
    },
})
worker.Wait()

entries, _ = store3.Recent(10)
assert(t, len(entries) == 1)
assert(t, strings.Contains(entries[0].Content, "Refactored auth"))
```

---

## 2. Coordinator Mode (`coordinator/coordinator.go`)

### 2.1 When to use coordinator mode

Coordinator mode is appropriate when:

- The task naturally decomposes into independent parallel subtasks
- Each subtask is substantial enough to warrant its own agent
- The subtasks don't have hard sequencing dependencies

Examples of good coordinator tasks:
- "Add comprehensive test coverage to the auth, user, and payment modules"
- "Refactor all handler functions to use the new error type"
- "Generate API documentation for every exported function in the project"

Examples of poor coordinator tasks:
- "Fix the bug in auth.go" — too focused, no natural decomposition
- "Implement the new database schema then update all queries to use it" —
  hard sequential dependency (schema first, then queries)
- "Explain how the codebase works" — pure reasoning, no tool parallelism benefit

### 2.2 Architecture

```
User request
    │
    ▼
Coordinator.Execute(ctx, task)
    │
    ├─ 1. decompose(ctx, task)
    │         LLM call → JSON array of subtask descriptions
    │
    ├─ 2. executeWorkers(ctx, subtasks)
    │         errgroup with semaphore (max 4 concurrent)
    │         ├─ Worker 0: runWorker(ctx, subtask[0])
    │         ├─ Worker 1: runWorker(ctx, subtask[1])
    │         ├─ Worker 2: runWorker(ctx, subtask[2])
    │         └─ Worker 3: runWorker(ctx, subtask[3])
    │
    └─ 3. synthesise(ctx, task, results)
              LLM call → merged response streamed to user
```

### 2.3 Decomposition

```go
func (c *Coordinator) decompose(ctx context.Context, task string) ([]Subtask, error) {
    prompt := fmt.Sprintf(`You are a coordinator agent. Break the following task into 2-4 parallel subtasks
that can be executed independently by separate worker agents.

Each worker has access to: read_file, write_file, edit_file, bash, glob, grep, git tools.

Return ONLY a JSON array of subtask descriptions (strings). No other text.
Example: ["Refactor authentication module", "Update unit tests for auth", "Update API documentation"]

Task: %s`, task)

    // Non-streaming LLM call...
    // Parse response as JSON array
    // Fall back to single subtask if parsing fails
}
```

**Why a separate LLM call for decomposition?** The coordinator needs to
understand the task before it can plan. A simple heuristic ("split on 'and'")
would fail for most real tasks. Using the model for decomposition leverages
its understanding of software engineering domain knowledge — it knows that
"refactor auth" and "add tests for auth" are separable tasks but "implement
X then use X" are not.

**The fallback to single subtask** is important for robustness. If the model
returns malformed JSON or describes only one task, we don't fail — we treat
the entire task as a single subtask and run one worker. The user gets a
slightly slower execution than hoped, not an error.

**2–4 subtasks.** The prompt constrains decomposition to 2–4 tasks. This is a
deliberate balance:
- Fewer than 2: no parallelism benefit, coordinator overhead wasted
- More than 4: diminishing returns, increased synthesis complexity, more risk
  of subtask conflicts

### 2.4 Worker isolation

```go
func (c *Coordinator) runWorker(ctx context.Context, st Subtask) WorkerResult {
    // Each worker gets its own convo.Manager
    workerMgr := convo.NewManagerWithSystem(workerSystemPrompt(st.Description))

    // Each worker gets its own event channel
    workerEvents := make(chan agent.Event, 128)
    go c.forwardWorkerEvents(st.Index, workerEvents)

    // Workers bypass permission — coordinator made the permission decision
    workerLoop := agent.NewLoop(
        c.client,      // shared — API client is goroutine-safe
        workerMgr,     // isolated — each worker's own conversation
        c.registry,    // shared — tools are goroutine-safe
        tools.AllowAll, // bypass permissions
        workerEvents,
    )
    // ...
}
```

**Shared vs isolated:**

| Resource | Sharing | Rationale |
|---|---|---|
| `api.Client` | Shared | HTTP client is goroutine-safe; connection pooling is efficient |
| `convo.Manager` | Isolated per worker | Conversation history must not bleed between workers |
| `tools.Registry` | Shared | Tools are goroutine-safe; no state per tool instance |
| Event channel | Isolated per worker | Workers emit to their own channel, coordinator forwards |

**Worker system prompt:**

```go
func workerSystemPrompt(task string) string {
    return fmt.Sprintf(`You are a worker agent. Your assigned task is:

%s

Complete this task using the available tools. Be precise and focused.
Do not attempt tasks outside your assignment. Report what you did concisely.`, task)
}
```

The worker's system prompt is focused on its subtask only. It does not know
about the overall task or other workers. This isolation prevents workers from
second-guessing the decomposition or trying to coordinate with each other
(which they couldn't do anyway, but the focused prompt reduces off-task
behaviour).

### 2.5 Event forwarding

```go
func (c *Coordinator) forwardWorkerEvents(workerIdx int, ch <-chan agent.Event) {
    for ev := range ch {
        switch e := ev.(type) {
        case agent.ToolStartEvent:
            e.CallIndex = workerIdx*100 + e.CallIndex
            select {
            case c.eventCh <- e:
            default:
            }
        case agent.ToolDoneEvent:
            e.CallIndex = workerIdx*100 + e.CallIndex
            select {
            case c.eventCh <- e:
            default:
            }
        }
    }
}
```

Worker tool events are relabelled and forwarded to the coordinator's event
channel. The `workerIdx*100 + callIndex` relabelling ensures unique `CallIndex`
values across all workers. The TUI receives these and can show parallel
spinners for different workers.

Only tool events are forwarded — text deltas from workers are collected into
the `WorkerResult.Output` string (for synthesis) and not streamed to the TUI.
The user sees tool activity during execution but doesn't see each worker's
intermediate text output. The synthesis step produces the single visible
output.

### 2.6 Parallel execution with semaphore

```go
func (c *Coordinator) executeWorkers(ctx context.Context, subtasks []Subtask) ([]WorkerResult, error) {
    results := make([]WorkerResult, len(subtasks))
    sem     := make(chan struct{}, c.MaxWorkers)  // semaphore, capacity = MaxWorkers

    g, gctx := errgroup.WithContext(ctx)
    var mu sync.Mutex

    for _, st := range subtasks {
        st := st
        sem <- struct{}{}  // acquire slot

        g.Go(func() error {
            defer func() { <-sem }()  // release slot

            result := c.runWorker(gctx, st)
            mu.Lock()
            results[st.Index] = result
            mu.Unlock()
            return nil
        })
    }

    return results, g.Wait()
}
```

**The semaphore pattern** (`make(chan struct{}, N)`) limits concurrency to N
goroutines regardless of how many subtasks were decomposed. The channel blocks
on `sem <- struct{}{}` when N goroutines are already running, then unblocks
when one finishes and releases its slot with `<-sem`.

**Why `MaxWorkers = 4` as default?**
- Each worker makes streaming API calls to the Anthropic API
- Each worker spawns tool subprocesses (bash, git)
- Each worker holds up to 200k tokens of context in memory
- 4 workers concurrently is substantial — more would hit API rate limits and
  consume significant memory on typical developer machines

**Worker failures don't propagate as Go errors.** `runWorker` never returns a
non-nil error — failures are captured in `WorkerResult.IsError`. This means
`errgroup` only fails on context cancellation (user Ctrl+C). Any individual
worker failure produces a `WorkerResult{IsError: true}` that the synthesis
step can describe to the user.

This is the right trade-off: one worker failing to complete its subtask should
not cancel all other workers that are making progress.

### 2.7 Synthesis

```go
func (c *Coordinator) synthesise(ctx context.Context, originalTask string, results []WorkerResult) (string, error) {
    var b strings.Builder
    fmt.Fprintf(&b, "Original task: %s\n\nWorker results:\n\n", originalTask)
    for _, r := range results {
        status := "✓"
        if r.IsError { status = "✗" }
        fmt.Fprintf(&b, "%s Worker %d (%s):\n%s\n\n", status, r.Index+1, r.Task, r.Output)
    }
    b.WriteString("Synthesise the above results into a single clear response for the user. " +
        "Summarise what was accomplished, note any errors, and provide a coherent overview.")

    // Streaming call — synthesis is streamed to the TUI in real time
    stream, _ := c.client.StreamMessage(ctx, api.StreamRequest{
        Messages:  []api.Message{api.UserMessage(b.String())},
        MaxTokens: 2048,
    })

    var summary strings.Builder
    for stream.Next() {
        if e, ok := stream.Event().(api.ContentBlockDeltaEvent); ok {
            if td, ok := e.Delta.(api.TextDelta); ok {
                summary.WriteString(td.Text)
                select {
                case c.eventCh <- agent.TextDeltaEvent{Text: td.Text}:
                default:
                }
            }
        }
    }
    // ...
}
```

**Synthesis is the only streaming call in the coordinator.** Decomposition and
individual worker runs don't stream to the user — the user sees tool spinners
from workers and then the synthesis response arriving token by token.

**2048 max tokens for synthesis.** The synthesis needs to summarise N workers'
outputs, mention any errors, and give a coherent overview. 2048 tokens (~1500
words) is ample for 2–4 worker summaries.

**Streaming synthesis tokens directly to `eventCh`.** This means the user sees
the synthesis response arriving in real time in the TUI, identical to how a
normal single-agent response arrives. From the TUI's perspective, coordinator
mode looks identical to normal mode once workers complete — the synthesis just
appears as streaming text.

### 2.8 The `extractJSON` helper

```go
func extractJSON(s string) string {
    // Strip markdown code fences if present
    if idx := strings.Index(s, "```"); idx >= 0 {
        s = s[idx+3:]
        if idx2 := strings.Index(s, "```"); idx2 >= 0 {
            s = s[:idx2]
        }
        s = strings.TrimPrefix(s, "json")
    }
    // Find [ ... ] bounds
    start := strings.Index(s, "[")
    end   := strings.LastIndex(s, "]")
    if start < 0 || end < 0 || end <= start { return "[]" }
    return s[start : end+1]
}
```

The decomposition prompt asks the model to return "ONLY a JSON array." Models
often comply, but sometimes prefix with "Here are the subtasks:" or wrap in
markdown code fences. `extractJSON` handles:
- Clean JSON: `["task1", "task2"]`
- Preamble: `Here are the subtasks:\n["task1", "task2"]`
- Code fence: `\`\`\`json\n["task1", "task2"]\n\`\`\``

The `strings.LastIndex(s, "]")` uses last-index rather than first-index for
the closing bracket, handling nested brackets within task descriptions
(e.g. `"Refactor [legacy] auth module"`).

### 2.9 Conflict avoidance

Workers operate in the same working directory. If two workers both try to
edit the same file simultaneously, the second edit may apply on top of the
first, producing unexpected results. Or both may read the file before either
writes, causing one to overwrite the other's change.

The current design does not prevent conflicts. It relies on the decomposition
being correct — subtasks should target different files or different parts of
the codebase. The prompt instructs the model to produce independent subtasks,
which in practice means "don't give multiple workers the same file."

A more robust approach would use isolated work directories:

```go
// Each worker clones the repository into its own temp dir
// Worker changes are staged and merged by the coordinator
func (c *Coordinator) runWorkerIsolated(ctx context.Context, st Subtask) WorkerResult {
    dir, cleanup := IsolatedWorkDir(c.workDir, st.Index)
    defer cleanup()
    // shallow copy of work dir (hard links)
    copyDir(c.workDir, dir)
    // run worker in isolated dir
    // after completion, merge changes back via patch/diff
}
```

This is significantly more complex and is planned for a future phase. For
the current implementation, the coordinator is most useful for tasks where
subtasks are clearly non-overlapping (different modules, different test files,
different documentation pages).

### 2.10 Testing strategy

```go
// Decompose: valid response
client := &fakeClient{decompositionResponse: `["Refactor auth", "Add tests", "Update docs"]`}
coord := coordinator.New(client, registry, workDir, eventCh)

subtasks, err := coord.DecomposeForTest(ctx, "improve the auth module")
assertNoError(t, err)
assert(t, len(subtasks) == 3)
assert(t, subtasks[0].Description == "Refactor auth")

// Decompose: malformed JSON falls back to single subtask
client.decompositionResponse = "I cannot break this into subtasks"
subtasks, err = coord.DecomposeForTest(ctx, "explain the code")
assertNoError(t, err)
assert(t, len(subtasks) == 1)

// Decompose: code-fenced JSON
client.decompositionResponse = "```json\n[\"Task A\", \"Task B\"]\n```"
subtasks, _ = coord.DecomposeForTest(ctx, "any task")
assert(t, len(subtasks) == 2)

// Workers run in parallel (timing test)
slowTool := &mockTool{name: "slow", delay: 100 * time.Millisecond}
fastTool := &mockTool{name: "fast", delay: 10 * time.Millisecond}
registry.Register(slowTool)
registry.Register(fastTool)

start := time.Now()
client.workerResponses = []string{
    "tool_use: slow",  // worker 0
    "tool_use: fast",  // worker 1
}
coord.Execute(ctx, "do both tasks")
elapsed := time.Since(start)

// Sequential would take 110ms+; parallel should be ~100ms+
// With real goroutines, parallel is ~100ms not ~200ms
assert(t, elapsed < 180*time.Millisecond)

// Worker failure is isolated — other workers still complete
client.workerResponses = []string{
    "error: permission denied",   // worker 0 fails
    "done",                        // worker 1 succeeds
}
result, err := coord.Execute(ctx, "parallel task")
assertNoError(t, err)  // Execute doesn't fail when workers fail
assert(t, strings.Contains(result, "✗"))  // synthesis notes the failure
assert(t, strings.Contains(result, "✓"))  // and the success

// Event forwarding: tool events relabelled
var events []agent.Event
go func() {
    for ev := range eventCh { events = append(events, ev) }
}()
// ... run coordinator with tools ...
// Worker 0 CallIndex 0 → emitted as CallIndex 0
// Worker 1 CallIndex 0 → emitted as CallIndex 100
toolEvents := filterToolEvents(events)
for _, e := range toolEvents {
    assert(t, e.CallIndex/100 == e.WorkerIndex || e.CallIndex == e.OriginalCallIndex)
}
```

---

## 3. Interaction Between Dream and Coordinator

Coordinator workers do **not** trigger Dream on their own. A single
`dream.Worker.Trigger` runs when **coordinator mode exits** (`main`): the
scratch transcript includes each user line, an assistant turn with the
**synthesised summary** plus a **per-worker section** (task, ok/error, truncated
output) so consolidation can see what each parallel agent did—not only the
final synthesis paragraph.

`coordinator.ExecuteWithResults` exposes `ExecuteOutcome{Summary, Workers}`
for that formatting without changing the user-visible printed summary (still
the synthesis only).

---

## 4. Performance Characteristics

### Dream worker

| Operation | Latency | Notes |
|---|---|---|
| `Trigger()` | O(1) | Channel send, non-blocking |
| `consolidate()` | 2–10s | Depends on conversation length + API response time |
| `store.Save()` | <1ms | Atomic file write (JSON) or insert (SQLite) |
| `store.Recent(5)` | <1ms | JSON: unmarshal + sort; SQLite: indexed `LIMIT` |

Session start is unaffected by dream consolidation — it happens after the
session ends. The only impact on session start is `BuildInjection()`, which
is a file read + JSON parse — negligible.

### Coordinator

| Phase | Latency | Notes |
|---|---|---|
| Decompose | 1–3s | One LLM call |
| Execute (parallel) | max(worker latencies) | Bounded by slowest worker |
| Execute (sequential would be) | sum(worker latencies) | Never used |
| Synthesise | 2–8s | One streaming LLM call |

For a 4-worker coordinator run where each worker takes ~30 seconds (a moderate
agentic task), sequential execution would take ~120 seconds. Parallel execution
takes ~30 seconds plus ~5 seconds overhead = ~35 seconds. A 3.4× speedup.

The speedup is real but bounded. Tasks that are I/O-bound (reading many files,
running many commands) parallelise well. Tasks that are compute-bound on the
API side (complex reasoning, large outputs) are limited by the API's token
generation rate, which doesn't scale horizontally.

---

## 5. Edge Cases and Known Issues

### Dream: conversation too long to summarise

If the conversation is extremely long (near the 200k token context limit),
sending it to the API for summarisation would fail — the summarisation prompt
itself is a conversation, and the conversation being summarised is its content.

Mitigation: truncate the conversation before summarisation. Include the most
recent N messages rather than the full history:

```go
// In consolidate():
msgs := s.Messages
if len(msgs) > 50 {
    msgs = msgs[len(msgs)-50:]  // last 50 messages
}
```

50 messages represents roughly the last 10 agent turns — enough for a useful
summary. This is not currently implemented.

### Dream: tool result content in summary

`TextBlock` content only is sent for summarisation, skipping `ToolUseBlock`
and `ToolResultBlock`. This means the summary never mentions specific file
contents or command outputs — only what the model said about them. This is
generally correct (memories should be about decisions and outcomes, not raw
data) but can miss important context like "the test output showed a nil pointer
at line 42" which is in a tool result, not a text block.

### Coordinator: API rate limits with many workers

Running 4 workers simultaneously means 4 concurrent streaming API calls. Under
heavy load this can hit Anthropic's rate limits. The 429 response from the API
propagates as an error in the worker, which is captured as `WorkerResult.IsError`.
The synthesis step describes the failure.

Future improvement: implement retry with exponential backoff in the API client
for coordinator-context calls.

### Coordinator: context inheritance

Worker agents don't share the parent conversation history. They start with only
their subtask description. If the parent session has established important
context ("we're using Go 1.22 and the project uses the `errors` package
convention"), workers don't know this unless it's in the CLAUDE.md or repeated
in the subtask description.

The coordinator prompt could inject relevant context into each subtask:

```go
func workerSystemPrompt(task string, parentContext string) string {
    return fmt.Sprintf(`You are a worker agent.

Project context:
%s

Your assigned task:
%s`, parentContext, task)
}
```

Where `parentContext` is extracted from the parent's conversation manager.

---

*Previous: [`08-config-permissions-undercover.md`](./08-config-permissions-undercover.md)*  
*Next: [`10-integrations.md`](./10-integrations.md) — IDE Bridge and GitHub Webhook*