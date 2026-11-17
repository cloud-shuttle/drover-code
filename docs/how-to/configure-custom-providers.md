---
title: Configure custom LLM providers
description: Point Drover Code at Anthropic-compatible or OpenAI-compatible API endpoints (including self-hosted SGLang).
product: drover-code
audience: platform-operator
doc_type: how-to
topics:
  - llm-routing
surface: repo-docs
---

# Custom LLM providers

Drover Code supports two wire formats:

| Protocol | Env trigger | Request path | Auth |
|----------|-------------|--------------|------|
| **Anthropic Messages** (default) | `ANTHROPIC_API_KEY` / `ANTHROPIC_AUTH_TOKEN` | `{BASE}/v1/messages` | `x-api-key` |
| **OpenAI Chat Completions** | `OPENAI_BASE_URL` or `DROVER_LLM_PROVIDER=openai` | `{BASE}/v1/chat/completions` | `Authorization: Bearer` |

The agent loop, tools, TUI, headless, webhook, and coordinator all share the same conversation types. The OpenAI path translates messages and tool calls to Chat Completions (including SGLang `--tool-call-parser deepseekv4`).

# Anthropic-compatible API providers

drover-code’s default protocol is the **Anthropic Messages API**. Any vendor that exposes that wire format can be used by pointing the client at their base URL and supplying their API key.

Official Anthropic needs only `ANTHROPIC_API_KEY` (default base URL is `https://api.anthropic.com`). **Alternate providers** need an explicit base URL and sometimes a different env var for the key (see below).

## Environment variables

| Variable | Required | Description |
|----------|----------|-------------|
| `ANTHROPIC_API_KEY` | One of key vars | Primary API key (Anthropic `sk-ant-…` or a provider key). |
| `ANTHROPIC_AUTH_TOKEN` | One of key vars | Alternative key name; **if `ANTHROPIC_API_KEY` is empty, this is used** (e.g. Moonshot/Kimi docs). |
| `ANTHROPIC_BASE_URL` | For gateways | Overrides the API host. Requests go to `{BASE_URL}/v1/messages` (no trailing slash required). |
| `ANTHROPIC_MODEL` | Optional | Model id for the provider (also configurable via `.claude/settings.json` `"model"`). |

At least one of `ANTHROPIC_API_KEY` or `ANTHROPIC_AUTH_TOKEN` must be non-empty.

The HTTP client sends **`x-api-key`** and **`anthropic-version`** like the official API. If a gateway only accepts **`Authorization: Bearer`**, requests may fail until the client gains an auth mode for that gateway (open an issue with the provider’s docs).

## Moonshot (Kimi K2.5)

Moonshot documents an Anthropic-compatible entrypoint for tools such as Claude Code. Authoritative setup and model list: [Use Kimi K2.5 Model in ClaudeCode/Cline/RooCode](https://platform.kimi.ai/docs/guide/agent-support).

Example:

```bash
export ANTHROPIC_BASE_URL=https://api.moonshot.ai/anthropic
export ANTHROPIC_AUTH_TOKEN=your-moonshot-api-key
export ANTHROPIC_MODEL=kimi-k2.5
./drover-code
```

Create API keys in the [Kimi Open Platform](https://platform.kimi.ai/console/api-keys).

## Zhipu GLM

Many GLM deployments expose an Anthropic-compatible path. A commonly cited prefix is:

`https://open.bigmodel.cn/api/anthropic`

**Confirm the exact base URL and model id in your GLM / BigModel console**; products and paths change.

Example:

```bash
export ANTHROPIC_BASE_URL=https://open.bigmodel.cn/api/anthropic
export ANTHROPIC_API_KEY=your-glm-api-key
export ANTHROPIC_MODEL=glm-4.5
./drover-code
```

Do **not** point `ANTHROPIC_BASE_URL` at a non-Anthropic API (for example some older “PaaS v4” OpenAI-style URLs): the path must resolve to **`/v1/messages`** on the Anthropic wire format. For OpenAI-style hosts, use the OpenAI variables below.

## OpenAI-compatible APIs (SGLang, DeepSeek-V4-Flash)

Use this when the server implements `POST /v1/chat/completions` with Bearer auth — including the self-hosted DeepSeek-V4-Flash SGLang stack (`https://llm.drover.sh/v1`).

| Variable | Required | Description |
|----------|----------|-------------|
| `OPENAI_BASE_URL` | Yes (or set `DROVER_LLM_PROVIDER`) | API root. `https://llm.drover.sh/v1` and `https://llm.drover.sh` both resolve to `/v1/chat/completions`. |
| `OPENAI_API_KEY` | One of key vars | Bearer token. |
| `SGLANG_API_KEY` | One of key vars | Used when `OPENAI_API_KEY` is empty (matches the Akamai `config/dsv4.env` kit). |
| `OPENAI_MODEL` | Recommended | Served model id. Falls back to `SERVED_MODEL_NAME` if set. |
| `DROVER_LLM_PROVIDER` | Optional | `openai`, `openai-compat`, or `sglang` forces this protocol. `anthropic` forces Messages even if `OPENAI_BASE_URL` is set. |

Selecting the OpenAI protocol: set `DROVER_LLM_PROVIDER=openai`, **or** set `OPENAI_BASE_URL`. Then at least one of `OPENAI_API_KEY` / `SGLANG_API_KEY` must be non-empty.

### DeepSeek-V4-Flash on SGLang

After `source config/dsv4.env` from the deploy kit:

```bash
source /path/to/akamai/config/dsv4.env
export OPENAI_BASE_URL=https://llm.drover.sh/v1
export OPENAI_API_KEY="$SGLANG_API_KEY"
export OPENAI_MODEL="${SERVED_MODEL_NAME:-deepseek-v4-flash}"
./drover-code
```

Equivalent (key alias only):

```bash
export DROVER_LLM_PROVIDER=openai
export OPENAI_BASE_URL=https://llm.drover.sh/v1
export SGLANG_API_KEY=your-sglang-api-key
export OPENAI_MODEL=deepseek-v4-flash
./drover-code
```

The SGLang service should be started with an OpenAI-compatible tool parser (the deploy kit uses `--tool-call-parser deepseekv4` and `--reasoning-parser deepseek-v4`). Drover Code sends tools as OpenAI `type: function` entries and maps streamed `tool_calls` back into the agent loop.

`max_tokens` is sent as in the OpenAI spec (same field the smoke `curl` uses). Reasoning tokens from the parser are not stored as assistant text.

## Webhook mode

`./drover-code webhook` uses the same provider variables as the TUI and headless modes.

## Live evals

The `evals` package accepts `ANTHROPIC_API_KEY` or `ANTHROPIC_AUTH_TOKEN` and honors `ANTHROPIC_BASE_URL` when running `go test ./evals/... -run TestAgentEvals`.

## Troubleshooting

1. **401 /403** — Wrong key, or the host expects a different header (`x-api-key` vs `Authorization: Bearer`). OpenAI-compatible hosts need Bearer.
2. **404 on `/v1/messages`** — You pointed `ANTHROPIC_BASE_URL` at an OpenAI-style server. Set `OPENAI_BASE_URL` instead.
3. **404 on `/v1/chat/completions`** — `OPENAI_BASE_URL` is wrong. For `https://llm.drover.sh/v1/chat/completions`, use `OPENAI_BASE_URL=https://llm.drover.sh/v1` (or the host without `/v1`).
4. **Stream or tool errors** — Provider may diverge from Anthropic SSE or OpenAI `tool_calls`; compare with a minimal `curl` against the same base URL.

## Adding another vendor

If the vendor documents “Anthropic API compatibility” or “Claude Code”-style env vars (`ANTHROPIC_BASE_URL` + token), try:

1. Set `ANTHROPIC_BASE_URL` to their documented Anthropic root (not their OpenAI `…/v1` root unless they alias it).
2. Set `ANTHROPIC_MODEL` to their model string.
3. Put the key in `ANTHROPIC_API_KEY` or `ANTHROPIC_AUTH_TOKEN` per their examples.

When it works, consider sending a short PR to extend this doc with the vendor name and example env block.
