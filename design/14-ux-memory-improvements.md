# 14 — UX, perceived pauses, and memory

**Status:** Phases **A–C** and most of **B** are implemented in code; **D** is partial; **E** is optional env-tunable (see below). Dream store supports **opt-in SQLite** via env.

**Related:** [`02-agent-loop.md`](./02-agent-loop.md), [`07-tui.md`](./07-tui.md), [`09-advanced-systems.md`](./09-advanced-systems.md), [`01-foundation.md`](./01-foundation.md)

**Implementation snapshot**

| Phase | Item | In repo |
|-------|------|--------|
| **A** | A1–A3 Compaction visibility + TUI banner + `DROVER_CODE_DEBUG_COMPACTION` | Yes (`CompactionStartEvent` / `CompactionDoneEvent`, TUI, agent loop) |
| **B** | B1 Tunable context limit (`contextLimitEstimate` / `DROVER_CODE_CONTEXT_LIMIT_EST`) | Yes |
| **B** | B2 Tunable chars/token divisor + tests | Yes (`internal/convo`) |
| **B** | B3 API vs heuristic EMA; `/tokens` calibration line | Yes |
| **B** | B4 Project markdown caps + **ignore globs** | Yes: `projectMarkdownMaxBytes`, `projectMarkdownMaxFiles`, **`projectMarkdownIgnoreGlobs`** ([`internal/config/loader.go`](../internal/config/loader.go)) |
| **B** | B5 `/tokens` breakdown (system / messages / last user + **text vs tool-result** on last user) | Yes (`convo.ContextBreakdown`, `LastUserContentBreakdown`) |
| **C** | C1–C2 Dream consolidation + injection caps | Yes (`internal/dream/dream.go`) |
| **C** | Dream backend: **SQLite** | Opt-in: `DROVER_CODE_DREAM_BACKEND=sqlite` → `.claude/memory.db` ([`internal/dream/sqlite_store.go`](../internal/dream/sqlite_store.go)); default remains `memory.json`. Empty DB + existing `memory.json` → one-shot import, then JSON renamed to **`memory.json.imported`** (skip with `DROVER_CODE_DREAM_SKIP_JSON_IMPORT=1`). |
| **C** | Dream **retention** | Settings: `dreamMaxRetentionEntries`, `dreamMaxRetentionAgeDays`; env: `DROVER_CODE_DREAM_MAX_ENTRIES`, `DROVER_CODE_DREAM_MAX_AGE_DAYS`. Prune runs after consolidation **and at store open** when limits are active. |
| **D** | D1 System prompt nudge for deliverables | Partial (see `main` / system prompt) |
| **D** | D2 `/plan <path>` | Yes (TUI: `/plan path [topic…]` prefills write-to-path guidance) |
| **E** | Glamour / history display caps | Env: `DROVER_CODE_TUI_MAX_GLAMOUR_RUNES`, `DROVER_CODE_TUI_MAX_HISTORY_DISPLAY`; `NO_COLOR` disables rich styling where applicable |

**Auto-compaction:** There is no separate “enable” flag — before each agent turn the loop calls `ensureContextCompacted` when **auto-compaction is on** (default). It runs only if `EstimatedTokens() > contextLimit`. Tune *when* it fires with `contextLimitEstimate` / `DROVER_CODE_CONTEXT_LIMIT_EST`, or turn off automatic rounds with `disableAutoCompaction` or `DROVER_CODE_DISABLE_AUTO_COMPACTION` (manual `/compact` still works).

---

## Problem statement (from real use)

Users report **long stretches where the TUI appears idle** (“pausing”) and concerns that **memory / context is not used well**. Typical causes mix **UX** (no visible progress) with **engineering** (hidden work, coarse token estimates, large in-memory buffers).

Concrete example: the model **says** it will write an implementation plan but the user sees **no new file** — that is often **tool omission** (model never called `write_file`), not a crash. Still, if a **compaction** or **slow API** round-trip ran without feedback, the same session feels “stuck.”

---

## What the design docs already assume

| Doc | Relevant commitment |
|-----|---------------------|
| **02 — Agent loop** | Each turn calls `ensureContextCompacted` before streaming; compaction is an extra **non-streaming** model call. Token budget uses **rune count / N** heuristic (`convo`; default N=4). |
| **07 — TUI** | Responsiveness: first token quickly, tool spinners, loop never waits on UI. Compaction now surfaces via dedicated events + banner where wired. |
| **09 — Dream** | End-of-session summarisation; `BuildInjection` adds recent memories to system prompt. Consolidation and injection are **capped**; store can be JSON or SQLite. |

---

## Root causes (prioritised)

1. **Silent compaction** — Mitigated by Phase **A** (events + TUI + optional debug logging).
2. **Coarse context accounting** — Mitigated by tunable limit/divisor and optional API calibration (**B1–B3**).
3. **Dream RAM / payload size** — Mitigated by consolidation/injection caps (**C**); large session *counts* can use SQLite backend.
4. **No “artifact contract”** — Partially addressed in **D** (prompt / slash helpers); still probabilistic.
5. **TUI / Glamour** — Mitigated by env caps (**E**); lower priority than compaction visibility.

---

## Context engineering (why it feels hard)

“Context engineering” here means: **what text is in the model’s window**, in what order, and whether it stays under the real limit while still being useful. drover-code is honest but **coarse** in several places, so the *agent* can look like it “doesn’t manage context well” even when the code is working as written.

### Fixed system prompt (often huge)

- **`buildSystemPrompt`** adds identity + optional undercover + concatenated `CLAUDE.md` material from `config.Loader.loadProjectMarkdown()`.
- The loader walks **from `workDir` up to `$HOME`**, with **byte/file caps** and **`projectMarkdownIgnoreGlobs`** (e.g. skip `**/design/**` or `vendor/**`) via settings / `.claude/settings.json`.
- **Implication:** Operators can stop huge design trees from silently eating the window; there is still **no automatic summarisation** of those files.

### Conversation history vs heuristic budget

- **`convo.Manager`** tracks an **estimated** token count against a tunable limit. This is **not** the API’s tokenizer ([doc 02](./02-agent-loop.md) §1.4).
- **Compaction** replaces older turns with a **single summary** message; details can be lost.
- **`/tokens`** shows system vs all messages vs last user turn, and **last user text vs tool-result** share (useful after large tool dumps).

### Dream (session memory)

- **`BuildInjection`** appends recent dream entries into the **system** prompt again. **`consolidate`** input and per-entry/injection sizes are bounded.
- **`DROVER_CODE_DREAM_BACKEND=sqlite`** uses `.claude/memory.db` instead of loading all entries from `memory.json`.

### What users can do today

- Keep **one** lean root `CLAUDE.md`; use **ignore globs** for paths you do not want in the stack.
- Use **`/tokens`** for the heuristic footprint and calibration hint.
- Use **`/compact`** or **`/clear`** when the session drifts.
- For deliverables, **name the output path** explicitly.

---

## Original implementation plan (reference)

### Phase A — Visibility

| # | Task | Detail |
|---|------|--------|
| A1 | **Compaction events** | `CompactionStartEvent` / `CompactionDoneEvent` in the agent loop. |
| A2 | **TUI status** | Banner / status when compacting. |
| A3 | **Optional stderr** | `DROVER_CODE_DEBUG_COMPACTION=1`. |

### Phase B — Context and memory accuracy

| # | Task | Detail |
|---|------|--------|
| B1 | **Tunable limits** | `context_limit` / env. |
| B2 | **Safer heuristic** | Tunable divisor + tests. |
| B3 | **Optional true counting** | EMA from API `usage` vs heuristic. |
| B4 | **System prompt budget** | Caps + **`projectMarkdownIgnoreGlobs`**. |
| B5 | **`/tokens` breakdown** | System, messages, last user; last user **text vs tool-result**. |

### Phase C — Dream consolidation bounds + store

| # | Task | Detail |
|---|------|--------|
| C1 | **Cap consolidation input** | Bounded `consolidate` prompt. |
| C2 | **Cap injection size** | `BuildInjection` limits. |
| — | **SQLite store (scale)** | Opt-in via `DROVER_CODE_DREAM_BACKEND=sqlite`. |

### Phase D — Artifact expectations

| # | Task | Detail |
|---|------|--------|
| D1 | **System prompt fragment** | Prefer tools for repo writes when appropriate. |
| D2 | **Slash helper** | `/plan path [topic…]` in TUI. |

### Phase E — TUI performance (optional)

| # | Task | Detail |
|---|------|--------|
| E1 | **Glamour cap** | `DROVER_CODE_TUI_MAX_GLAMOUR_RUNES` (`0` = unlimited). |
| E2 | **History display cap** | `DROVER_CODE_TUI_MAX_HISTORY_DISPLAY`. |

---

## Dream — operator quick reference

| Mechanism | Values |
|-----------|--------|
| Enable | `dreamEnabled: true` in merged `.claude/settings.json` |
| Backend | Default: `.claude/memory.json`. `DROVER_CODE_DREAM_BACKEND=sqlite` → `.claude/memory.db` |
| JSON → SQLite | If the DB is **empty** and `memory.json` exists, entries are imported once; JSON is renamed to **`memory.json.imported`**. `DROVER_CODE_DREAM_SKIP_JSON_IMPORT=1` skips import. |
| Retention (settings) | `dreamMaxRetentionEntries`, `dreamMaxRetentionAgeDays` |
| Retention (env) | `DROVER_CODE_DREAM_MAX_ENTRIES`, `DROVER_CODE_DREAM_MAX_AGE_DAYS` (`0` = unlimited for that rule); overrides settings |
| Prune timing | After each consolidation **save**, and **once at store open** when any retention limit is active |
| Worker | `dream.NewWorker(store, client, dream.Retention{})` — pass limits via `Retention` or rely on settings/env merged in `main` |

---

## Coordinator robustness (related)

- Worker agents get **per-worker tool registries** and an **isolated directory** under `.drover-code-workers/` with a **`workspace` symlink** to the real project root (fallback to the main workdir if symlinks fail).
- Decompose parses **only string** array elements, trims, drops empties, and **caps at 8** subtasks.

---

## Implemented elsewhere (API rate limits)

- **`429` input TPM:** `internal/api.Client.StreamMessage` retries with backoff and `Retry-After` where present.

---

## Out of scope (for this doc)

- Changing default **model**.
- **Guaranteeing** the model always calls tools (fundamentally probabilistic).
- Full **git-style merge** of isolated worker trees (current coordinator still assumes non-overlapping subtasks for conflicting edits).

---

*Previous: [`13-test-coverage-plan.md`](./13-test-coverage-plan.md)*
