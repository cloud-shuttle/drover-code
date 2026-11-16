# Drover-Code TUI Test Strategy

**Version:** 1.0  
**Date:** May 13, 2026  
**Status:** Approved  
**Goal:** Build and maintain the most reliable, professional, and governed AI agent TUI in the industry.

-----

### 1. Executive Summary

Drover-Code’s TUI is built on **BubbleTea** (Go) and aims to combine **enterprise-grade governance** (via Drover Guard) with **excellent usability**.

We will adopt the strongest testing practices from:

- **OpenHands** → Snapshot + visual regression testing
- **Pi** → Extension-style, agent-driven testing
- **Aider** → Subprocess + output capture for integration tests
- **Plandex** → Focused interactive diff / hunk selection testing

This strategy ensures high confidence in TUI behavior, especially for complex features like Interactive Diffs, Custom Commands, and Input History.

-----

### 2. Learnings from Other Projects

|Project  |Key Strength                             |How We Apply It                                  |
|---------|-----------------------------------------|-------------------------------------------------|
|OpenHands|Snapshot + visual regression testing     |Golden master tests for markdown & diff rendering|
|Pi       |Extension-driven, agent-tested UI        |Test harness for custom commands                 |
|Aider    |Subprocess + output capture              |Robust E2E TUI interaction tests                 |
|Plandex  |Interactive diff / hunk selection testing|Dedicated tests for diff review flows            |

-----

### 3. Test Types & Coverage Goals

|Test Type         |Coverage Goal  |Framework / Tool                |Frequency|
|------------------|---------------|--------------------------------|---------|
|Unit Tests        |95%+           |Go `testing` + testify          |Every PR |
|Component Tests   |90%+           |BubbleTea message simulation    |Every PR |
|Snapshot / Visual |All major views|Golden files + custom comparator|Every PR |
|Integration Tests |Core flows     |Subprocess + output capture     |Daily    |
|E2E / Behavioral  |Critical paths |Real agent + TUI simulation     |Weekly   |
|Manual Smoke Tests|New features   |Human checklist                 |Release  |

-----

### 4. Comprehensive Test Suites

#### 4.1 Input & Navigation

- Input History (Up/Down + fuzzy search)
- Multi-line input & paste handling
- Draft preservation
- Command palette (`Ctrl+K`)
- Fuzzy history search (`Ctrl+R`)

#### 4.2 Interactive Diffs (High Priority)

- Hunk parsing correctness
- Visual rendering & selection
- Accept / Reject / Accept-All / Reject-All flows
- Safe patch application (no partial writes)
- Guard integration before/after apply

#### 4.3 Custom Commands

- Command loading & parsing (markdown + JSON)

#### 4.8 Component Tests (New — 2026)

With the introduction of the component architecture (see epic dcode-001 and `design/20-week-1-tui-component-migration.md`):

- Every component in `internal/tui/components/*/` must have a `_test.go` focused on `View()` output.
- Use table-driven tests with varying widths, states (busy, streaming, error), and content sizes.
- Tests must run in <100ms and require no `tea.Program`.
- Snapshot tests (`*_test.go` using golden files) remain the integrated visual contract.
- When adding a new component, add its isolated test in the same PR.

Example pattern (StatusBar / LiveRegion):
```go
func TestStatusBar_View(t *testing.T) {
    tests := []struct{ name string; bar statusbar.StatusBar; wantContains string }{
        {"idle", statusbar.StatusBar{ModelName: "claude-3-5", InputTokens: 1234}, "claude-3-5"},
        {"busy", statusbar.StatusBar{AgentBusy: true}, "● LIVE"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := tt.bar.View()
            if !strings.Contains(got, tt.wantContains) {
                t.Errorf("got %q, want contains %q", got, tt.wantContains)
            }
        })
    }
}
```

Run with:
```bash
go test ./internal/tui/components/... -count=1
```

See `design/20-week-1-tui-component-migration.md` for the Week 1 delivery plan that includes these tests.
- Template expansion (`$1`, `@file`, `!shell`)
- Guard evaluation for every command
- `/commands init` and `/commands list`

#### 4.4 Agent Interaction

- Message queueing while agent is busy
- Interrupt handling (`Ctrl+C`)
- Pause / Resume
- Live status updates

#### 4.5 Rendering & UX

- Markdown rendering (Glamour)
- Terminal resize handling
- Theme switching
- Long session stability (500+ turns)

-----

### 5. Implementation Plan (Next 4 Weeks)

**Week 1**

- Snapshot testing infrastructure for markdown & diffs
- Expand existing `model_test.go` with history & fuzzy search tests

**Week 2**

- Interactive Diffs dedicated test suite
- Subprocess-based E2E tests (inspired by Aider)

**Week 3**

- Custom Commands test harness
- Guard integration tests for commands

**Week 4**

- Full CI integration + visual regression checks

-----

### 6. Tools & Frameworks

- **Unit/Component**: Go `testing` + `testify` + BubbleTea message simulation
- **Snapshot**: Custom golden file comparator (or `github.com/sergi/go-diff`)
- **E2E**: `github.com/ory/dockertest` + subprocess execution
- **CI**: GitHub Actions with terminal capture

---

## Post-Migration Testing Reality (dcode-001 hygiene)

Component tests (e.g. historyview_test.go, liveregion_test.go, permissionprompt_test.go, statusbar_test.go, inputarea_test.go, toolspinner_test.go, strip_test.go) are table-driven, fast, and cover View() output for different widths/states/stream content without running a full tea.Program.

Integration truth remains the snapshot suite (snapshot_test.go) + model_test.go + builtin_test.go + permission_fuzz_test.go + e2e_test.go. These were kept green with **zero drift** across every dual-state step and every consolidation deletion pass (HistoryView, LiveRegion, Permission + permission.go removal).

When adding a new component or changing rendering:
1. Add/extend the isolated component _test.go (table-driven View cases).
2. Run the full snapshot + model tests; if they fail, the change has user-visible impact — investigate before updating goldens.

The dual-state technique (legacy + component fields co-existing) made it possible to update tests incrementally without ever having a broken tree. See design/20 Reality Record for the consolidation sequence.
