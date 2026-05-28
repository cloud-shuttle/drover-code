# Drover Code — domain glossary

Canonical language for Drover Code. Shared actors (**customer**, **member**, **agent**) are defined in [`../CONTEXT-MAP.md`](../CONTEXT-MAP.md). Provisioning and hosted **agent job** dispatch are defined in [`../drover-cloud/CONTEXT.md`](../drover-cloud/CONTEXT.md). Brain query surface and **agent credential** details: [`../drover-brain/CONTEXT.md`](../drover-brain/CONTEXT.md). Implementation details belong in `docs/` and ADRs, not here.

## Product role

**Drover Code**  
The core agent engine: a static Go binary that runs the LLM tool loop (local TUI, headless batch, IDE bridge, GitHub webhook). On the platform, **Drover Cloud** dispatches **agent jobs** that run Drover Code headlessly inside isolated workers. Distinct from **Drover Orchestrator** (epic-level parallel dispatch in Git worktrees) and from **Drover Brain** (knowledge query).

## Remote execution strategy

**Worker contract**  
The shared HTTP interface between an **execution client** and a remote **worker runtime** (`ukc-agent` on an ephemeral Unikraft instance): workspace upload, headless agent execution, SSE progress stream, workspace download. Defined once; both BYOC and hosted paths must conform. Implementation details live in `docs/`, not here.

**Execution client**  
Whoever orchestrates remote runs against the **worker contract**. **BYOC execution** uses a local client (e.g. coordinator-remote with the operator’s Kraftcloud credentials). **Hosted execution** uses **Drover Cloud** as the client (job API, tenant isolation, platform-managed keys). v1 validates the contract via BYOC; SaaS wires the same contract on top.

**BYOC execution**  
Bring-your-own-cloud: the developer’s machine runs the **execution client** and provisions Unikraft instances with their own `UKC_TOKEN`. Workspace and API keys stay off Drover servers. Open-core path; not the production **member** experience.

**Hosted execution**  
Production path: **Drover Cloud** is the **execution client**—members trigger work via the **Cloud console** or API; Cloud provisions workers, injects **ephemeral job credentials**, and streams progress. Same **worker contract** as BYOC; different auth, billing, and lifecycle owner.

## Agent jobs and dispatch

**Agent job**  
One atomic remote run: provision one worker instance → sync workspace → one headless Drover Code execution → stream progress → download results → destroy instance. Defined in [`../drover-cloud/CONTEXT.md`](../drover-cloud/CONTEXT.md); Drover Code implements the in-worker half. Not a coordinator session, not **provisioning work**, not an Orchestrator epic.

**Job dispatch**  
Fan-out of multiple **agent jobs** from one user request. **Coordinator mode** (local `--coordinator-remote`) is a BYOC **job dispatch** pattern: decompose a prompt, run N parallel **agent jobs**, merge results locally. **Drover Cloud** Milestone A exposes single **agent jobs** only. Distinct from an **orchestrator run** ([`../drover/CONTEXT.md`](../drover/CONTEXT.md))—parallel **tasks** in Git worktrees on the operator’s machine, not Cloud workers.

**Worker instance**  
One ephemeral Unikraft machine for one **agent job** (1:1 in v1). Distinct from the **agent** (the Drover Code process doing the tool loop).

**Worker runtime**  
The HTTP sidecar on a **worker instance** that implements the **worker contract** (today: the `ukc-agent` binary). Not an **agent**—does not run the LLM loop. Prefer *worker runtime* in glossary and customer-facing docs; *ukc-agent* is the implementation name.

## Workspace sync

**Workspace payload**  
The tar.gz archive uploaded via `POST /workspace` and downloaded via `GET /workspace`. The **worker contract** is upload/download only—the worker runtime never clones Git or holds Git credentials.

**Workspace source**  
Where the **execution client** builds the **workspace payload** before upload. **BYOC execution** packs the developer’s local working tree (with exclusion rules). **Hosted execution** clones the **customer**’s **platform-managed Git remote** at a chosen ref in the client, then packs and uploads—remote is authoritative; unpushed local edits are not visible to hosted jobs.

**Workspace exclusion**  
Rules applied by the **execution client** when building a **workspace payload**: respect `.gitignore` (and optional `.droverignore`); always deny secret patterns (`.env`, `.env.*`, `*.pem`, `*.key`, `credentials*`, etc.) even if not gitignored; enforce max file size and max total payload—reject the job with a clear error if exceeded. **BYOC execution** may show file count/size preview before upload; **hosted execution** applies the same rules after clone without member preview.

**Upload preview**  
Optional BYOC confirmation before `POST /workspace`: show file count and payload size when stdin is a TTY (interactive coordinator-remote). Non-TTY / scripted BYOC skips preview but still enforces **workspace exclusion** and fails on cap exceed.

## Job results

**Result payload**  
The tar.gz archive returned via `GET /workspace` after a successful **agent job**—the modified sandbox tree. Same shape as **workspace payload**; download-only **worker contract** (no Git push on the worker).

**Base SHA**  
The Git commit the **execution client** cloned or captured when building the **workspace payload**. Recorded with the job so **result integration** knows what the agent started from. Used to detect concurrent remote changes—not for rsync or patch logic alone.

**Job branch**  
A dedicated Git branch for one **agent job**’s output, never a direct overwrite of `main`. **BYOC execution** uses `drover/task-{n}-*` branches; **hosted execution** uses `drover/job/{job-id}` (see [`../drover-cloud/CONTEXT.md`](../drover-cloud/CONTEXT.md)). Isolates parallel jobs and concurrent member commits.

**Result integration**  
**Execution client** post-processing after **result payload** download: materialize tree, commit on a **job branch**, push, then merge or hand off for review. Git owns concurrency; rsync may optimize local file copy during integration but does not replace branch/isolation semantics. Never rsync with `--delete` onto a moving `main` checkout. **Merge conflict handoff** (hosted): see [`../drover-cloud/CONTEXT.md`](../drover-cloud/CONTEXT.md).

**Incremental sync**  
Optional transport optimization (e.g. rsync) between client and **worker instance** to reduce bytes on repeated upload/download. Belongs in the **execution client**, not in the **worker contract**. Does not resolve commit races—**job branch** and **base SHA** do.

## Worker credentials

**Worker runtime token**  
Secret the **execution client** generates per **worker instance** and injects as `AGENT_TOKEN`. Authenticates HTTP calls to the **worker runtime** (`/health`, `/workspace`, `/exec`). Distinct from an **agent credential** (Brain MCP secret—see [`../CONTEXT-MAP.md`](../CONTEXT-MAP.md)). Never use *agent token* for `AGENT_TOKEN`; prefer *worker runtime token*.

**Job environment**  
Environment variables the **execution client** injects when provisioning a **worker instance** before headless Drover Code starts. Split by path: **BYOC execution** may pass through the operator’s LLM provider keys (operator risk). **Hosted execution** injects Gateway base URL + **agent-facing virtual key** (shared in Milestone A; **job-scoped** at GA), **ephemeral job credential** (Brain MCP), and **worker runtime token**—no raw provider API keys on hosted workers.

## Instance lifecycle

**Instance lifecycle**  
Owned by the **execution client** for **agent jobs**: provision at job start, destroy in a guaranteed finally (success, failure, cancel, timeout). One **worker instance** per **agent job** (1:1); no model-driven create/delete inside contract headless runs. Hosted jobs require destroy even on client crash (reconciliation via Cloud job state).

**Ad-hoc instance**  
A **worker instance** created by the model via `ukc_*` tools during an interactive or exploratory session—not part of the **agent job** contract path. Escape hatch for power users; not used for hosted **member** jobs. Operator responsible for cleanup (`ukc_delete_all`, instance registry, periodic reconciliation). Contract headless workers should not expose `ukc_*` in the unikernel tool preset.

**Orphan reconciliation**  
Background process that terminates **worker instances** whose **agent job** ended without destroy (client crash, network drop). Required for production **hosted execution**; recommended for **BYOC execution**.

## Job progress

**Execution stream**  
Server-Sent Events from `GET /exec/{job_id}/stream` on the **worker runtime**—stdout/stderr lines and completion from the headless Drover Code process. Canonical live telemetry for the **worker contract**.

**Progress proxy**  
**BYOC execution** (v1 contract validation): **execution client** forwards the **execution stream** directly to the CLI with optional reconnect (`Last-Event-ID`). No separate event model required to prove the contract.

**Stream replay**  
Worker-contract guarantee for reconnecting to an **execution stream**: **worker runtime** retains a bounded ring buffer (e.g. last 1000 events) per exec job; each SSE event has a monotonic index sent as `id:`. Client sends `Last-Event-ID` and retries with backoff until it receives `done`. History kept until job completes plus a grace window (e.g. 15 min), then stream returns 404. Events evicted from the buffer are lost—client may warn on gap. **Job event log** (hosted) persists the full transcript separately.

**Job event log**  
**Hosted execution** target: **Drover Cloud** ingests the **execution stream** into durable storage and proxies live to the **Cloud console**; console reads history from the log after disconnect or job end. Dual layer—live from worker, replay from store—without changing **worker contract** endpoints.

## Worker image

**Default worker image**  
Stock Unikraft/OCI image with **worker runtime**, headless Drover Code, and minimal tooling (bash, git). Used when the workspace declares no custom toolchain. Sufficient for contract v1 validation on simple repos. Post-**Warden integration gate**: platform default Beads baked into this image—see [`../drover-warden/CONTEXT.md`](../drover-warden/CONTEXT.md), [ADR 0006](../drover-brain/docs/adr/0006-drover-warden-hosted-integration.md).

**Custom worker image**  
Optional per-workspace image built by the **execution client** when `drover-worker.Dockerfile` (or equivalent) is present—adds language runtimes (Node, Rust, etc.) before provision. Not required for v1 contract proof; required for credible runs on many real repos. **Hosted execution** may later offer a platform catalog in addition to workspace-declared Dockerfiles.

## In-worker agent

**Sandbox agent**  
Headless Drover Code inside `/workspace` during an **agent job**. May use workspace tools (read/write/edit, bash, glob, grep, git read and commit *inside the sandbox*, web_fetch). Denied: `git_push` (no Git credentials on worker), all `ukc_*` (lifecycle owned by **execution client**), and infra-provisioning tools. Sandbox git history is ephemeral; **result integration** creates the real **job branch** from the **result payload**. On **hosted execution**, Brain access via Gateway MCP and **ephemeral job credential** only.

**Inference driver**  
The interface (seam) between the orchestrating **sandbox agent** loop and the underlying LLM/inference network mechanics. It abstracts away specific chunking, token tracking, and SSE parsing (e.g. Anthropic's message chunks) and yields complete semantic intents (text generation, tool call requests) to the agent loop.

**Drover Warden (deferred)**  
Semantic content safety (JSONL Beads: bash patterns, PII, input/output guards) is **not** embedded in Milestone A hosted runs—**`unikernel` preset** provides structural tool allow/deny only. When Warden ships on hosted jobs, it runs **in-process here on the worker** (action guard before tool execute; input/output around LLM turns)—not at Gateway. See [`../drover-warden/CONTEXT.md`](../drover-warden/CONTEXT.md).

## SQLForge integration

**SQLForge workspace**  
A checkout whose root (walking up from `workDir`) contains `sqlforge.yml`. Activates automatic **system prompt injection** with CLI commands (`plan`, `apply`, `query`, `snapshot`, `env create`). Detection: `internal/integrations/sqlforge`; wired in `internal/config` on `Load()`.

**SQLForge CLI path (v1)**  
Primary integration with [`../drover-sqlforge/CONTEXT.md`](../drover-sqlforge/CONTEXT.md): agents invoke `sqlforge` via **bash** in the project root—not Brain MCP for metrics or warehouse DDL. **Apply** requires explicit user or CI confirmation. Optional sidecar `sqlforge mcp <env>` for read-heavy MCP tools. See [`docs/how-to/sqlforge-from-drover-code.md`](docs/how-to/sqlforge-from-drover-code.md) and [`../drover-sqlforge/docs/adr/0002-cli-invocation-drover-code-integration.md`](../drover-sqlforge/docs/adr/0002-cli-invocation-drover-code-integration.md).
