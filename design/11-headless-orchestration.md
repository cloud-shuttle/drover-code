# 11 — Headless mode and orchestrated execution (Drover × Unikraft unikernels)

**Entrypoint:** `cmd/drover-code`  
**Primary packages:** `internal/agent`, `internal/permissions`, `internal/tools`  
**Related docs:** [07 — TUI](07-tui.md), [10 — Integrations](10-integrations.md), [08 — Config, permissions, undercover](08-config-permissions-undercover.md)

---

## Purpose

This document specifies **non-interactive ("headless") execution** of drover-code
when a **controller** (e.g. Drover) runs **many isolated jobs** — for example one
**Unikraft unikernel instance per task** — instead of a human at a terminal.

Goals:

1. **Stable automation contract** — predictable activation, inputs, outputs, and
   exit codes so orchestrators can schedule, retry, and aggregate results.
2. **First-class batch path** — headless is not "TUI with stdin piped"; it is an
   explicit execution mode for CI, unikernel instances, and supervisors.
3. **Clear boundaries** — what the **agent** must accomplish inside the unikernel
   vs what **Drover** does after success (git push, open PR, Slack) — so
   completion is observable and not inferred from log prose.

---

## Motivation: Drover on Unikraft unikernel instances

### What Unikraft is

Unikraft is a framework for building **unikernels** — specialised, single-address-space
OS images that contain only the code strictly necessary for one application to run.
Key properties relevant to Drover:

- **Boot time:** sub-1ms for the unikernel itself; 3ms total with Firecracker as VMM.
- **Memory footprint:** 2–6 MB base; each instance gets a hard ceiling enforced by
  the hypervisor — OOM in one instance cannot cascade to others.
- **Security:** cross-instance isolation is provided by the hypervisor, not software
  primitives. No shell, no unneeded syscalls, no unneeded OS features in the image.
- **Compatibility:** binary-compatible with Linux ELFs (x86_64, PIE) via Unikraft's
  syscall shim layer. Existing `drover-code` binaries run without modification.
- **VMM support:** Firecracker (recommended, 3ms boot), QEMU microVM (~10ms),
  Solo5 (~3ms). KVM is currently the only supported hypervisor.

Figures above reflect Unikraft's documented positioning; validate against current
releases and your VMM stack before hard SLAs.

### Target flow

1. **Drover** schedules a `drover-code` job per unikernel instance (one task = one
   instance).
2. Each instance receives a **task description** (ticket, spec, branch policy)
   injected at boot.
3. The agent **edits, runs tests, and validates** work in the workspace.
4. On success, the agent commits and the **completion artifact** is written. Drover
   then runs deterministic post-hooks: `git push`, `gh pr create`, Slack notify.
5. The unikernel instance exits; the hypervisor reclaims all memory.

That environment has **no TTY**, **no human** for permission prompts, and
**no long-lived stdin**. The binary must behave like a **batch worker** with
machine-readable telemetry.

---

## Current state (baseline)

Today, `cmd/drover-code` chooses between TUI and a simple headless path:

- If **stdin is not a character device**, the process runs **`runHeadless`**:
  line-oriented reads from stdin, agent loop per non-empty line, permission engine
  in bypass-style mode, events printed for humans, non-zero exit if any loop
  iteration failed (and context was not cancelled).
- If stdin **is** a TTY, the **BubbleTea TUI** runs ([07 — TUI](07-tui.md)).

**Gap:** mode selection by `isatty(stdin)` alone is **fragile** under systemd,
supervisors, SSH, and some VM consoles. Headless also assumes **interactive
permission UX is absent** — already reflected in bypass-style permissions, but not
documented as a supported product contract.

Separately, **`drover-code webhook`** ([10 — Integrations](10-integrations.md))
is a **long-lived HTTP server** shape; unikernel **one-shot jobs** are closer to
**batch headless** than to webhook, though both reuse the same agent and tools.

---

## Design principles

1. **Explicit over implicit** — prefer `DROVER_CODE_HEADLESS=1` and/or
   `--headless` over relying only on TTY detection.
2. **One-shot by default for unikernels** — a single task string or file should
   be able to run **one** agent turn (or a bounded multi-turn policy) without
   requiring a long-lived stdin session.
3. **Structured output for machines, readable logs for humans** — orchestrators
   consume **JSON Lines** (jsonl), one object per significant event; operators
   may still get text on stderr.
4. **Fail closed on ambiguity** — if permissions cannot be resolved without a
   human, **exit with a defined code** and a structured error — do not hang.
5. **Ephemeral trust model** — unikernel instances are **short-lived** and
   **scoped**: combine narrow filesystem layout, repo-only git credentials, and
   tool allowlists rather than interactive approval dialogs.
6. **Hard resource ceilings** — enforce token budget, wall-clock timeout, and
   tool call budget; map breaches to defined exit codes. Timeouts must be
   enforced from *outside* the agent loop, not relying on model self-termination.

---

## Execution modes (matrix)

| Mode            | Trigger (today / intended)     | Typical use                      |
|-----------------|--------------------------------|----------------------------------|
| TUI             | TTY + default entrypoint       | Local developer                  |
| Headless / batch| Explicit flag/env + non-TTY    | Unikernel instances, CI, workers |
| Coordinator     | `DROVER_CODE_COORDINATOR_MODE` | Multi-agent stdin-driven         |
| IDE bridge      | `DROVER_CODE_IDE_BRIDGE`       | Editor extension                 |
| Webhook server  | `drover-code webhook`          | GitHub `@mention` bot            |

**Headless** remains **orthogonal** to coordinator and webhook: same
`internal/agent` loop, different **adapter** (stdin/TUI vs JSON-RPC vs HTTP vs
batch flags).

---

## Headless contract (target)

### Activation

- **Required:** at least one of:
  - `DROVER_CODE_HEADLESS=1`, or
  - `--headless` on the CLI once a small flag parser exists.
- **Optional:** retain `!isatty(stdin)` as a **compatibility** signal when the
  explicit flag is unset (document as legacy / convenience only).

### Input

Support a clear precedence order:

1. **`--prompt` / `-p`** — single argument (shell-escaped by orchestrator).
2. **`--prompt-file` / `@file`** — path inside the unikernel; full task spec,
   markdown or plain text.
3. **Stdin** — line-oriented or single "here document" when no file/flag given;
   empty lines and `/quit` behavior should match documented rules.

For **one-shot unikernel jobs**, `--prompt-file` is preferred: Drover writes the
spec to a known path before exec, avoiding heredoc and quoting issues.

### Agent turns

Define **max turns** policy for batch mode:

- **Single-turn default:** one user message → model runs until stop (tool loop
  internal to `agent.Loop` as today).
- **Optional multi-turn:** env `DROVER_CODE_MAX_TURNS` or flag for rare cases
  where the controller injects follow-ups (advanced; phase 2).

Document that **"one job"** for Drover maps to **one headless invocation** with
**one primary prompt**, not an interactive REPL.

### Output

- **Structured stream (stdout):** JSON Lines (`jsonl`), one object per significant
  event, e.g.:

  ```json
  {"type":"tool_start","name":"bash","ts":"..."}
  {"type":"heartbeat","ts":"...","turn":3}
  {"type":"tool_end","name":"bash","exit_code":0,"ts":"..."}
  {"type":"complete","ok":true,"ts":"..."}
  ```

  Controllers tail or collect this stream. A **heartbeat event** must be emitted
  periodically (every N seconds, configurable) so the orchestrator can distinguish
  "still running" from "silently hung" without polling the hypervisor health
  endpoint. This is especially important for 20–30 minute agent runs.

- **Human diagnostics (stderr):** optional pretty lines for crash debugging; must
  not be the only machine-parseable channel.

- **Completion artifact (optional file):** `$DROVER_CODE_RESULT_PATH` — atomic
  write of final JSON on exit:

  ```json
  {
    "schema_version": 1,
    "ok": true,
    "branch": "feat/task-abc123",
    "commit_sha": "abc123...",
    "tests_passed": true,
    "pr_url": null,
    "error": null,
    "turns_used": 4,
    "tokens_used": 12400
  }
  ```

  `pr_url` is null when using the controller-owned PR pattern (see below); Drover
  fills it after running post-hooks. Enables Drover to avoid parsing free-form
  logs for results. Must be flushed even on non-zero exit (partial result).

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success — task completed per policy |
| 1 | Agent task failure — model could not complete task; likely not worth retrying without modification |
| 2 | Usage / config error (bad flags, missing key, invalid prompt file) |
| 3 | Permission / policy violation — action required human approval not available in headless mode |
| 4 | Timeout or max-turns exceeded |
| 5 | Infrastructure / transient failure — API rate limit, network error, Anthropic API unavailable; retry with backoff is appropriate |

The distinction between codes 1 and 5 is critical for orchestrators: code 1
means the task itself is problematic (retry with the same input is unlikely to
help); code 5 means the infrastructure failed (retry after backoff is the right
response).

Document **signal handling** (`SIGINT`/`SIGTERM`): flush telemetry, write partial
completion artifact if possible, exit non-zero.

---

## Permissions in headless

Interactive TUI uses permission prompts; headless **cannot**.

Options (not mutually exclusive):

1. **Bypass with scoped tools** — current direction: allow execution inside a
   unikernel that only mounts the repo and uses a **restricted token**. Pair with
   `allowed_tools` / `denied_tools` from config ([08](08-config-permissions-undercover.md)).
2. **Policy-only mode** — no prompts; deny if action is not pre-approved; exit
   code `3`.
3. **Pre-seeded permissions file** — generate `.claude/permissions.json` (or
   equivalent) in the unikernel image from Drover before start.

**Default for unikernel jobs:** bypass + deny list + read-only except workspace.
Deviations must be explicit in run configuration.

> **Phase ordering note:** permission presets (`DROVER_CODE_PERMISSION_PRESET=unikernel`)
> should be implemented in **Phase 3**, before the completion artifact (moved to
> Phase 4). The permission model is a blocker for real workloads; structured I/O
> is useful but not a blocker.

---

## Git push, PR, and Slack: who owns what?

Three supported patterns:

### A. Agent-owned (prompt + tools)

The task spec instructs the model to run tests, commit, push, and open a PR using
existing git/GitHub tools. **Risk:** model may omit a step. **Mitigation:** require
tool traces in structured logs and assert expected tools ran in Drover.

### B. Controller-owned post-hooks

The agent stops at **"workspace ready + tests green + committed"** (exit 0). **Drover**
runs deterministic scripts: `git push`, `gh pr create`, Slack webhook.

**Benefits:**

- Reproducible side effects regardless of agent behaviour.
- Credentials for push/PR/Slack never live in the agent context — a significant
  security improvement when running inside a unikernel with a restricted egress
  policy.
- `pr_url` in the completion artifact is filled by Drover after post-hooks, giving
  a clean observable record.

**Cost:** less per-task flexibility.

### C. Hybrid (default for Unikraft jobs)

Agent performs **git commit** inside the unikernel; Drover performs **push/PR/notify**
with credentials that never live in the agent context. This is the usual way to
realise pattern B in practice: **commit in-VM, privileged network actions in
Drover**.

The **completion artifact** must record which pattern was used (`"post_hook_owner": "controller"`)
so Drover can assert `pr_url` was produced by the right layer.

---

## Operational concerns (unikernel instances)

- **Binary compatibility:** `drover-code` must be built as a PIE (Position-Independent
  Executable) — the default on modern Linux toolchains. Verify with `file drover-code`
  (should report "pie executable"). This is required for Unikraft's ELF binary
  compatibility layer.
- **Secrets:** inject `ANTHROPIC_API_KEY`, `GITHUB_TOKEN`, Slack webhook URL via
  hypervisor (Firecracker `boot_args` / virtio-vsock / init process), not baked
  into images.
- **Network egress allowlist:** egress policy is task-type dependent and must be
  configurable per job. A Go/Node.js agent running builds and tests will need
  package registry access in addition to Anthropic, GitHub, and Slack. Suggested
  default allowlist:
  - `api.anthropic.com` — always required
  - `github.com`, `api.github.com` — always required
  - `registry.npmjs.org`, `crates.io`, `pypi.org` — required if running builds
  - Slack webhook hostname — if using agent-owned notifications
- **Clock and TLS:** unikernels need correct time for HTTPS certificate validation.
  Ensure the VMM provides a time source (Firecracker does via KVM clock).
- **Workspace layout:** single clone path, known `workDir`, clean shutdown (sync
  filesystem before exit if using virtio-fs or shared storage).
- **Resource limits:** cap tokens (`DROVER_CODE_MAX_TOKENS`), wall time
  (`DROVER_CODE_TIMEOUT_SECS`), and tool call budget. Timeouts must be enforced
  by a watchdog goroutine outside the agent loop — not relying on the model to
  self-terminate. Map breaches to exit code `4`.
- **Conversation summarisation:** if `drover-code` performs in-memory conversation
  summarisation during a long agent run, it must flush a summarisation record to
  the completion artifact before exit. If the unikernel is killed mid-run, Drover
  needs to know what progress was made to avoid restarting from zero.

---

## Phased implementation

**Phase 1 — Contract hardening (minimal code)**

- Document current `runHeadless` behaviour and add **explicit** env/flag gating.
- Define exit code table and document stdin line protocol.
- Add `{"type":"heartbeat","ts":"..."}` emission to the agent event loop.

**Phase 2 — Orchestrator-friendly I/O**

- JSONL event sink on stdout; keep stderr for errors.
- `--prompt` / `--prompt-file`; deprecate implicit reliance on piped stdin for
  production orchestration.

**Phase 3 — Permission presets**

- `DROVER_CODE_PERMISSION_PRESET=unikernel` wiring into `internal/permissions`
  without TUI.
- Configurable `allowed_tools` / `denied_tools` from run config.

**Phase 4 — Completion artifact**

- `--result-json path` or `$DROVER_CODE_RESULT_PATH`; atomic write on exit;
  schema versioned; partial write on non-zero exit.
- Record `post_hook_owner`, `turns_used`, `tokens_used`, `commit_sha`.

**Phase 5 — Resource enforcement**

- Watchdog goroutine enforcing wall-clock timeout and token budget outside the
  agent loop.
- Configurable egress allowlist per job passed via env at boot.

---

## Non-goals (for this doc)

- Replacing the **webhook server** for GitHub-driven development flows.
- Defining the **Drover** control plane API (only the **drover-code worker**
  interface).
- Unikraft image build tooling — assumes a generic "Linux ABI-compatible runtime"
  using Unikraft's binary compatibility layer unless `drover-code` is natively
  ported later.
- Native Unikraft port of `drover-code` (building against Unikraft's musl port
  and linking directly) — deferred until binary compat layer proves insufficient.

---

## Open questions

1. Should **structured logs** duplicate every `agent.Event` or a **reduced**
   subset for size and stability? Recommendation: reduced subset by default,
   full event log opt-in via `DROVER_CODE_VERBOSE_EVENTS=1`.
2. For **multi-repo** or **monorepo sparse** checkouts, is `workDir` always the
   git root, or do we need first-class **subpath** in the task spec?
3. Do we require **deterministic test commands** in the task file (YAML front
   matter) vs natural-language-only prompts?
4. How should **conversation summarisation** interact with one-shot exit — flush
   to completion artifact vs skip? (See operational concerns above for interim
   recommendation.)
5. What is the right **heartbeat interval** default? Suggested: 30 seconds, with
   `DROVER_CODE_HEARTBEAT_INTERVAL_SECS` override.
6. Does the **Firecracker Go SDK** (or `kraft`'s programmatic API) provide
   sufficient VM lifecycle control for Drover's orchestrator, or does Drover need
   to shell out to `kraft` / use Firecracker's HTTP API directly?

---

## References

- `cmd/drover-code/main.go` — `runHeadless`, mode switch, webhook entry.
- `internal/agent/loop.go` — agent turn and tool execution.
- `internal/permissions/engine.go` — modes and policy without UI.
- [07 — TUI](07-tui.md) — interactive permission and display path.
- [10 — Integrations](10-integrations.md) — webhook and IDE bridge adapters.
- [Unikraft concepts](https://unikraft.org/docs/concepts) — unikernel model, boot times, memory footprint.
- [Unikraft compatibility](https://unikraft.org/docs/concepts/compatibility) — binary compat layer, PIE requirement, syscall shim.
- [Unikraft security](https://unikraft.org/docs/concepts/security) — cross-instance isolation, minimal attack surface.
