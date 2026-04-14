# 13 — Test coverage plan

**Status:** In progress (Phases 1–5 materially implemented; iterate on remaining gaps)  
**Related:** [`12-property-fuzz-testing.md`](./12-property-fuzz-testing.md), [`11-headless-orchestration.md`](./11-headless-orchestration.md)

**Last coverage check:** `CGO_ENABLED=0 go test ./... -coverprofile=/tmp/cover.out -count=1` then `go tool cover -func=/tmp/cover.out | tail -1` → total statements **61.4%** (2026-04-13). Highlights: **`internal/tui` ~42%**, **`internal/bridge` ~61%**, **`internal/github` ~76%**, **`internal/agent` ~79%**.

**Recent test additions:** `internal/bridge` — **`TestBridge_readMessage_contentLengthLongerThanBody`**; `internal/coordinator` — **worker `context.Canceled` / `DeadlineExceeded` propagates via errgroup** + **`TestCoordinator_Execute_ContextCanceledDuringWorker`**; `internal/github` — post-**`Shutdown`** health request fails in **`TestHTTPServer_ServeAndShutdown`**. **Next:** webhook **background jobs** vs `Shutdown` (document: HTTP stops, jobs may still run until timeout); coordinator **partial worker results** on cancel; bridge **oversized body** with valid framing.

---

## Goals

Raise **overall** statement coverage and eliminate **0%** packages without chasing meaningless 100%. Prefer tests that lock in **behavior and invariants** over line coverage.

Property and fuzz work is tracked in **doc 12** and CI; this doc is the **roadmap** for the rest.

---

## What “reasonable” means

For a Go CLI with a TUI, live network, and `git` subprocesses:

| Target | Aim |
|--------|-----|
| **Overall** | ~**55–65%** on `go test ./... -cover` after planned work |
| **Floor** | No production package **stuck at 0%** indefinitely |
| **TUI** | Do not hold `internal/tui` to the same bar; target **model-level** tests (~**40–45%** package with current suite) |

UI glue and `main` will always drag the average; that is expected.

---

## Phase 1 — Fast wins (pure logic, high ROI)

| Item | Package | Notes | Status |
|------|---------|--------|--------|
| Conversation state | `internal/convo` | Token estimate, compaction budget, `Summarise`, `Reset`, thread-safety via API | **Done:** property + fuzz + high line coverage |
| Registry + registration | `internal/tools` | `Register` / `Definitions` / `Execute`, `RegisterAll` tool list | **Done:** `register_all_test.go` (16 tools, sorted names), registry property/fuzz |
| Undercover remotes | `internal/undercover` | `isInternalDomain`, `Detect` (git-backed) | **Done:** property + fuzz + `detect_test.go` (temp git repos) |

---

## Phase 2 — Subprocess / filesystem tools

| Item | Package | Approach |
|------|---------|----------|
| Git tools | `internal/tools/git` | **Done (initial):** extended `git_test.go` — diff / clean tree, add/commit, branch, validation errors. |
| Path helpers | `internal/tools/toolutil` | **Done (initial):** `util_test.go` — `Truncate`, `SafePath`, `WriteAtomic`, `Schema`. |
| FS tools | `internal/tools/fs` | **Done (initial):** missing file, bad JSON, bad line range, path escape. |

---

## Phase 3 — Bridge and headless `main`

| Item | Package | Approach |
|------|---------|----------|
| JSON-RPC loop | `internal/bridge` | **Improved:** unknown method, ping via `io.Pipe`; **`readMessage`** missing `Content-Length`, bad JSON body. |
| CLI glue | `cmd/drover-code` | **Improved:** `main_helpers_test.go` — `headlessExitCode`, `coalesce`, `buildSystemPrompt`, `wantsHeadlessMode`; `internal/config/apply_runtime_test.go` — context limit + chars-per-token (env overrides); `headless_result_test` — `envOptionalBool`, `gitWorkspaceHead`. |
| Coordinator | `internal/coordinator` | **Improved:** `Execute` end-to-end; **decompose** fallback when model returns non-`[]string` JSON (e.g. `[1,2,3]`). |

---

## Phase 4 — TUI (model, not pixels)

| Item | Package | Approach |
|------|---------|----------|
| Bubble Tea model | `internal/tui` | **Improved:** agent + compaction + tools; permissions; **`/plan`** path + multi-word topic; autocomplete; `compactCompleteMsg`; heartbeat no-op. **Remaining:** deeper multi-turn / edge cases if desired. |

---

## Phase 5 — GitHub + telemetry

| Item | Package | Approach |
|------|---------|----------|
| Webhook / client | `internal/github` | **Done (core integration):** `httptest` client + **`Runner.run`** + **`Runner.Handle`** (POST comment → agent → PATCH update); PATCH failure after successful run; placeholder POST failure; **`Runner.cloneRepo`** `file://`; server/parser fuzz + tests. |
| Export / env | `internal/telemetry` | **Improved:** `Noop` + ingestion batch + **`context_test`** (span ID, nil tracer); `ConfigFromEnv` (incl. default host); disabled tracer flush; **ingestion HTTP 4xx** flush does not panic. |

---

## Process / guardrails

1. Track **overall + per-package** coverage periodically (`go test ./... -cover`). Optional: CI artifact or non-blocking report before any enforced threshold.
2. Prefer **invariants** (errors, counts, tool lists) over line hunting.
3. **Property + fuzz:** follow [`12-property-fuzz-testing.md`](./12-property-fuzz-testing.md); add new `Fuzz*` targets to **`.github/workflows/ci.yml`** when you add meaningful entry points.
4. **`evals`** — unit tests in `score_test.go` always run; **`TestAgentEvals`** (live Anthropic) is **opt-in**: `RUN_AGENT_EVALS=1` plus API key and usually **`ANTHROPIC_MODEL`** (the baked default may 404 if the id is unavailable for your key).

---

## Suggested execution order

1. **Optional polish:** multi-turn TUI, `drover/execute` bridge integration tests, webhook edge cases.  
2. Re-run **Milestone check** coverage after meaningful test batches.

---

## Milestone check

Rough targets:

- **M1 (~55% overall):** Phases 1–2 materially done, bridge started — **55.5%** historical baseline.  
- **M2 (~65% overall):** Phases 3–5 in good shape; TUI at model-test bar — latest total **61.4%** (see top of doc).

Re-run coverage from repo root:

```bash
CGO_ENABLED=0 go test ./... -coverprofile=/tmp/cover.out -count=1
go tool cover -func=/tmp/cover.out | tail -1
```

---

*Previous: [`12-property-fuzz-testing.md`](./12-property-fuzz-testing.md)*  
*Next: [`14-ux-memory-improvements.md`](./14-ux-memory-improvements.md)*
