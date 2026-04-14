# drover-code — agent / contributor notes

## What this repo is

Single static Go binary: Anthropic Messages API client with streaming, tools, Bubble Tea TUI, headless mode, IDE JSON-RPC bridge, optional coordinator mode, and GitHub webhook runner. Module path: `github.com/cloudshuttle/drover-code`.

## Layout

| Path | Role |
|------|------|
| `cmd/drover-code` | CLI entry: TUI, headless, `webhook`, flags |
| `internal/agent` | Agent loop, events |
| `internal/api` | HTTP client, SSE stream |
| `internal/bridge` | IDE bridge (JSON-RPC framing over stdio) |
| `internal/config` | Settings merge, `CLAUDE.md` / markdown injection |
| `internal/convo` | Conversation state, compaction heuristics |
| `internal/coordinator` | Multi-worker coordinator mode |
| `internal/github` | Webhook server, parser, runner |
| `internal/tools` | Tool registry and implementations |
| `internal/tui` | Bubble Tea model and views |
| `internal/dream` | Session memory (JSON / SQLite) |
| `design/` | Design specs and roadmap (numbered `01-…`, test plan `13`, UX `14`) |
| `docs/` | User-facing docs (e.g. Anthropic-compatible providers) |

## Build and test

```bash
CGO_ENABLED=0 go build -o drover-code ./cmd/drover-code
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

## Optional evals

Live Anthropic eval tests are opt-in (`RUN_AGENT_EVALS=1` and API key); see `evals/` and `README.md`.
