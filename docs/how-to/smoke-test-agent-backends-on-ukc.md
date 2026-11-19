---
title: Smoke-test agent backends on Unikraft Cloud (syd0)
description: Plan and harness to verify subprocess spawning, git and outbound TLS for Claude Code, OpenCode Go, Grok Build, Codex CLI and Gemini CLI inside Unikraft Cloud microVMs in Sydney.
product: drover-code
audience: platform-operator
doc_type: how-to
topics:
  - agent-jobs
  - deployment
  - llm-routing
surface: repo-docs
---

# Smoke-test agent backends on Unikraft Cloud (syd0)

**Status:** plan only. No instances have been launched. Results go in the
[pass/fail matrix](#pass-fail-matrix) when the plan is run.

**Goal:** before Drover Cloud routes work to a third-party coding agent, prove
that each agent binary can boot, spawn subprocesses, use git, and make verified
outbound TLS connections inside a Unikraft Cloud microVM on the Sydney metro
(`syd0-plausible-gnu`), then complete one headless edit-and-commit task.

**Harness:** [`scripts/smoke/agent-backends/`](../../scripts/smoke/agent-backends/)
contains one `Kraftfile` + `Dockerfile` per backend and a shared in-VM script,
`smoke.sh`, that runs every step below and prints one JSON line per step.

| Backend | What is tested | Binary type | Auth used in this test |
|---|---|---|---|
| Claude Code | `claude` CLI | Native binary (glibc build used here; musl build exists) | `ANTHROPIC_API_KEY`, or `CLAUDE_CODE_OAUTH_TOKEN` for a Peter-only run |
| OpenCode Go | `opencode` CLI client calling `https://opencode.ai/inference/go` | npm package `opencode-ai` on Node 22 | `OPENCODE_GO_API_KEY` |
| Grok Build | `grok` CLI | Static-PIE ELF, rustls (checked on v1.0.89) | `XAI_API_KEY` |
| Codex CLI | `codex` CLI | Static-PIE musl ELF (checked on v0.170.0) | `OPENAI_API_KEY` |
| Gemini CLI | `gemini` CLI | npm package `@google/gemini-cli` on Node 22 | `GEMINI_API_KEY` |

OpenCode Go is an endpoint, not a binary. This plan tests the `opencode` client
against it directly. The run through drover-gateway comes later, once the
gateway has an OpenCode Go provider.

## Auth policy

- **API keys are the default** for every backend in this plan. They are the only
  credentials that are safe for tenant-facing use.
- **Subscription tokens are Peter-only.** A `CLAUDE_CODE_OAUTH_TOKEN` from
  `claude setup-token`, a ChatGPT `auth.json` for Codex, or a `grok login
  --device-auth` session may only be used for Peter's own runs. They are never
  shared, never baked into an image, and never used for tenants. See the
  [Claude Code legal and compliance page](https://code.claude.com/docs/en/legal-and-compliance)
  (undated, read 19 Nov 2026) and the
  [Anthropic Usage Policy](https://www.anthropic.com/legal/aup) (effective 12 Nov 2026).
- Gemini subscription (Code Assist) login is out of scope. Gemini CLI's own
  [terms page](https://geminicli.com/docs/resources/tos-privacy/) (last updated
  10 Apr 2026) prohibits using its OAuth from third-party tools, and its
  [auth docs](https://geminicli.com/docs/get-started/authentication/) recommend
  an API key or Vertex for headless use.
- Secrets are passed at run time as environment variables, **named but never
  with values** in this doc, the image, the Kraftfile or shell history. Load them
  from your secret store into the operator shell first.

## Images

All five images use `runtime: base-compat:latest`, the same runtime as
drover-code's `ukc-agent` (`Kraftfile`, `cmd/ukc-agent/Kraftfile`). Unikraft's
OpenClaw example (a Node agent with bash and sshd) runs on Unikraft Cloud with
the default runtime ([guide](https://unikraft.com/docs/guides/openclaw)), so a
second pass on the default runtime is optional (see [Order](#order-of-execution)).

| Backend | Rootfs base | Install | Memory | Secret env (names only) |
|---|---|---|---|---|
| claude | `debian:bookworm-slim` (glibc) | `https://claude.ai/install.sh`, binary copied to `/usr/local/bin/claude`; `DISABLE_AUTOUPDATER=1` | 4096 MiB (Claude Code lists 4 GB+ RAM) | `ANTHROPIC_API_KEY` or `CLAUDE_CODE_OAUTH_TOKEN` |
| opencode | `node:22-bookworm-slim` | `npm i -g opencode-ai`; config in `opencode/opencode.json` | 2048 MiB | `OPENCODE_GO_API_KEY` |
| grok | `debian:bookworm-slim` + `ca-certificates` | `https://x.ai/cli/install.sh` pinned to a version | 2048 MiB | `XAI_API_KEY` |
| codex | `debian:bookworm-slim` | GitHub release `codex-x86_64-unknown-linux-musl` | 2048 MiB | `OPENAI_API_KEY` |
| gemini | `node:22-bookworm-slim` | `npm i -g @google/gemini-cli` | 2048 MiB | `GEMINI_API_KEY` |

Every image also installs `bash coreutils curl ca-certificates openssl git jq procps`.

**Writable storage:** the rootfs is EROFS, which is read-only, so each instance
mounts a volume at `/work`. `smoke.sh` sets `HOME=/work/home` and
`TMPDIR=/work/tmp`, and Codex's `CODEX_HOME` points at `/work/home/.codex`.

Kraftfile (identical shape per backend):

```yaml
spec: v0.6
runtime: base-compat:latest
rootfs: ./Dockerfile
cmd: ["/usr/bin/bash", "/usr/local/bin/smoke.sh"]
```

Dockerfile shape (Claude shown; others in the harness directory):

```dockerfile
FROM --platform=linux/amd64 debian:bookworm-slim AS fetch
ARG CLAUDE_VERSION=stable
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates bash
RUN curl -fsSL https://claude.ai/install.sh | bash -s "${CLAUDE_VERSION}" && cp -L /root/.local/bin/claude /claude

FROM --platform=linux/amd64 debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends bash coreutils curl ca-certificates openssl git jq procps
COPY --from=fetch /claude /usr/local/bin/claude
COPY smoke.sh /usr/local/bin/smoke.sh
ENV SMOKE_BACKEND=claude DISABLE_AUTOUPDATER=1
```

Unverified: the install-script paths for Claude Code and Grok Build, the
`{env:...}` substitution in `opencode.json`, and the exact OpenCode Go model ID
(`opencode-go/kimi-k3` is the docs example). Fix these at build time if they
differ.

## Launch (operator machine)

Do not run this until the plan is approved. Example for one backend:

```bash
cd scripts/smoke/agent-backends/grok
cp ../smoke.sh .                      # staged copy, git-ignored
unikraft build . --output <org>/smoke-grok:2026-11-20
unikraft run --metro syd0-plausible-gnu \
  --name smoke-grok-$(date +%Y%m%d%H%M) \
  -m 2048M \
  --volume :/work:size=2GiB \
  -e SMOKE_BACKEND=grok \
  -e XAI_API_KEY="${XAI_API_KEY}" \
  --image <org>/smoke-grok:2026-11-20
unikraft instances logs smoke-grok-<ts>   # JSON lines from smoke.sh
```

- No service or public port is needed: the script runs as the instance command
  and results are read from the instance logs.
- `deploy/sydney/deploy_ukc.py` in drover-cloud packages with `kraft pkg` to
  `index.syd0-plausible-gnu.unikraft.cloud`. That path works too. Use whichever
  CLI Peter's Sydney profile is set up for.
- Unikraft Cloud shows environment values as `*` in instance output (see the
  OpenClaw guide above). Still, never paste a value on the command line in a
  shared terminal recording.

Optional rerun channel: attach the sandbox plugin
(`--plugin 'name=sandbox,image=plugins/sandbox:latest'`) and rerun single steps
with `unikraft instance exec <name> -- bash /usr/local/bin/smoke.sh`. This needs
`base-compat` and has no PTY, which is fine because every step is non-interactive
([plugins docs](https://unikraft.com/docs/features/plugins)).

## Steps, commands and success criteria

`smoke.sh` runs these in order. Every step has a timeout (default 300 s). A step
passes when its exit code (`rc`) is 0, unless noted.

### 0. Environment facts (logged, not pass/fail)

| Fact | Command | Expected |
|---|---|---|
| Boot uptime | `cut -d' ' -f1 /proc/uptime` | A small number of seconds |
| Kernel | `uname -a` | Whatever base-compat reports; record it |
| VM clock | `date -u +%s` | Within 5 s of the operator's NTP-synced clock at launch |
| HTTP Date | `curl -sI http://example.com/` `Date:` header | Present. Plain HTTP so a bad clock can't hide behind a TLS failure |
| Clock delta | VM epoch minus HTTP Date epoch | `clock_delta_s` within ±5 |
| DNS | `cat /etc/resolv.conf`; `getent hosts <host>` for each backend host and `github.com` | Each host resolves |

### 1. Binary

| Step | Command | Success |
|---|---|---|
| `version` | `claude --version` / `opencode --version` / `grok --version` / `codex --version` / `gemini --version` | rc 0 and a version string. rc 127 means the binary is missing; any crash or `ENOSYS` points at ELF-loader incompatibility |

### 2. Spawn

| Step | Command | Success |
|---|---|---|
| `spawn_sh` | `sh -c 'echo ok'` | stdout `ok` |
| `spawn_nested` | `sh -c 'sh -c "bash -c \"echo nested-ok\""'` | stdout `nested-ok` |
| `spawn_pipeline` | `bash -c 'printf "a\nb\n" \| sort -r \| head -1'` | stdout `b` |
| `spawn_bg` | `bash -c 'sleep 1 & wait $! && echo bg-ok'` | stdout `bg-ok` |
| `agent_spawn` | Headless prompt (below) asking the agent's own shell tool to run `uname -a && git --version` | rc 0, and the output contains `git version` and the same kernel string as step 0 |

### 3. Git

| Step | Command | Success |
|---|---|---|
| `git_local` | `git init`, write `a.txt`, `git add`, `git commit -m init`, `git log --oneline \| wc -l` | stdout `1` |
| `git_clone_https` | `git clone --depth 1 https://github.com/octocat/Hello-World.git` | rc 0 and the directory has a `.git` |

### 4. TLS

For every backend host plus `github.com`:

| Step | Command | Success |
|---|---|---|
| `tls_openssl_<host>` | `openssl s_client -connect <host>:443 -servername <host> -verify_return_error` | Output contains `Verify return code: 0 (ok)`; issuer is logged |
| `tls_curl_<host>` | `curl -sS -o /dev/null -w '%{http_code} verify=%{ssl_verify_result}' https://<host>/` | rc 0 and `verify=0`. Any HTTP status is fine |
| `tls_ca_system` | Same curl to GitHub with `SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt` | `verify=0` |
| `tls_ca_empty_expect_fail` | curl to GitHub with an empty `--cacert` | Must **fail**. Proves verification is really on |

Also logged: `NODE_EXTRA_CA_CERTS` and `SSL_CERT_FILE` as seen by the process
(the Node images set `NODE_EXTRA_CA_CERTS` to the system bundle).

| Backend | Hosts checked |
|---|---|
| claude | `api.anthropic.com`, `claude.ai`, `platform.claude.com` ([network config](https://code.claude.com/docs/en/network-config)) |
| opencode | `opencode.ai` |
| grok | `api.x.ai`, `auth.x.ai`, `cli-chat-proxy.grok.com` ([enterprise docs](https://docs.x.ai/build/enterprise)) |
| codex | `api.openai.com`, `chatgpt.com` (the second is for ChatGPT sign-in only) |
| gemini | `generativelanguage.googleapis.com` |

### 5. Headless end-to-end

`smoke.sh` creates `/work/e2e` as a git repo with one commit containing
`SMOKE.md`, then gives the agent this prompt:

> In the current git repository, append the line 'smoke-ok <backend>' to
> SMOKE.md, then run: git add SMOKE.md && git commit -m 'smoke: <backend>' .
> Do not push. Reply DONE when the commit exists.

| Backend | Command |
|---|---|
| claude | `claude -p "<prompt>" --output-format json --allowedTools Bash Edit Write Read` |
| opencode | `opencode run --model opencode-go/kimi-k3 "<prompt>"` (permissions allowed in `opencode.json`) |
| grok | `grok -p "<prompt>" --output-format json --always-approve` |
| codex | `codex exec --json --skip-git-repo-check --dangerously-bypass-approvals-and-sandbox "<prompt>"`, after `printenv OPENAI_API_KEY \| codex login --with-api-key` |
| gemini | `gemini -p "<prompt>" --output-format json --yolo` |

Codex's own sandbox needs bubblewrap and user namespaces, which a unikernel
won't have. The microVM is the sandbox, which is what the bypass flag is for
([sandboxing](https://developers.openai.com/codex/concepts/sandboxing)).
Unverified: whether Gemini CLI still accepts `--output-format json` and
`--yolo` in the version you install.

**Success (`e2e_verify`):**
- the agent step returns rc 0;
- `git log -1 --format=%s` is exactly `smoke: <backend>`;
- `SMOKE.md` contains the line `smoke-ok <backend>`;
- `git rev-list --count HEAD` is `2`.

## What to log

Inside the VM, `smoke.sh` prints one JSON object per step:

```json
{"backend":"grok","step":"tls_openssl_api.x.ai","rc":0,"ms":44,"stdout":"…","stderr":"…"}
```

`stdout` and `stderr` keep the last 2 KiB of each. Facts are logged as
`{"backend":…,"note":…,"value":…}` lines. The run ends with
`SMOKE_DONE backend=<b> fails=<n>`.

On the operator side, record for each run:

| Field | Source |
|---|---|
| Instance name and UUID | `unikraft run` output |
| Image digest | build or push output |
| Runtime | `base-compat:latest` digest, from the Kraftfile resolution |
| Boot time | Instance metadata if exposed; otherwise `boot_uptime` plus launch timestamp |
| Clock delta | `clock_delta_s` note, and operator NTP time at launch |
| DNS | `dns_<host>` notes |
| Per-step rc, stderr, duration | JSON lines |
| Token usage | From agent JSON output: Claude `usage`/`total_cost_usd`; Codex `turn.completed` usage events; Grok and Gemini JSON stats if present |

**Redaction rule (mandatory):**
- `smoke.sh` replaces the literal value of every secret env var
  (`CLAUDE_CODE_OAUTH_TOKEN`, `ANTHROPIC_API_KEY`, `OPENCODE_GO_API_KEY`,
  `XAI_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`) with `[REDACTED:<name>]`
  before printing anything.
- It also masks token-shaped strings: `sk-ant-…`, `sk-…`, `xai-…`, `AIza…`,
  `Bearer …`, and JSON `access_token`/`refresh_token`/`id_token`/`api_key` values.
- Never add `set -x`, never print `env`, and never upload `/work/home` (it can
  hold Codex `auth.json`).
- Before sharing a log, grep it for the first 8 characters of each key you used.

## Pass/fail matrix

Fill in `PASS`, `FAIL` or `n/a`, with the run date (AEDT) in Notes.

| Backend | Boot | Spawn | Nested spawn | Git local | Git clone HTTPS | TLS verify | Clock OK | Headless E2E | Notes |
|---|---|---|---|---|---|---|---|---|---|
| Claude Code |  |  |  |  |  |  |  |  |  |
| OpenCode Go (opencode CLI) |  |  |  |  |  |  |  |  |  |
| Grok Build |  |  |  |  |  |  |  |  |  |
| Codex CLI |  |  |  |  |  |  |  |  |  |
| Gemini CLI |  |  |  |  |  |  |  |  |  |

Column mapping:
- **Boot:** the instance reaches `running` and the first JSON line appears.
- **Spawn:** `spawn_sh`, `spawn_pipeline` and `spawn_bg` all pass.
- **Nested spawn:** `spawn_nested` and `agent_spawn` both pass.
- **Git local:** `git_local` passes.
- **Git clone HTTPS:** `git_clone_https` passes.
- **TLS verify:** every `tls_*` step passes, and `tls_ca_empty_expect_fail` failed as expected.
- **Clock OK:** `clock_delta_s` within ±5.
- **Headless E2E:** `agent_e2e` and `e2e_verify` both pass.

## Known risks to watch for

| Risk | What it looks like | Response |
|---|---|---|
| No `fork()` on Unikraft. It supports `vfork`, `execve` and `posix_spawn` only ([multiprocess post](https://unikraft.org/blog/2025-05-15-multiprocess)) | `spawn_*` or `agent_spawn` fails with `ENOSYS` or hangs | Record it. Retry once on the default runtime; if it still fails, that backend needs a full Linux microVM |
| `execve` needs PIE binaries ([posix-process README](https://github.com/unikraft/unikraft/blob/staging/lib/posix-process/README.md)) | `version` crashes or reports an exec format error | Codex and Grok are static-PIE (checked). Claude Code and Node are unverified |
| Firecracker 1999 clock bug ([unikraft#1759](https://github.com/unikraft/unikraft/issues/1759), open) | `clock_delta_s` is huge and every TLS step fails | Expected on self-hosted drover-runner only, not hosted Unikraft Cloud. If it shows up on syd0, stop and report it |
| Network shield CA ([network shield](https://unikraft.com/docs/features/network-shield)) | TLS issuer is the shield's CA and verification fails | Only if a shield or relay is attached. Add its CA to the rootfs trust store and `NODE_EXTRA_CA_CERTS` |
| Codex ChatGPT `auth.json` refreshes in place ([CI auth](https://developers.openai.com/codex/auth/ci-cd-auth)) | A Peter-only subscription run fails on a later boot | Keep `CODEX_HOME` on the `/work` volume, one instance per `auth.json`. The API-key run is unaffected |
| Sandbox plugin has no PTY ([plugins](https://unikraft.com/docs/features/plugins)) | Interactive `/login` or prompts hang | Use API keys or `CLAUDE_CODE_OAUTH_TOKEN`; every step here is non-interactive |
| Grok rustls uses the OS trust store ([enterprise docs](https://docs.x.ai/build/enterprise)) | `grok` TLS errors while `curl` passes | Confirm `ca-certificates` is installed in the grok image (it is in the sketch) |
| Read-only EROFS rootfs | `git init` or agent config writes fail | `/work` volume; `HOME` and `TMPDIR` point there |

## Order of execution

1. **grok**, then **codex**: static-PIE binaries with no runtime
   dependencies. These isolate platform problems (spawn, TLS, clock) from
   packaging problems.
2. **claude**: native binary, the planning-tier backend.
3. **opencode**, then **gemini**: Node-based, so they also test Node on base-compat.
4. If anything fails on spawn or exec, rerun that backend once on the default
   runtime (remove the `runtime:` line, as the OpenClaw example does) before
   calling it.
5. Later, once drover-gateway has an OpenCode Go provider, rerun opencode with its
   base URL pointed at the gateway and a virtual key.

Run one backend at a time, so a failure in one doesn't leave several instances
and volumes behind.

## Teardown

After reading logs for each backend:

```bash
unikraft instances delete smoke-<backend>-<ts>
unikraft volumes delete <volume-name-or-uuid>   # removes /work, including any auth.json
```

Then confirm nothing is left:

```bash
unikraft instances list --metro syd0-plausible-gnu | grep smoke- || echo clean
unikraft volumes list --metro syd0-plausible-gnu
```

## Cost and time notes

- Each run is short: boot, about 30 short steps, two agent prompts, then the
  script lingers 30 s (`SMOKE_LINGER_S`) and exits. Expect a few minutes of
  instance time per backend. This is an estimate; nothing has been run.
- Model spend is two small prompts per backend on API keys. Check the token
  usage lines afterwards.
- Unikraft Cloud pricing for syd0 isn't stated here. Check Peter's plan before
  running all five in parallel.
- Image builds happen on the operator machine and take longer than the runs
  (Node images especially).

## References

- Spike sources (read 19–20 Nov 2026): [Claude Code authentication](https://code.claude.com/docs/en/authentication),
  [Claude Code setup](https://code.claude.com/docs/en/setup),
  [Codex authentication](https://developers.openai.com/codex/auth),
  [Codex non-interactive mode](https://developers.openai.com/codex/noninteractive),
  [Grok Build overview](https://docs.x.ai/build/overview),
  [OpenCode Go](https://opencode.ai/docs/go),
  [Gemini CLI authentication](https://geminicli.com/docs/get-started/authentication/).
- Related in this repo: [Unikraft Cloud explanation](../explanation/unikraft-cloud.md),
  `cmd/ukc-agent`, `pkg/hostedworker`, `internal/tools/ukc/api.go`.
