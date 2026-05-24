# 12 — Property-based checks and fuzzing

**Status:** Implemented  
**Related:** `claude-go-test-spec.md`, `11-headless-orchestration.md`

---

## Goals

1. **Catch panics and crashes** on hostile or random inputs in small, security-relevant parsers and resolvers.
2. **Lock in invariants** with property-style tests (especially permission precedence).
3. **Stay local and deterministic in CI** — normal `go test ./...` stays fast; a separate workflow job runs each fuzz target for 30s on push/PR.

This doc does **not** adopt external chaos platforms (e.g. Antithesis). It uses the Go standard toolchain: `testing/quick` and Go 1.18+ `testing.F`.

---

## Scope

| Technique | Where | Invariant / intent |
|-----------|--------|---------------------|
| **Fuzzing** | `internal/api` SSE reader | `newStream` + `Next` / `Err` never panic on arbitrary bytes; bounded iteration. |
| **Fuzzing** | `internal/tools/toolutil` | `SafePath`: if it returns a path with non-empty `workDir`, result is under that work dir (or equals it). |
| **Fuzzing** | `internal/coordinator` | `extractJSON` never panics; returns a string (possibly `"[]"`). |
| **Fuzzing** | `internal/github` | `ParseWebhook` for `issue_comment` / `pull_request_review_comment` bodies; `extractMention` on arbitrary strings — no panics. |
| **Fuzzing** | `internal/config` | `mergeInto` on pairs of JSON objects that unmarshal to `Settings` — no panics. |
| **Fuzzing** | `internal/convo` | `FuzzConvoManager`: append / estimate / summarise / reset on NUL-split payloads — no panics; non-negative token estimate; reset clears history. |
| **Fuzzing** | `internal/tools` | `FuzzRegistryExecute`: `Execute` for a registered stub tool and for a missing name — no panics. |
| **Fuzzing** | `internal/undercover` | `FuzzIsInternalDomain`: arbitrary strings — no panics. |
| **Property test** | `internal/permissions` | With `ModeDefault`, if a tool name appears in `deniedTools`, `Check` returns `Deny` and does not call the prompt, even when the same tool is also allowed. |
| **Property test** | `internal/config` | `mergeInto`: a non-empty `model` from the second layer overwrites the first; empty second layer preserves the first. |
| **Property test** | `internal/convo` | `EstimatedTokens` ≥ 0; `Summarise` preserves message-count rules; `SetContextLimit` only applies for `limit > 0`; `Reset` clears history but keeps system prompt. |
| **Property test** | `internal/tools` | Registry round-trip for two distinct tools; `Execute` on unknown tool returns error. |
| **Property test** | `internal/undercover` | Internal-host classification is stable under suffix extension; normal `github.com/org/repo` remotes are not treated as internal Anthropic domains. |

---

## Non-goals

- Fuzzing the live Anthropic API or the full agent loop (needs keys and is non-deterministic).
- Fuzzing Bubble Tea TUI input (better suited to scripted `teatest` or manual QA).
- Continuous long-running fuzz beyond the fixed per-target time budget (use local `-fuzztime` or scheduled jobs if needed).

---

## How to run

```bash
# Unit + property tests (default CI)
CGO_ENABLED=0 go test ./...

# Fuzz API stream (stop with Ctrl+C or -fuzztime)
go test ./internal/api -fuzz=FuzzStreamNext -fuzztime=30s

# Fuzz SafePath
go test ./internal/tools/toolutil -fuzz=FuzzSafePath -fuzztime=30s

# Fuzz extractJSON
go test ./internal/coordinator -fuzz=FuzzExtractJSON -fuzztime=30s

# Fuzz GitHub webhook / mention parsing
go test ./internal/github -fuzz=FuzzParseWebhook -fuzztime=30s
go test ./internal/github -fuzz=FuzzExtractMention -fuzztime=30s

# Fuzz config merge
go test ./internal/config -fuzz=FuzzMergeInto -fuzztime=30s

# Fuzz conversation manager
go test ./internal/convo -fuzz=FuzzConvoManager -fuzztime=30s

# Fuzz tool registry Execute
go test ./internal/tools -fuzz=FuzzRegistryExecute -fuzztime=30s

# Fuzz undercover internal-domain classifier
go test ./internal/undercover -fuzz=FuzzIsInternalDomain -fuzztime=30s
```

CI (`.github/workflows/ci.yml`) runs the same targets in a dedicated **fuzz** job (`-fuzztime=30s` each).

After fuzzing, commit any files under `testdata/fuzz/` if you want to keep regression seeds.

---

## Implementation notes

- **Stream fuzz:** Caps iterations per input so a hypothetical `Next` bug cannot infinite-loop the fuzz worker.
- **SafePath fuzz:** Skips empty `workDir` (engine allows any absolute path in that mode — not a security boundary test).
- **Permissions property:** Uses `testing/quick` with a deterministic prompt stub; if `Check` returns `Deny` without invoking the prompt when the tool is denied, the property holds.

---

## Future extensions

- Scheduled workflow with a longer `-fuzztime` or corpus replay from `testdata/fuzz/`.
- Additional fuzz entry points (e.g. full `Loader` with temp dirs) if gaps show up in review.

---

*Previous: [`11-headless-orchestration.md`](./11-headless-orchestration.md)*  
*Next: [`13-test-coverage-plan.md`](./13-test-coverage-plan.md)*
