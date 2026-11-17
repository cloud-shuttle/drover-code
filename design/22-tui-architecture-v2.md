# TUI Architecture v2 – What Changed + How to Test

**Date:** 2026-05  
**Status:** Merged to `main` as commit `418dc85` (feature branch deleted)

This document summarizes the major TUI refactoring completed as part of the dcode-001 epic. It is intended for the broader team (drover, drover-cloud, etc.) who will be using or extending the TUI.

## What Changed

### Before (God Model)
- `internal/tui/model.go` had grown to ~1,200+ lines.
- Most UI state and rendering logic lived directly in the main `Model`.
- Live region, history, input, permission prompts, and status were all tightly coupled with lots of manual layout and state mutation.
- Adding new features (especially anything visual or interactive) was becoming increasingly painful.

### After (Component Architecture)
The TUI has been broken into a small set of focused, reusable components:

| Component          | Responsibility                                      | Key Files |
|--------------------|-----------------------------------------------------|---------|
| `statusbar`        | Model name, token counts, busy state, **Guard risk** | `components/statusbar/` |
| `liveregion` + `toolspinner` | Active tools + live streaming output             | `components/liveregion/`, `toolspinner/` |
| `historyview`      | Scrollable conversation history + truncation      | `components/historyview/` |
| `inputarea`        | Textarea, slash autocomplete, message queue banner | `components/inputarea/` |
| `permissionprompt` | Single + batch permission prompts                 | `components/permissionprompt/` |
| `commandpalette`   | Ctrl+K palette with semantic actions              | `commandpalette/` |

**Core improvements:**
- Clear ownership (no more dual-state mutation hell for most areas).
- Styles centralized in `internal/tui/styles/colors.go`.
- Much easier to reason about and extend individual regions.
- Model is now primarily orchestration + high-level behavior.

## New Features

### 1. Command Palette (Ctrl+K)
- Replaces/augments slash commands with a searchable palette.
- Supports two types of entries:
  - **Text commands**: Inject `/command ` into the input (backward compatible).
  - **Semantic actions**: Execute directly (e.g. "compact", "clear").
- Rich metadata supported: `Category`, `Shortcut`, `RiskLevel`.

**Extensibility (new) — Recommended Path:**
External layers should use the following APIs instead of (or in addition to) the older `RegisterCustomCommands`:

```go
// Static rich commands
model.RegisterPaletteCommands([]commandpalette.Command{...})

// Dynamic / context-aware commands (called every time palette opens)
model.RegisterPaletteProvider(func() []commandpalette.Command { ... })

// Own execution of semantic actions (enables "execute directly" behavior)
model.RegisterPaletteActionHandler("my-action", func(key string) tea.Cmd { ... })
```

**Reference Implementation:**
The `program.go` integration now demonstrates the full pattern:
- A `CommandProvider` that surfaces loaded custom commands with `Category`, `RiskLevel` (derived from `RiskTier`), and `ActionKey`.
- `RegisterPaletteActionHandler` registrations so selecting a custom command executes it directly via the existing `runFunc` path (no manual Enter required).

This is the recommended model for drover, drover-cloud, and other consumers.

**Current hardcoded semantic actions:**
- `compact`, `clear`, `tokens`, `model`

### Migration & Integration Guide for External Layers

- **Slash autocomplete** (basic): Continue using `RegisterCustomCommands(names, descs)`. This path is preserved.
- **Rich palette experience** (recommended): Use the three new APIs above. This is the only way to get `Category`, `Shortcut`, `RiskLevel`, and direct semantic execution.
- Custom commands can now appear with risk-aware coloring in the palette and execute without the user pressing Enter after selection.
- If you previously injected commands only for text, consider migrating them to use `ActionKey` + handler for a much better UX.

### 2. Real Guard Integration
- `assessPermissionRisk()` now performs deeper analysis (sensitive files + dangerous bash patterns).
- `SetGuardRisk(level, reason)` allows external code (drover-guard, etc.) to push risk signals.
- StatusBar now renders risk state:
  - `● CAUTION (reason)` (yellow)
  - `● HIGH (reason)` (red)
- Risk also flows into the Command Palette metadata.

## How to Test

### Command Palette
- Press `Ctrl+K` while the agent is idle.
- Try typing to filter.
- Select a slash command → it should inject text.
- Select "clear" or "compact" → they should execute directly.
- Test with long lists of custom commands (via `RegisterCustomCommands`).
- **New extensibility path**: Verify that commands registered via `RegisterPaletteProvider` appear dynamically and that `RegisterPaletteActionHandler` causes direct execution (no extra Enter press). Check that `RiskLevel` metadata affects display.

### Guard Risk
- Trigger a permission request on a sensitive file (`.env`, `Dockerfile`, `/etc/...`, etc.) → StatusBar should show high/caution risk.
- Trigger a dangerous shell command (e.g. `rm -rf`) → high risk.
- Send an error containing "Governance Policy" or "Drover Guard" → high risk should appear.
- Risk should clear on normal activity or explicit reset.

### General Regression
- All existing snapshot tests must continue to pass (they have so far with zero drift).
- Manual smoke: long streaming, tool activity, permission prompts (single + batch), history recall (Up/Down), paste handling.

## Known Limitations / Work in Progress

- Command Palette has a significantly expanded extension API:
  - `RegisterPaletteCommands([]commandpalette.Command)` — static rich commands
  - `RegisterPaletteProvider(func() []commandpalette.Command)` — dynamic/context-aware commands
  - `RegisterPaletteActionHandler(key, handler)` — own execution of custom semantic actions

  This allows external systems (drover, custom command frameworks, etc.) to deeply integrate without modifying core TUI code.
- Guard risk is currently only evaluated at permission request time (deeper result-based risk is planned).
- Some coverage gaps remain (notably `renderMarkdown`).
- The palette and risk UI are functional but will likely receive polish based on real usage feedback.

## Feedback

Please dogfood this and report:
- Things that feel worse than before
- Missing power features in the Command Palette
- Guard risk false positives/negatives
- Any surprising behavior in the new components

We're deliberately **not** cutting a tagged release yet. The goal for the next 1–2 weeks is to merge to `main`, gather real usage feedback, and address the highest-leverage gaps (starting with Command Palette extensibility).

---

**Related artifacts:**
- Beads epic: `dcode-001`
- Design docs: `design/20-week-1-tui-component-migration.md`, `design/21-tui-component-implementation-roadmap.md`
- Main implementation: `internal/tui/model.go`, `view.go`, and `components/`
- Merge notes: `MERGE_TUI_V2.md` and `MERGE_COMMANDS.md`
- PR description: `PR_DESCRIPTION_TUI_V2.md`

Feedback and questions welcome.