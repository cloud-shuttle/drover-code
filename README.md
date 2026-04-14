# drover-code

A single static Go binary that runs an agentic coding assistant against the Anthropic Messages API (streaming, tools, Bubble Tea TUI). No Node or Bun.

## Requirements

- Go 1.22+
- Anthropic-compatible API access: `ANTHROPIC_API_KEY`, or `ANTHROPIC_AUTH_TOKEN` (see [docs/anthropic-compatible-providers.md](docs/anthropic-compatible-providers.md))
- Optional: `ANTHROPIC_BASE_URL` for non-Anthropic gateways (same doc)
- `git` on `PATH` if you use git tools

## Build

```bash
CGO_ENABLED=0 go build -o drover-code ./cmd/drover-code
```

## Run

**Interactive TUI** (from a project directory):

```bash
export ANTHROPIC_API_KEY=sk-ant-...
./drover-code
```

**Headless** (stdin, non-interactive stdout):

```bash
echo "Summarise the README" | ANTHROPIC_API_KEY=sk-ant-... ./drover-code
```

**Model** — default is baked into the binary; override with:

```bash
export ANTHROPIC_MODEL=claude-haiku-4-5-20251001
```

or `"model"` in `~/.claude/settings.json` or `.claude/settings.json`.

## Other LLM providers (Moonshot, GLM, …)

drover-code targets the **Anthropic Messages API** wire format. Gateways that implement the same protocol can be used with `ANTHROPIC_BASE_URL` and the provider’s key.

**Setup, examples, and troubleshooting:** [docs/anthropic-compatible-providers.md](docs/anthropic-compatible-providers.md)

## Configuration

Settings merge in order:

1. `~/.claude/settings.json`
2. `<workdir>/.claude/settings.json`
3. `<workdir>/.claude/settings.local.json`

`CLAUDE.md` files under the working tree are concatenated and injected into the system prompt.

Permission rules and optional `dream` memory use the same `.claude` layout as other Claude Code–compatible clients.

### Dream (session memory)

Enable with `"dreamEnabled": true` in merged settings. On exit, the session is summarised into long-term memory for the next launch (TUI, headless, IDE bridge, and coordinator each flush once).

| Storage | Detail |
|--------|--------|
| Default | `.claude/memory.json` |
| SQLite | Set `DROVER_CODE_DREAM_BACKEND=sqlite` → `.claude/memory.db` |
| Import | First time SQLite opens an **empty** DB, existing `memory.json` is imported and renamed to **`memory.json.imported`**. Set `DROVER_CODE_DREAM_SKIP_JSON_IMPORT=1` to skip. |

**Retention** (optional caps; applied after each new memory write and **once at startup** if limits are set):

| Source | Keys |
|--------|------|
| Settings | `dreamMaxRetentionEntries` (max rows kept, newest first), `dreamMaxRetentionAgeDays` |
| Env | `DROVER_CODE_DREAM_MAX_ENTRIES`, `DROVER_CODE_DREAM_MAX_AGE_DAYS` (`0` = unlimited for that rule) |

Env overrides win over JSON settings for those two values.

## Modes

| Mode | How |
|------|-----|
| Coordinator | `CLAUDE_CODE_COORDINATOR_MODE=1` or `"coordinatorMode": true` in settings |
| IDE bridge (JSON-RPC over stdio) | `CLAUDE_CODE_IDE_BRIDGE=1` |
| GitHub webhook server | `./drover-code webhook` (needs `GITHUB_TOKEN`; optional `GITHUB_WEBHOOK_SECRET`, `WEBHOOK_ADDR`, `WEBHOOK_WORK_DIR`) |

## TUI slash commands

| Command | Action |
|---------|--------|
| `/clear`, `/reset` | Clears on-screen history **and** API conversation state |
| `/tokens` | Estimated context size (char÷4 heuristic) and cumulative API usage |
| `/model` | Shows active model and how to change it |
| `/compact` | One summarisation round (older turns collapsed into a summary message) |
| `/quit`, `/exit` | Exit |

Long sessions: the agent loop **automatically** summarises when the estimated context passes the soft limit (see `internal/convo`). Exact token counts are not implemented yet.

## Limitations

- Context size is estimated with a **character heuristic**, not a real tokenizer.
- IDE bridge handlers are minimal; full parity with every extension RPC is not guaranteed.
- Run `go test ./...` before releases; use `CGO_ENABLED=0` in CI to keep builds portable.
