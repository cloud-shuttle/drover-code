# Anthropic-compatible API providers

drover-code speaks the **Anthropic Messages API**: `POST …/v1/messages`, Server-Sent Events, tool use in the same shape as Anthropic’s API. Any vendor that exposes that wire format can be used by pointing the client at their base URL and supplying their API key.

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

Do **not** point `ANTHROPIC_BASE_URL` at a non-Anthropic API (for example some older “PaaS v4” OpenAI-style URLs): the path must resolve to **`/v1/messages`** on the Anthropic wire format.

## Webhook mode

`./drover-code webhook` uses the same variables. Set `ANTHROPIC_BASE_URL` and key env vars before starting the server.

## Live evals

The `evals` package accepts `ANTHROPIC_API_KEY` or `ANTHROPIC_AUTH_TOKEN` and honors `ANTHROPIC_BASE_URL` when running `go test ./evals/... -run TestAgentEvals`.

## Troubleshooting

1. **401 /403** — Wrong key, wrong project, or gateway expects a different header than `x-api-key`.
2. **404 on `/v1/messages`** — `ANTHROPIC_BASE_URL` is wrong: the Messages API path must be `{base}/v1/messages`.
3. **Stream or tool errors** — Provider may diverge slightly from Anthropic’s SSE or tool schema; compare with vendor docs or capture a minimal `curl` against the same base URL.

## Adding another vendor

If the vendor documents “Anthropic API compatibility” or “Claude Code”-style env vars (`ANTHROPIC_BASE_URL` + token), try:

1. Set `ANTHROPIC_BASE_URL` to their documented Anthropic root (not their OpenAI `…/v1` root unless they alias it).
2. Set `ANTHROPIC_MODEL` to their model string.
3. Put the key in `ANTHROPIC_API_KEY` or `ANTHROPIC_AUTH_TOKEN` per their examples.

When it works, consider sending a short PR to extend this doc with the vendor name and example env block.
