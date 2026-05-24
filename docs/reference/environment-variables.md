---
title: Environment Variables
description: Comprehensive reference of all environment variables used to configure Drover Code.
product: drover-code
audience: platform-operator
doc_type: reference
surface: repo-docs
---

# Environment Variables Reference

`drover-code` is highly configurable via environment variables. This reference documents all available variables, their purpose, and their acceptable values.

## Anthropic / API Configuration

| Variable | Required | Description |
|----------|----------|-------------|
| `ANTHROPIC_API_KEY` | One of key vars | Primary API key (Anthropic `sk-ant-…` or a provider key). |
| `ANTHROPIC_AUTH_TOKEN` | One of key vars | Alternative key name; **if `ANTHROPIC_API_KEY` is empty, this is used** (e.g. Moonshot/Kimi docs). |
| `ANTHROPIC_BASE_URL` | For gateways | Overrides the API host. Requests go to `{BASE_URL}/v1/messages` (no trailing slash required). |
| `ANTHROPIC_MODEL` | Optional | Model id for the provider (also configurable via `.drover/settings.json` `"model"`). |

At least one of `ANTHROPIC_API_KEY` or `ANTHROPIC_AUTH_TOKEN` must be non-empty to run the agent.

## Headless & Coordinator Modes

| Variable | Description |
|----------|-------------|
| `DROVER_CODE_HEADLESS` | Set to `1` to run in non-interactive batch worker mode (no TTY, no prompts). |
| `DROVER_CODE_HEADLESS_PLAIN` | Set to `1` to force plain text output instead of default headless formatting. |
| `DROVER_CODE_JSONL` | Set to `1` to force JSON Lines event output in headless mode. |
| `DROVER_CODE_RESULT_PATH` | Path to write the final structured completion artifact on exit. |
| `CLAUDE_CODE_COORDINATOR_MODE`| Set to `1` to run as a multi-worker task coordinator. |
| `CLAUDE_CODE_IDE_BRIDGE` | Set to `1` to run as a JSON-RPC over stdio backend. |

## Permission & Governance

| Variable | Description |
|----------|-------------|
| `DROVER_CODE_PERMISSION_PRESET`| Preset rules for tool execution (e.g., `unikernel` for isolated workers). |
| `DROVER_WARDEN_BEADS_DIR` | Directory containing `policies.jsonl` and `audit.jsonl` for the Warden semantic firewall. |

## Dream Memory Backend

| Variable | Description |
|----------|-------------|
| `DROVER_CODE_DREAM_BACKEND` | Storage backend for memory. Set to `sqlite` for database-backed storage (`.drover/memory.db`). Defaults to JSON. |
| `DROVER_CODE_DREAM_SKIP_JSON_IMPORT`| Set to `1` to skip importing existing `memory.json` on first SQLite init. |
| `DROVER_CODE_DREAM_MAX_ENTRIES`| Maximum number of memory rows to retain (newest first). |
| `DROVER_CODE_DREAM_MAX_AGE_DAYS`| Maximum age of memory entries in days. |

## Webhook Mode

| Variable | Description |
|----------|-------------|
| `GITHUB_TOKEN` | Required for `./drover-code webhook`. API token for interacting with GitHub. |
| `GITHUB_WEBHOOK_SECRET` | Secret for verifying HMAC signatures on inbound GitHub webhooks. |
| `WEBHOOK_ADDR` | Listen address for the webhook server (e.g., `:8080`). |
| `WEBHOOK_WORK_DIR` | Working directory for the agent sessions spawned by webhooks. |

## Evals

| Variable | Description |
|----------|-------------|
| `RUN_AGENT_EVALS` | Set to `1` to enable live Anthropic eval tests during `go test ./evals/...`. |
