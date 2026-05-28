# Agent Context for Drover Code

Welcome, AI Agent. This file is intended to help AI coding assistants understand the structure, context, and conventions of the `drover-code` repository.

**Glossary:** [`CONTEXT.md`](CONTEXT.md). **Org index:** [`../AGENTS.md`](../AGENTS.md).

## Ecosystem Role

> **Part of the Drover Ecosystem**: `drover-code` serves as the **Core Agent Engine**. It is the fast, static Go binary that actually runs the agentic loop, calls the Anthropic API (via `drover-gateway`), and executes tools. It is orchestrated by `drover` and runs headlessly inside `drover-cloud` unikernels.

## What this repo is

## Layout

| Path | Role |
|------|------|
| `cmd/drover-code` | CLI entry: TUI, headless, `webhook`, flags |
| `cmd/ukc-agent` | HTTP agent for Unikraft Cloud instances (workspace sync & exec) |
| `internal/agent` | Agent loop, events |
| `internal/api` | HTTP client, SSE stream |
| `internal/bridge` | IDE bridge (JSON-RPC framing over stdio) |
| `internal/config` | Settings merge, `CLAUDE.md` / markdown injection |
| `internal/integrations/sqlforge` | Detect `sqlforge.yml`; inject SQLForge CLI guidance |
| `internal/convo` | Conversation state, compaction heuristics |
| `internal/coordinator` | Multi-worker coordinator mode |
| `internal/github` | Webhook server, parser, runner |
| `internal/tools` | Tool registry and implementations |
| `internal/tui` | Bubble Tea model and views |
| `internal/dream` | Session memory (JSON / SQLite) |
| `design/` | Design specs and roadmap (numbered `01-…`, test plan `13`, UX `14`) |
| `docs/` | User-facing docs (Tutorials, How-Tos, Reference, Explanation) |

## Build and test

```bash
CGO_ENABLED=0 go build -o drover-code ./cmd/drover-code
CGO_ENABLED=0 go build -o ukc-agent ./cmd/ukc-agent
CGO_ENABLED=0 go test ./...
```

CI uses Go 1.22 with `CGO_ENABLED=0`. Local `go.mod` may list a newer `go` directive; keep changes compatible with CI unless you bump the workflow.

Fuzz targets are listed in `.github/workflows/ci.yml` (`fuzz` job).

## Conventions

- Prefer focused changes: match existing style, imports, and error wrapping in touched packages.
- Property / fuzz tests: see `design/12-property-fuzz-testing.md` and doc `13-test-coverage-plan.md`.
- Do not assume Node: this is Go-only for the main binary.

## Product behavior pointers

- End-user overview, env vars, and modes: `README.md`.
- `internal/config` walks upward from `workDir` and merges `CLAUDE.md` files into the system prompt. If this repository is the working directory, **this** `CLAUDE.md` is included like any other project instructions file.
- When `sqlforge.yml` is found at a project root, SQLForge CLI guidance is appended automatically. How-to: `docs/how-to/sqlforge-from-drover-code.md`.

## Optional evals

Live Anthropic eval tests are opt-in (`RUN_AGENT_EVALS=1` and API key); see `evals/` and `README.md`.

## TUI Component Architecture (post dcode-001 migration)

The TUI was migrated from a god-model (~956–1290 LOC in model.go) to a proper componentized Bubble Tea design using a deliberate dual-state technique:

- Primary visual regions now have dedicated owners in `internal/tui/components/`:
  - `statusbar/` — always-visible bar (model, tokens, Guard risk level/reason)
  - `liveregion/` + `toolspinner/` — active tools + live streaming preview (owns ActiveTools, CompletedTools, StreamLines, Drain)
  - `historyview/` — scrollable conversation (owns viewport.Model + []core.RenderedTurn, AppendTurn, truncation banner)
  - `inputarea/` — textarea + autocomplete + queued message banner
  - `permissionprompt/` — single + batch permission prompts (with jsonPreview)

- `internal/tui/core/types.go` holds lightweight shared types (RenderedTurn, CompletedTool).
- `internal/tui/styles/colors.go` is the single source of truth for all Col* AdaptiveColors and common lipgloss styles (no more duplicated color definitions in components).
- `internal/tui/commandpalette/` provides semantic actions (ActionKey + Category + Shortcut + RiskLevel) beyond simple text injection; wired at Ctrl+K with overlay.
- Guard hooks are real: `pkg/guardclient`, `assessPermissionRisk` (file + bash dangerous patterns), `GuardRiskLevel/Reason` on Model, StatusBar renders risk state.

**Migration history (important for future edits):**
A safe dual-state period was used (legacy Model fields like m.history/m.activeTools/m.permPrompt lived alongside the component fields). All mutations hit both during transition. Once every call site + test was updated, legacy paths and fields were deleted in focused consolidation passes (HistoryView first, LiveRegion second, Permission + full permission.go deletion third). InputArea kept a lighter `syncInputArea()` bridge. Snapshots and 20+ history/fuzz/e2e tests never drifted. See `design/20-week-1-tui-component-migration.md` (Reality Record section) and beads dcode-001..009 for the full story.

When touching TUI code, prefer the component APIs. Update the component's isolated test + the integration snapshots. Do not re-introduce direct mutations on legacy fields that have been removed.
