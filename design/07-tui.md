# 07 — Terminal UI: BubbleTea

**Package:** `internal/tui`  
**Files:** `model.go`, `view.go`, `styles.go`, `messages.go`, `permission.go`, `program.go`  
**Depends on:** `internal/agent`, `internal/api`, `internal/tools`, `charmbracelet/*`  
**Depended on by:** `cmd/drover-code`

---

## Purpose

The TUI package turns the agent loop's event stream into a live terminal interface. It owns everything visual: the scrollable history of past turns, the streaming live region where current output appears token by token, the tool spinners, the permission prompt overlay, the status bar, and the input area with slash command autocomplete.

The design goal is responsiveness. The user should see the first token of the model's response in milliseconds, tool activity in real time, and a permission prompt that appears instantly when needed. The agent loop never waits for the UI to render.

---

## 1. Architectural Foundation: BubbleTea vs React/Ink

The original Claude Code uses React with the Ink library — a React renderer that targets the terminal rather than the DOM. Go does not have React. The closest equivalent is BubbleTea, which implements the Elm architecture.

### 1.1 The Elm architecture

Elm (and BubbleTea) programs have three pure functions:

```
init()               → (Model, Cmd)
update(Model, Msg)   → (Model, Cmd)
view(Model)          → string
```

`Model` is all state. `Msg` is all events. `Cmd` is all side effects (I/O operations that run off the main goroutine and deliver results as `Msg`s). `view` is a pure function from state to a rendered string. There is no mutation, no callbacks, no shared mutable state between components.

This is architecturally cleaner for a terminal than React because:

**Single goroutine for all state changes.** `Update` is called sequentially on BubbleTea's main goroutine. Race conditions in the state machine are impossible by design.

**No virtual DOM diffing.** `View` returns a complete string on every frame. BubbleTea diffs the string against the previously rendered string to produce minimal terminal writes.

**Explicit I/O.** All I/O happens through `Cmd` — a function that runs on a separate goroutine and returns a `Msg`. The streaming API response, tool executions, and permission prompts all flow through this single channel.

### 1.2 Synchronous vs BubbleTea

Traditional synchronous loop:

```go
for {
    input    := readLine()      // blocks
    response := callAPI(input)  // blocks — UI frozen
    print(response)
}
```

BubbleTea equivalent:

```go
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        if msg.Type == tea.KeyEnter {
            return m, runAgent(m.input)  // starts goroutine, returns immediately
        }
    case agentMsg:
        return m.handleAgentEvent(msg.event), waitForEvent(m.eventCh)
    }
}
```

`runAgent` starts a goroutine and returns immediately. The UI stays responsive. Events arrive as `agentMsg` messages processed one at a time.

---

## 2. Model Structure

```go
type Model struct {
    width, height int

    viewport    viewport.Model
    history     []renderedTurn
    viewportBuf strings.Builder

    streaming   bool
    streamBuf   strings.Builder
    streamLines string

    activeTools map[int]*activeTool
    toolOrder   []int
    pendingDone []completedTool

    textarea     textarea.Model
    inputFocused bool

    autoList  []slashItem
    autoIndex int
    showAuto  bool

    permPrompt *permissionPrompt

    glamourRenderer *glamour.TermRenderer

    eventCh <-chan agent.Event
    runFunc RunFunc

    modelName         string
    totalInputTokens  int
    totalOutputTokens int
    agentBusy         bool

    lastError string
}
```

### 2.1 Two text buffers

**`streamBuf`** accumulates raw markdown as tokens arrive. On `DoneEvent` it is glamour-rendered and committed to history.

**`streamLines`** is a plain-text preview for the live region. Glamour cannot process partial markdown (mid-codeblock, mid-table produces garbled output), and calling it on every text delta would be too slow. The live region shows raw text; on `DoneEvent` the full buffer is rendered once.

### 2.2 `activeTools` and `toolOrder`

Map semantics handle out-of-order completion. `toolOrder` provides stable insertion-order iteration for rendering — Go map iteration is random.

### 2.3 `pendingDone`

Buffers completed tool results while other tools are still running. Flushed to history on `DoneEvent` so all results appear together with the final response.

---

## 3. The Event Pump

### 3.1 `waitForEvent`

```go
func waitForEvent(ch <-chan agent.Event) tea.Cmd {
    return func() tea.Msg {
        ev, ok := <-ch
        if !ok {
            return agentMsg{event: agent.DoneEvent{}}
        }
        return agentMsg{event: ev}
    }
}
```

Returns a `tea.Cmd` that blocks until an event arrives. BubbleTea runs it on a goroutine. When the event arrives, it's wrapped in `agentMsg` and returned to `Update()`.

**The pump chain:** After every `agentMsg`, `Update` returns a new `waitForEvent`:

```go
case agentMsg:
    cmd := m.handleAgentEvent(msg.event)
    cmds = append(cmds, waitForEvent(m.eventCh))  // re-arm
    return m, tea.Batch(cmds...)
```

When the channel closes, `waitForEvent` returns a synthetic `DoneEvent` and the chain stops.

**Why not `program.Send()`?** Creates a hard dependency between the agent loop and the `tea.Program`. The channel approach keeps the loop independent.

**Why not `tea.Tick`?** A ticker polls at fixed intervals, introducing latency. The blocking receive delivers events the instant they arrive.

### 3.2 Spinner ticks

```go
case spinner.TickMsg:
    for idx, at := range m.activeTools {
        var cmd tea.Cmd
        m.activeTools[idx].spinner, cmd = at.spinner.Update(msg)
        cmds = append(cmds, cmd)
    }
    return m, tea.Batch(cmds...)
```

Each `spinner.Update()` returns the next tick command. When `activeTools` is empty, no more tick commands are issued — spinners stop automatically.

---

## 4. Layout: View()

### 4.1 Layout sections

```
┌──────────────────────────────────────────────────┐
│  viewport.View()                                 │  ← glamour-rendered history
│  (scrollable: pgup/pgdn/mouse)                   │
├── live region (amber left border) ───────────────┤
│  ⠋ read_file: src/auth.go                       │  ← active spinners
│  Hello, I'll refactor the auth...               │  ← streaming text (last 12 lines)
├──────────────────────────────────────────────────┤
│  claude-sonnet ····· ● in:1.2k out:89            │  ← status bar
├──────────────────────────────────────────────────┤
│ ╭────────────────────────────────────────────╮   │
│ │ Message…                                   │   │  ← textarea or permission prompt
│ ╰────────────────────────────────────────────╯   │
└──────────────────────────────────────────────────┘
```

Viewport gets all space not reserved by bottom sections. When the permission prompt replaces the textarea, the reserved height changes and the viewport expands.

### 4.2 Status bar

Uses a fill technique to push right-aligned content to the right edge:

```go
usedWidth := lipgloss.Width(left) + lipgloss.Width(centre) + lipgloss.Width(right)
fill := styleStatusBar.Width(m.width - usedWidth).Render("")
return lipgloss.JoinHorizontal(lipgloss.Top, left, fill, centre, right)
```

`lipgloss.Width()` measures rendered width including ANSI codes — fill calculation is accurate at all terminal widths.

Token format: `1500` → `1.5k`, `23400` → `23k`, `1200000` → `1.2m`.

---

## 5. History and the Viewport

### 5.1 Pre-rendered turns

Turns are pre-rendered strings. Once committed, they never change. `View()` is cheap — just concatenate pre-rendered strings. Glamour runs once per turn, not on every frame.

Resize requires re-rendering all turns (glamour word-wraps to terminal width). `rebuildViewport()` handles this after every resize.

### 5.2 Glamour

```go
r, _ := glamour.NewTermRenderer(
    glamour.WithAutoStyle(),           // auto light/dark mode
    glamour.WithWordWrap(m.width - 4),
)
```

`WithAutoStyle()` detects terminal background and chooses colours automatically. Recreated on every resize; all history re-rendered at the new width.

### 5.3 User vs assistant

User messages: plain text, subtle background fill. Not glamour-rendered — asterisks and backticks in user input should not be interpreted as markdown.

Assistant messages: glamour-rendered. Completed tool rows appear above the response text.

---

## 6. Permission Prompt

### 6.1 Rendering

Amber/warning border. Shows: title, tool name, human-readable input preview, key hints.

`jsonPreview` extracts the most readable field (`command`, `path`, `query`, `pattern`, `url`, `content`) — shows `command: go test ./...` rather than raw JSON.

### 6.2 Input interception

All keypresses go to the permission handler while `m.permPrompt != nil`. The user cannot type in the textarea while a permission prompt is waiting.

`y`/`Y` → Allow, `a`/`A` → AlwaysAllow, `n`/`N`/Esc → Deny.

### 6.3 Blocking agent goroutine

The agent loop goroutine blocks on `respCh` while the prompt is shown. For parallel tool calls: the permission-needing goroutine blocks; others run freely. Prompts are shown one at a time.

---

## 7. Slash Command Autocomplete

**Trigger:** input starts with `/` and contains no space.  
**Hide:** space is typed (now typing arguments).

Navigation: `↑`/`↓` navigate, `Tab` completes (adds trailing space), `Esc` closes.

**Tab not Enter:** Enter submits. Tab lets users type arguments before submitting.

**Dropdown above input** — input stays anchored to bottom. Matches `fzf` convention.

---

## 8. Styles and Theme

### 8.1 Adaptive colours

Every colour uses `lipgloss.AdaptiveColor{Light: "...", Dark: "..."}`. Works in dark and light terminals without configuration.

### 8.2 Colour vocabulary

| Category | Use |
|---|---|
| `colBase` | Primary text |
| `colMuted` | Secondary text, labels |
| `colAccent` | Model name, active border, tool names |
| `colAccentDim` | Live region border, pending tools |
| `colSuccess` | Completed tool ✓ |
| `colError` | Failed tools, error banner |
| `colWarning` | Permission prompt border |

One accent colour (amber). No gradients, no shadows. The content is the UI.

---

## 9. Circular Dependency Resolution

`Model` needs `loop.Run()`. `Loop` needs `eventCh`. `eventCh` is created by `Model`.

Solution: post-construction injection via `SetRunFunc(fn RunFunc)`.

```go
eventCh := make(chan agent.Event, 512)
loop    := agent.NewLoop(client, mgr, registry, permitFn, eventCh)
model   := tui.New(eventCh, modelName)

model.SetRunFunc(func(input string) tea.Cmd {
    return runAgent(func() error { return loop.Run(ctx, input) })
})
```

---

## 10. Permission Function Factory

```go
func makePermitFn(ctx context.Context, eventCh chan<- agent.Event) tools.PermissionFunc {
    return func(callCtx context.Context, req tools.PermissionRequest) tools.Decision {
        respCh := make(chan agent.PermissionDecision, 1)  // buffered — critical

        select {
        case eventCh <- agent.PermissionRequestEvent{..., DecisionCh: respCh}:
        case <-callCtx.Done():  return tools.Deny
        case <-ctx.Done():      return tools.Deny
        }

        select {
        case d := <-respCh:     return mapDecision(d)
        case <-callCtx.Done():  return tools.Deny
        case <-ctx.Done():      return tools.Deny
        }
    }
}
```

`respCh` buffered (size 1): the TUI sends the decision and immediately clears `m.permPrompt`. If unbuffered and the agent goroutine had timed out, the TUI's send would block — deadlock. Buffer of 1 ensures TUI can always complete without blocking.

Two `select` statements each with two cancellation arms — handles all termination cases cleanly.

---

## 11. Testing Strategy

BubbleTea models are testable without a terminal by calling `Update` and `View` directly:

```go
model := tui.New(make(chan agent.Event, 10), "test")
model.width, model.height = 80, 24

// Text delta
model.Update(agentMsg{event: agent.TextDeltaEvent{Text: "Hello"}})
assert(t, model.streamBuf.String() == "Hello")

// Tool spinner created and removed
model.Update(agentMsg{event: agent.ToolStartEvent{CallIndex: 0, Name: "bash"}})
assert(t, model.activeTools[0] != nil)
model.Update(agentMsg{event: agent.ToolDoneEvent{CallIndex: 0}})
assert(t, model.activeTools[0] == nil)

// DoneEvent commits to history
model.Update(agentMsg{event: agent.DoneEvent{}})
assert(t, len(model.history) == 1)
assert(t, model.streamBuf.Len() == 0)

// Permission prompt intercepts keys
respCh := make(chan agent.PermissionDecision, 1)
model.permPrompt = &permissionPrompt{toolName: "bash", decisionCh: respCh}
model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
assert(t, model.permPrompt == nil)
assert(t, <-respCh == agent.PermAllow)

// Autocomplete triggers on '/'
model.textarea.SetValue("/")
model.updateAutoComplete()
assert(t, model.showAuto == true)
model.textarea.SetValue("/clear ")
model.updateAutoComplete()
assert(t, model.showAuto == false)
```

---

## 12. Edge Cases and Known Issues

**Resize during streaming.** In-progress `streamBuf` continues at old glamour width until `DoneEvent`. Committed history re-renders at new width immediately. Brief inconsistency is acceptable.

**Very long streaming lines.** Plain-text preview doesn't word-wrap. Long URLs or minified JSON truncated by terminal. Final rendered version wraps correctly.

**Narrow terminals (< 40 cols).** Spinner + name + summary may not fit. No truncation currently.

**Concurrent permission prompts.** Shown one at a time. Second prompt's event waits in the channel buffer. Works correctly as long as the buffer (512 events) doesn't overflow.

---

## 13. Future Considerations

**Streaming glamour.** Render partial markdown incrementally. Non-trivial — glamour's parser needs a complete AST. Would require a streaming markdown renderer.

**Copy mode.** Vim-style selection in viewport with `c` to enter, hjkl to move, `y` to yank. `viewport.Model` supports selection — needs integration.

**Split-pane layout.** Conversation left, file diff right. Right pane updates as `edit_file` calls complete.

**Session persistence.** Save and restore rendered history across invocations.

---

*Previous: [`06-git-web-tools.md`](./06-git-web-tools.md)*  
*Next: [`08-config-permissions-undercover.md`](./08-config-permissions-undercover.md)*

---

## Post-Migration Component Structure (dcode-001, 2026-05)

After the component migration (dual-state extraction followed by ownership consolidation passes):

- `internal/tui/components/statusbar/` — StatusBar (risk/Guard aware)
- `internal/tui/components/liveregion/` + `toolspinner/` — live activity + tool spinners (sole owner of active/completed tools + streaming preview)
- `internal/tui/components/historyview/` — conversation history (sole owner of viewport + RenderedTurn list)
- `internal/tui/components/inputarea/` — textarea + autocomplete + queue banner (sync bridge from legacy fields still present)
- `internal/tui/components/permissionprompt/` — single + batch prompts (jsonPreview internal)

`internal/tui/core/types.go` + `styles/colors.go` (central Col* + lipgloss styles) + `commandpalette/` (semantic actions with Category/Shortcut/RiskLevel) complete the layer.

Model is now primarily orchestration. View() composes the four regions + overlays (palette, diff, search, permission). Legacy fields for history/live/permission were deleted after every call site and test was updated. See `design/20-week-1-tui-component-migration.md` (Reality Record) and beads dcode-001/002–009 for the full dual-state → consolidation story. All snapshots remained stable throughout.
