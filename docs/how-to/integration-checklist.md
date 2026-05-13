# Integration Points & Developer Checklist

A practical guide to implementing features and maintaining integration points in drover-code.

---

## Quick Reference: Module Entry Points

### Adding a New Tool

**Steps:**

1. **Choose a package** under `internal/tools/`
   - Existing: `fs/`, `git/`, `search/`, `shell/`, `web/`, `ukc/`
   - Or create new: `internal/tools/yourname/`

2. **Implement `Tool` interface**
   ```go
   type YourTool struct {
       WorkDir string  // if filesystem access needed
   }
   
   func (t *YourTool) Name() string {
       return "your_tool"  // snake_case, verb-first
   }
   
   func (t *YourTool) Description() string {
       return "Clear, model-focused description"
   }
   
   func (t *YourTool) InputSchema() json.RawMessage {
       return toolutil.NewSchema("object").
           Prop("param", toolutil.NewSchema("string")).
           Desc("Parameter description").
           Required("param").
           RawJSON()
   }
   
   func (t *YourTool) NeedsPermission(input json.RawMessage) bool {
       return false  // true if modifies state
   }
   
   func (t *YourTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
       var params struct {
           Param string `json:"param"`
       }
       if err := json.Unmarshal(input, &params); err != nil {
           return "", err
       }
       // Do work
       return "output", nil
   }
   ```

3. **Write tests** (required)
   - `yourname_test.go` in same package
   - Test schema validity
   - Test error cases
   - Test permission detection

4. **Register in registry**
   - Add to `tools/register.go` in `RegisterAll()`
   ```go
   r.Register(&yourpkg.YourTool{WorkDir: workDir})
   ```

5. **Document**
   - Add entry to `available-tools.md`
   - Update category if needed

6. **Update permission presets** (if needed)
   - Modify `internal/permissions/preset.go`

---

### Modifying the Agent Loop

**When:** Adding response-handling logic, tool execution paths, streaming behavior

**Files to change:**
- `internal/agent/loop.go` — main loop, event emission
- `internal/agent/events.go` — event types if adding new event categories
- `internal/convo/manager.go` — if affecting message history

**Critical invariants:**
- Tool results always batched in single user message
- Block accumulation keyed by index (not sequence)
- Stop reason checked, never message content
- Events emitted for all significant state changes

**Testing:**
- Use `FakeStream` in tests to simulate API responses
- Test tool call accumulation with interleaved blocks
- Test error recovery (retryable vs permanent)

---

### Adding a New Configuration Option

**Steps:**

1. **Define in `Settings` struct** (`internal/config/loader.go`)
   ```go
   type Settings struct {
       // ... existing fields
       MyNewOption string `json:"myNewOption"`
   }
   ```

2. **Add environment variable support** (if applicable)
   - Convention: `DROVER_CODE_MY_NEW_OPTION`
   - Apply in `applyRuntimeSettings()` in `apply_runtime.go`

3. **Provide defaults** 
   - In `defaultSettings()` or at use site
   - Make sure all paths handle zero value

4. **Document**
   - Add to README.md Configuration section
   - Add to this checklist

5. **Test**
   - Unit test in `config/*_test.go`
   - Test cascade: default → env → local.json → project.json → home.json

---

### Adding a Custom Command

**When:** Implementing repeatable agentic workflows or prompt templates.

**Steps:**

1. **Create Markdown Definition**
   - Place in `.drover/commands/my-command.md` or `~/.drover/commands/`
   ```markdown
   ---
   name: my-command
   description: Does something useful
   risk_tier: 1
   ---
   Run the following on: $1
   Include context: @context.txt
   Execute helper: !`date`
   ```

2. **Verify Expansion**
   - Commands support positional arguments (`$1`), bulk arguments (`$ARGUMENTS`), and placeholders (`{var}`).
   - File inclusion (`@file`) and shell execution (``!`cmd` ``) are supported.

3. **Check Drover Guard Rules**
   - If `DROVER_GUARD_URL` is set, ensure the command's `risk_tier` and action are allowed by the governing ReBAC policies in Drover Guard.

4. **Document**
   - Add to project's internal documentation or README.

---

### Extending the TUI

**When:** Adding new views, keyboard commands, status displays

**Files:**
- `internal/tui/model.go` — state machine
- `internal/tui/view.go` — rendering
- `internal/tui/styles.go` — colors/formatting
- `internal/tui/messages.go` — internal message types

**Bubble Tea patterns:**
```go
// New internal message type
type MyMsg struct {
    data string
}

// Update handler
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case MyMsg:
        // handle
        return m, nil
    }
    // ... other cases
}

// View contribution
func (m *Model) renderMyView() string {
    return lipgloss.NewStyle().
        /* styling */
        Render("content")
}
```

**Key state fields** (in Model):
- `inputBuffer` — current user input
- `conversationHistory` — messages to display
- `mode` — input/confirmation/error/normal
- `selectedToolIndex` — for permission prompt

---

### Adding a Permission Preset

**Steps:**

1. **Define in `internal/permissions/preset.go`**
   ```go
   var presets = map[string]PresetRules{
       "mypreset": {
           AllowedCategories: map[string]bool{
               "read": true,
               "git":  true,
           },
           RequireApproval: []string{"write"},
       },
   }
   ```

2. **Enable via environment**
   - User: `DROVER_CODE_PERMISSION_PRESET=mypreset`

3. **Document in README.md**

4. **Test**
   - Verify `engine.IsAllowed()` for each tool
   - Verify `NeedsPermission()` behavior

---

### Implementing a New Operation Mode

**Modes:** TUI (default), headless, bridge (IDE), webhook (GitHub), coordinator

**If adding new mode:**

1. **Add detection logic** in `cmd/drover-code/main.go`
   ```go
   if os.Getenv("MY_MODE_ENABLED") == "1" {
       return runMyMode(ctx, cfg)
   }
   ```

2. **Create mode entry function**
   ```go
   func runMyMode(ctx context.Context, cfg *config.Config) error {
       // Set up client, convo manager, agent
       // Run mode-specific logic
       return nil
   }
   ```

3. **Handle events**
   - Most modes read `agent.Events()` channel
   - Transform into mode-specific output format

4. **Test end-to-end**
   - Mock API responses (use FakeStream)
   - Verify event flow
   - Check output format

---

### Memory/Dream System Integration

**When:** Sessions end and need consolidation

**Already implemented** — developers rarely touch this. But if extending:

1. **Store interface** (`internal/dream/store.go`)
   ```go
   type Store interface {
       Save(e Entry) error
       Recent(n int) ([]Entry, error)
       Prune(retention Retention) error
   }
   ```

2. **Adding new backend:**
   - Implement `Store` interface
   - Update `OpenStore()` to detect backend type
   - Add environment variable flag

3. **Consolidation summarisation:**
   - Controlled by `consolidate()` in `worker.go`
   - Uses API client to summarise conversation
   - Non-blocking (buffered channel)

---

### GitHub Webhook Integration Points

**When:** Adding new GitHub actions or events

**Current flow:**
```
GitHub webhook → parser → runner → agent → comment post
```

**To add new action:**

1. **Update parser** (`internal/github/parser.go`)
   - Parse new event type
   - Extract action-specific data

2. **Update runner** (`internal/github/runner.go`)
   - Handle new event in `Run()`
   - Set up git context
   - Run agent
   - Post result

3. **Test**
   - Unit test parser with example payloads
   - Integration test with mock GitHub API

---

### Bridge (IDE) Extension Points

**Protocol:** LSP wire format (Content-Length + JSON-RPC 2.0)

**To add new RPC method:**

1. **Define handler** in `internal/bridge/bridge.go`
   ```go
   func (b *Bridge) handleMyMethod(ctx context.Context, msg Message, send SendFunc) error {
       // Parse params from msg.Params
       // Perform action
       // Send response
       send(msg.ID, "result", resultData)
       return nil
   }
   ```

2. **Register handler**
   ```go
   b.Register("drover/myMethod", b.handleMyMethod)
   ```

3. **Test**
   - Unit test with synthetic JSON-RPC messages
   - Integration test with bridge round-trip

---

### Coordinator Mode Extensions

**When:** Adding task decomposition strategies

**Current:**
```
1. Decompose(prompt) → []Task via API
2. Spawn workers per task
3. Merge results
```

**To extend:**

1. **Decomposition strategy** (`internal/coordinator/coordinator.go`)
   ```go
   func (c *Coordinator) Decompose(ctx context.Context, prompt string) ([]Task, error) {
       // Call API to decompose
       // Parse response → Task list
       // Return for worker distribution
   }
   ```

2. **Result merging**
   - Already handles multiple worker outputs
   - Consider order independence

3. **Failure handling**
   - Partial failures already retried
   - Can add fallback strategy

---

### Testing Integration Points

**Unit test template** (per tool):
```go
func TestMyToolSchema(t *testing.T) {
    tool := &MyTool{}
    schema := tool.InputSchema()
    // Verify it's valid JSON Schema
    var s interface{}
    if err := json.Unmarshal(schema, &s); err != nil {
        t.Fatal(err)
    }
}

func TestMyToolExecute(t *testing.T) {
    tool := &MyTool{WorkDir: t.TempDir()}
    input := []byte(`{"param":"value"}`)
    
    output, err := tool.Execute(context.Background(), input)
    if err != nil {
        t.Fatal(err)
    }
    
    // Assert output
    if !strings.Contains(output, "expected") {
        t.Error("unexpected output")
    }
}

func TestMyToolPermission(t *testing.T) {
    tool := &MyTool{}
    
    // Test that read-only operation needs no permission
    input := []byte(`{"path":"file.txt"}`)
    if tool.NeedsPermission(input) {
        t.Error("should not need permission for read")
    }
}
```

**Integration test template** (agent + tool):
```go
func TestAgentWithMyTool(t *testing.T) {
    // Set up fake API stream
    events := []api.StreamEvent{
        // ... simulate API response with tool call to MyTool
    }
    stream := &FakeStream{events: events}
    
    // Set up registry with MyTool
    reg := tools.NewRegistry()
    reg.Register(&MyTool{})
    
    // Run agent
    agent := agent.NewAgent(client, convo, reg)
    // ... collect events
    // ... verify tool was called
    // ... verify result was submitted
}
```

---

### Configuration Troubleshooting

**Common integration issues:**

| Issue | Check |
|-------|-------|
| Tool not found | `RegisterAll()` calls `r.Register()` |
| Schema invalid | `InputSchema()` returns valid JSON Schema |
| Permission denied | `NeedsPermission()` return value |
| Settings ignored | Check cascade order (env > local.json > project.json > home.json) |
| Context not injected | `ApplyRuntimeSettings()` called after load |
| TUI not starting | Check TTY detection + DROVER_CODE_HEADLESS env |

---

### Environment Variable Checklist

**Core:**
- [ ] `ANTHROPIC_API_KEY` (required)
- [ ] `ANTHROPIC_BASE_URL` (optional, for gateways)
- [ ] `ANTHROPIC_MODEL` (optional, overrides default)

**Modes:**
- [ ] `DROVER_CODE_HEADLESS=1` (force headless)
- [ ] `CLAUDE_CODE_IDE_BRIDGE=1` (IDE bridge mode)
- [ ] `CLAUDE_CODE_COORDINATOR_MODE=1` (coordinator mode)

**Permissions:**
- [ ] `DROVER_CODE_PERMISSION_PRESET=` (preset name)

**Dream:**
- [ ] `DROVER_CODE_DREAM_BACKEND=sqlite` (use SQLite)
- [ ] `DROVER_CODE_DREAM_MAX_ENTRIES=100` (retention cap)
- [ ] `DROVER_CODE_DREAM_MAX_AGE_DAYS=30` (age cap)

**Output:**
- [ ] `DROVER_CODE_JSONL=1` (JSON Lines output)
- [ ] `DROVER_CODE_RESULT_PATH=/tmp/result.json` (final result)

**GitHub Webhook:**
- [ ] `GITHUB_TOKEN` (required)
- [ ] `GITHUB_WEBHOOK_SECRET` (optional)
- [ ] `WEBHOOK_ADDR=:8080` (optional)

**UKC:**
- [ ] `UKC_TOKEN` (required for cloud tools)

---

### Build and Test Checklist

**Before committing:**

```bash
# Format
go fmt ./...

# Vet
go vet ./...

# Run all tests
CGO_ENABLED=0 go test ./...

# Fuzz specific test (30s)
go test -fuzz=FuzzMyThing -fuzztime=30s ./internal/mything

# Build both binaries
CGO_ENABLED=0 go build -o drover-code ./cmd/drover-code
CGO_ENABLED=0 go build -o ukc-agent ./cmd/ukc-agent

# Smoke test TUI
./drover-code < /dev/null  # exits immediately

# Smoke test headless
echo "test" | DROVER_CODE_HEADLESS=1 ./drover-code 2>&1 | head

# Smoke test webhook (requires API key)
./drover-code webhook &
PID=$!
sleep 1
kill $PID
```

---

### Performance Considerations

**Critical paths:**

| Operation | Optimization | Details |
|-----------|---|---|
| Stream parsing | Large scanner buffer | 1 MB max token size |
| Tool input accumulation | strings.Builder | Avoid intermediate allocations |
| Message snapshots | Copy-on-read | snapshots are CoW, not shared refs |
| Permission checks | Pre-computed categories | Registry caches definitions |
| Dream consolidation | Non-blocking channel | Drop if buffer full |
| Bridge message dispatch | Goroutine per request | Concurrent handler execution |

**Memory concerns:**
- Conversation history grows unbounded until compaction
- Coordinator spawns one goroutine per task
- UKC workspace sync buffers entire tar in memory (future: streaming)

---

### Documentation Update Checklist

**When adding a feature:**

- [ ] `README.md` — user-visible changes
- [ ] `available-tools.md` — new tools
- [ ] `interaction-guidelines.md` — behavioral changes
- [ ] Design doc — major architecture changes
- [ ] Code comments — non-obvious logic
- [ ] Godoc — exported functions/types
- [ ] This checklist — integration points

---

### Release Checklist

**Before releasing:**

```bash
# Update version in relevant places (if using versioning)
# Ensure all tests pass
go test -race ./...

# Build all targets
CGO_ENABLED=0 go build -o drover-code ./cmd/drover-code
CGO_ENABLED=0 go build -o ukc-agent ./cmd/ukc-agent

# Verify builds work
./drover-code --version  (or similar)
./ukc-agent --help

# Spot-check docs
grep -r "FIXME\|TODO\|XXX" internal/

# Confirm no debug code left
grep -r "debugLog\|fmt.Println" cmd/ | grep -v ".go.example"

# Smoke test key scenarios
# TUI
# Headless
# GitHub webhook (if changes)
# IDE bridge (if changes)
# Coordinator (if changes)

# Tag release in git
# Update CHANGELOG
```

---

*Last updated: As part of implementation guide*
*Keep this synchronized with actual code changes*
